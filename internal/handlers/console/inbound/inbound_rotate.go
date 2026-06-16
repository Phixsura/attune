// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound/adapter/webhook"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Rotate handles POST /fb/v1/console/inbound/sources/{id}/rotate-secret.
// Calls into webhook.RotateSecret (the one place where the handler
// layer touches an adapter directly — documented exception in the
// inbound-boundary depguard rule).
func (h *Handler) Rotate(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.RotateInboundSourceSecretRequest) (dispatcher.Result[*attunev1.RotateInboundSourceSecretResponse], error) {
	const where = "console.inbound.Rotate"
	auth := ctx.Auth
	id := req.GetId()
	src, err := h.getOwnedSource(ctx, auth, id, where)
	if err != nil {
		return dispatcher.Result[*attunev1.RotateInboundSourceSecretResponse]{}, err
	}
	if src.Channel != channelWebhook {
		return dispatcher.Fail[*attunev1.RotateInboundSourceSecretResponse](http.StatusBadRequest, attunev1.ErrorCode_UNSUPPORTED,
			"rotate-secret only applies to webhook sources")
	}

	logext.Infof(ctx, "[%s] start,tenant_id:%s,id:%s", where, auth.TenantID, id)
	newSecret, nextEligible, err := h.rotate(ctx, h.pool, h.secrets, id)
	if err != nil {
		if errors.Is(err, webhook.ErrRotationInGraceWindow) {
			nextStr := nextEligible.UTC().Format(time.RFC3339)
			logext.Warnf(ctx, "[%s] reject: grace window,tenant_id:%s,id:%s,next:%s",
				where, auth.TenantID, id, nextStr)
			return dispatcher.Fail[*attunev1.RotateInboundSourceSecretResponse](http.StatusConflict,
				attunev1.ErrorCode_ROTATION_IN_GRACE_WINDOW,
				"rotation refused while previous secret is still inside its 24h grace window")
		}
		logext.Errorf(ctx, "[%s] rotate failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.RotateInboundSourceSecretResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to rotate secret")
	}
	// Reuse the `nextEligible` that the rotator already computed (and
	// persisted into the on-disk envelope). Recomputing `time.Now() +
	// GraceWindow` here drifted by microseconds vs the DB value (#66
	// review H-3); the response field and the actual grace boundary
	// now agree to the nanosecond.
	resp := ptrext.Of(attunev1.RotateInboundSourceSecretResponse{
		SecretHex:      hex.EncodeToString(newSecret),
		NextEligibleAt: nextEligible.UTC().Format(time.RFC3339),
	})
	if err := h.recordAudit(ctx, auth.UserType, auth.UserID, auth.TenantID, "inbound_source.rotate_secret", src.ID, "Rotated inbound webhook secret", ctx.Request(), nil, map[string]any{
		"id":               src.ID,
		"channel":          src.Channel,
		"next_eligible_at": nextEligible.UTC().Format(time.RFC3339),
	}); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.RotateInboundSourceSecretResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s", where, auth.TenantID, id)
	return dispatcher.OK(resp)
}
