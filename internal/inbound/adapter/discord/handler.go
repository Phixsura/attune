// SPDX-License-Identifier: Apache-2.0

package discord

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	maxBodyBytes = 64 * 1024

	hdrSignature = "X-Signature-Ed25519"
	hdrTimestamp = "X-Signature-Timestamp"

	unknownLabel = "unknown"

	interactionPing               = 1
	interactionApplicationCommand = 2
	interactionMessageComponent   = 3
	interactionModalSubmit        = 5
)

// discordPayload is the top-level Discord Interactions POST body.
type discordPayload struct {
	Type int             `json:"type"`
	Data json.RawMessage `json:"data"`

	// Present on application_command / message_component / modal_submit
	GuildID   string       `json:"guild_id"`
	ChannelID string       `json:"channel_id"`
	Member    *guildMember `json:"member"`
	User      *discordUser `json:"user"`
	Message   *msgObject   `json:"message"`
	ID        string       `json:"id"`
	Token     string       `json:"token"`
}

type guildMember struct {
	User discordUser `json:"user"`
}

type discordUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type msgObject struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

type commandData struct {
	Name    string          `json:"name"`
	Options []commandOption `json:"options"`
}

type commandOption struct {
	Name  string `json:"name"`
	Type  int    `json:"type"`
	Value any    `json:"value"`
}

func (a *adapter) handleHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := nowFn()

	tenantSlug := chi.URLParam(r, "tenant-slug")
	sourceSlug := chi.URLParam(r, "source-slug")

	body, err := readCappedBody(r.Body, maxBodyBytes)
	if err != nil {
		a.deps.Metrics.Total(channelName, unknownLabel, unknownLabel, "validate_err")
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}

	src, srcErr := a.deps.Sources.GetBySlugs(ctx, tenantSlug, channelName, sourceSlug)
	if srcErr != nil || !src.Enabled {
		a.deps.Metrics.Total(channelName, unknownLabel, unknownLabel, "auth_err")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	pubKey, err := a.getPublicKey(ctx, src)
	if err != nil {
		logext.Errorf(ctx, "[inbound.discord] config decrypt failed,err:%+v", err.Error())
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	sig := r.Header.Get(hdrSignature)
	ts := r.Header.Get(hdrTimestamp)
	if !verifyDiscordSignature(pubKey, sig, ts, body) {
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "auth_err")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var payload discordPayload
	if err := json.Unmarshal(body, &payload); err != nil { // ptrext:allow unmarshal-out-param
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	switch payload.Type {
	case interactionPing:
		w.Header().Set("Content-Type", "application/json")
		resp, _ := json.Marshal(map[string]int{"type": 1})
		w.Write(resp) //nolint:errcheck // best-effort response
		return

	case interactionApplicationCommand, interactionMessageComponent, interactionModalSubmit:
		a.handleInteraction(ctx, w, src, payload, body, start)
		return

	default:
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		http.Error(w, "unsupported interaction type", http.StatusBadRequest)
	}
}

func (a *adapter) handleInteraction(
	ctx context.Context, w http.ResponseWriter,
	src inbound.Source, payload discordPayload, rawBody []byte, start time.Time,
) {
	const where = "inbound.discord.handleInteraction"

	content := extractContent(payload)
	if content == "" {
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		w.WriteHeader(http.StatusOK)
		return
	}

	userID, username := resolveUser(payload)

	meta := map[string]any{
		domain.SourceMetaInboundSourceID:   src.ID,
		domain.SourceMetaInboundSourceName: src.Name,
		"discord_guild_id":                 payload.GuildID,
		"discord_channel_id":               payload.ChannelID,
		"discord_user_id":                  userID,
		"discord_username":                 username,
		"discord_interaction_id":           payload.ID,
	}

	in := domain.IngestInput{
		Source:     channelName,
		Content:    content,
		SourceUser: username,
		SourceMeta: meta,
	}

	id, err := a.deps.Ingest.Ingest(ctx, src.TenantID, uuid.Nil, in)
	if err != nil {
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		logext.Warnf(ctx, "[%s] ingest failed,err:%+v", where, err.Error())
		http.Error(w, "ingest error", http.StatusBadRequest)
		return
	}

	a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "ok")
	a.deps.Metrics.Latency(channelName, src.TenantID, src.Slug, time.Since(start).Seconds())
	a.deps.Metrics.SetSourceState(channelName, src.TenantID, src.Slug, "enabled", true)

	if err := a.deps.Sources.UpdateState(ctx, src.ID, inbound.SourceState{
		LastEventAt: ptrext.Of(nowFn()),
	}); err != nil {
		logext.Warnf(ctx, "[%s] UpdateState failed,err:%+v", where, err.Error())
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,source_id:%s,feedback_id:%d",
		where, src.TenantID, src.ID, id)

	// Respond with ACK (type 1) — Discord requires a response within 3s.
	w.Header().Set("Content-Type", "application/json")
	resp, _ := json.Marshal(map[string]any{"type": 1})
	w.Write(resp) //nolint:errcheck // best-effort response
}

func extractContent(p discordPayload) string {
	if p.Message != nil && p.Message.Content != "" {
		return p.Message.Content
	}

	if p.Type == interactionApplicationCommand && len(p.Data) > 0 {
		var cmd commandData
		if json.Unmarshal(p.Data, &cmd) == nil { // ptrext:allow unmarshal-out-param
			for _, opt := range cmd.Options {
				if opt.Name == "feedback" || opt.Name == "content" || opt.Name == "text" {
					if s, ok := opt.Value.(string); ok && s != "" {
						return s
					}
				}
			}
			if cmd.Name != "" {
				return fmt.Sprintf("/%s command invoked", cmd.Name)
			}
		}
	}

	return ""
}

func resolveUser(p discordPayload) (id, username string) {
	if p.Member != nil {
		return p.Member.User.ID, p.Member.User.Username
	}
	if p.User != nil {
		return p.User.ID, p.User.Username
	}
	return "", ""
}

func (a *adapter) getPublicKey(ctx context.Context, src inbound.Source) (ed25519.PublicKey, error) {
	cfg, err := parseConfig(src.Config, a.deps.Secrets)
	if err != nil {
		return nil, err
	}

	pk, err := hex.DecodeString(string(cfg.publicKey))
	if err != nil {
		return nil, fmt.Errorf("discord public key: invalid hex: %w", err)
	}
	if len(pk) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("discord public key: expected %d bytes, got %d", ed25519.PublicKeySize, len(pk))
	}
	return pk, nil
}

// verifyDiscordSignature verifies the Ed25519 signature per
// https://discord.com/developers/docs/interactions/overview#setting-up-an-endpoint
func verifyDiscordSignature(pubKey ed25519.PublicKey, sigHex, timestamp string, body []byte) bool {
	if len(pubKey) == 0 || sigHex == "" || timestamp == "" {
		return false
	}

	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	if len(sig) != ed25519.SignatureSize {
		return false
	}

	msg := []byte(timestamp)
	msg = append(msg, body...)
	return ed25519.Verify(pubKey, msg, sig)
}

func readCappedBody(r io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("body exceeds %d bytes", limit)
	}
	return body, nil
}
