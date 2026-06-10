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
	"github.com/Phixsura/attune/internal/inbound/adapter/webhook"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
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
	encSecret, err := h.secrets.Encrypt(rawSecret)
	if err != nil {
		logext.Errorf(ctx, "[%s] encrypt secret failed,err:%+v", where, err.Error())
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to encrypt secret")
	}
	cfg := webhook.Config{
		Version:                webhook.ConfigVersion,
		SecretCurrentEncrypted: encSecret,
		HMACAlgo:               "sha256",
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		logext.Errorf(ctx, "[%s] marshal cfg failed,err:%+v", where, err.Error())
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to encode config")
	}
	envelope, err := h.secrets.Encrypt(cfgBytes)
	if err != nil {
		logext.Errorf(ctx, "[%s] encrypt cfg failed,err:%+v", where, err.Error())
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to encrypt config")
	}
	if err := h.insertRow(ctx, id, auth.TenantID, channelWebhook, name, slug, envelope); err != nil {
		return dispatcher.Result[*attunev1.CreateInboundSourceResponse]{}, h.insertErr(ctx, where, auth.TenantID, err)
	}

	stored, err := h.sources.Get(ctx, id)
	if err != nil {
		logext.Errorf(ctx, "[%s] post-insert reload failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.CreateInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "row created but reload failed")
	}
	resp := ptrext.Of(attunev1.CreateInboundSourceResponse{Source: rowToProto(stored)})

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
