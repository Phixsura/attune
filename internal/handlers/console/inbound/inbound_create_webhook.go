// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	inboundcore "github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/adapter/webhook"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/secretlock"
)

const secretLen = 32

// createWebhook handles the webhook-channel branch of Create. Generates
// a fresh 32-byte secret, encrypts both the secret and the wrapping
// webhookConfig JSON via the SecretStore, then inserts the row. The raw
// secret is returned once in the response — never persisted plaintext.
func (h *Handler) createWebhook(ctx context.Context, auth *session.AuthCtx, name, slug string) (dispatcher.Result[*attunev1.CreateInboundSourceResponse], error) {
	const where = "console.inbound.createWebhook"
	id := uuid.NewString()

	rawSecret := make([]byte, secretLen)
	if _, err := rand.Read(rawSecret); err != nil {
		logext.Errorf(ctx, "[%s] rand failed,err:%+v", where, err.Error())
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to generate secret")
	}

	if err := secretlock.WithTx(ctx, h.pool, true, func(ctx context.Context, tx secretlock.Tx) error {
		if err := secretlock.EnsureWritableKey(ctx, tx, inboundcore.PrimaryKeyID(h.secrets)); err != nil {
			return err
		}
		encSecret, err := h.secrets.Encrypt(rawSecret)
		if err != nil {
			return fmt.Errorf("encrypt webhook secret: %w", err)
		}
		cfg := webhook.Config{
			Version:                webhook.ConfigVersion,
			SecretCurrentEncrypted: encSecret,
			HMACAlgo:               "sha256",
		}
		cfgBytes, err := json.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("marshal webhook config: %w", err)
		}
		envelope, err := h.secrets.Encrypt(cfgBytes)
		if err != nil {
			return fmt.Errorf("encrypt webhook config: %w", err)
		}
		return h.insertRowTx(ctx, tx, id, auth.TenantID, channelWebhook, name, slug, envelope)
	}); err != nil {
		return dispatcher.Result[*attunev1.CreateInboundSourceResponse]{}, h.insertErr(ctx, where, auth.TenantID, err)
	}

	stored, err := h.sources.Get(ctx, id)
	if err != nil {
		logext.Errorf(ctx, "[%s] post-insert reload failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "row created but reload failed")
	}
	resp := ptrext.Of(attunev1.CreateInboundSourceResponse{Source: rowToProto(stored)})
	if err := h.recordAudit(ctx, auth.UserType, auth.UserID, auth.TenantID, "inbound_source.create", stored.ID, "Created webhook inbound source", nil, nil, map[string]any{
		"id":      stored.ID,
		"channel": stored.Channel,
		"name":    stored.Name,
		"slug":    stored.Slug,
		"enabled": stored.Enabled,
	}); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, stored.ID, err.Error())
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}

	tenantSlug, err := h.tenantSlug(ctx, auth.TenantID)
	if err != nil {
		// The row landed; surface a soft warning but still return the
		// secret so the operator can finish setup. The URL field will
		// be empty — they can derive it from docs.
		logext.Warnf(ctx, "[%s] tenantSlug lookup failed,tenant_id:%s,err:%s",
			where, auth.TenantID, err.Error())
		tenantSlug = ""
	}
	reveal := ptrext.Of(attunev1.WebhookSecretReveal{
		SecretHex: hex.EncodeToString(rawSecret),
	})
	if tenantSlug != "" && h.baseURL != "" {
		reveal.Url = fmt.Sprintf("%s/v1/inbound/webhook/%s/%s", h.baseURL, tenantSlug, slug)
		reveal.CurlExample = buildCurlExample(reveal.Url)
	}
	resp.WebhookSecretReveal = reveal
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,slug:%s",
		where, auth.TenantID, id, slug)
	return dispatcher.Created(resp)
}

// buildCurlExample — short curl one-liner the operator can paste. Body
// is an inline IngestRequest stub matching the proto JSON shape; the
// signature here is dummy (the operator computes the real HMAC client-
// side; the example is just to show the call shape).
func buildCurlExample(targetURL string) string {
	return fmt.Sprintf(`curl -X POST %q -H 'Content-Type: application/json' `+
		`-H 'X-Attune-Timestamp: <unix-seconds>' -H 'X-Attune-Signature: <hex-hmac>' `+
		`-d '{"content":"hello from webhook","source_user":"alice@example.com"}'`, targetURL)
}
