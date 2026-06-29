// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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

	hdrSlackSignature      = "X-Slack-Signature"
	hdrSlackRequestTS      = "X-Slack-Request-Timestamp"
	slackSignatureVersion  = "v0"
	timestampToleranceSecs = 300

	unknownLabel = "unknown"
)

// slackPayload is the top-level Events API POST body.
type slackPayload struct {
	Type      string     `json:"type"`
	Token     string     `json:"token"`
	Challenge string     `json:"challenge"`
	TeamID    string     `json:"team_id"`
	Event     slackEvent `json:"event"`
	EventID   string     `json:"event_id"`
	EventTime int64      `json:"event_time"`
}

type slackEvent struct {
	Type    string `json:"type"`
	User    string `json:"user"`
	Text    string `json:"text"`
	Channel string `json:"channel"`
	TS      string `json:"ts"`
	BotID   string `json:"bot_id"`

	// reaction_added / reaction_removed
	Reaction string     `json:"reaction"`
	Item     *eventItem `json:"item"`
	ItemUser string     `json:"item_user"`
}

type eventItem struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
	TS      string `json:"ts"`
}

// handleHTTP is the raw HTTP handler. It handles both url_verification
// challenges and event_callback payloads. We use a raw handler instead
// of the dispatcher framework because Slack's url_verification response
// must be a bare JSON { "challenge": "..." } — not attune's standard
// envelope.
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

	signingSecret, err := a.getSigningSecret(ctx, src)
	if err != nil {
		logext.Errorf(ctx, "[inbound.slack] config decrypt failed,err:%+v", err.Error())
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "internal_err")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	timestamp := r.Header.Get(hdrSlackRequestTS)
	signature := r.Header.Get(hdrSlackSignature)
	if !verifySlackSignature(signingSecret, timestamp, body, signature) {
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "auth_err")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var payload slackPayload
	if err := json.Unmarshal(body, &payload); err != nil { // ptrext:allow unmarshal-out-param
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	switch payload.Type {
	case "url_verification":
		w.Header().Set("Content-Type", "application/json")
		resp, _ := json.Marshal(map[string]string{"challenge": payload.Challenge})
		w.Write(resp) //nolint:errcheck // best-effort response
		return

	case "event_callback":
		a.handleEvent(ctx, w, src, payload, start)
		return

	default:
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		http.Error(w, "unsupported event type", http.StatusBadRequest)
	}
}

func (a *adapter) handleEvent(
	ctx context.Context, w http.ResponseWriter,
	src inbound.Source, payload slackPayload, start time.Time,
) {
	const where = "inbound.slack.handleEvent"
	ev := payload.Event

	if ev.BotID != "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	content := extractContent(ev)
	if content == "" {
		a.deps.Metrics.Total(channelName, src.TenantID, src.Slug, "validate_err")
		w.WriteHeader(http.StatusOK)
		return
	}

	meta := map[string]any{
		domain.SourceMetaInboundSourceID:   src.ID,
		domain.SourceMetaInboundSourceName: src.Name,
		"slack_team_id":                    payload.TeamID,
		"slack_channel":                    ev.Channel,
		"slack_user":                       ev.User,
		"slack_event_type":                 ev.Type,
		"slack_event_id":                   payload.EventID,
		"slack_ts":                         ev.TS,
	}

	in := domain.IngestInput{
		Source:     channelName,
		Content:    content,
		SourceUser: ev.User,
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

	w.WriteHeader(http.StatusOK)
	resp, _ := json.Marshal(map[string]any{"id": id})
	w.Write(resp) //nolint:errcheck // best-effort response
}

func extractContent(ev slackEvent) string {
	switch ev.Type {
	case "message", "app_mention":
		return ev.Text
	case "reaction_added":
		if ev.Reaction != "" {
			return fmt.Sprintf(":%s: reaction on message", ev.Reaction)
		}
		return ""
	default:
		return ev.Text
	}
}

func (a *adapter) getSigningSecret(ctx context.Context, src inbound.Source) ([]byte, error) {
	cfg, err := parseConfig(src.Config, a.deps.Secrets)
	if err != nil {
		return nil, err
	}
	return cfg.signingSecret, nil
}

// verifySlackSignature verifies the Slack request signature per
// https://api.slack.com/authentication/verifying-requests-from-slack
func verifySlackSignature(secret []byte, timestamp string, body []byte, signature string) bool {
	if len(secret) == 0 || timestamp == "" || signature == "" {
		return false
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if abs(nowFn().Unix()-ts) > timestampToleranceSecs {
		return false
	}

	sigBaseString := fmt.Sprintf("%s:%s:%s", slackSignatureVersion, timestamp, body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sigBaseString))
	expected := fmt.Sprintf("%s=%s", slackSignatureVersion, hex.EncodeToString(mac.Sum(nil)))

	return hmac.Equal([]byte(expected), []byte(signature))
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
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
