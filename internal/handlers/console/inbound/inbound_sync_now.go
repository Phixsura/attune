// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// SyncNow handles POST /fb/v1/console/inbound/sources/{id}/sync-now.
// Triggers an immediate poll for the specified source. Non-blocking:
// the sync happens in the background on the adapter's poll loop.
func (h *Handler) SyncNow(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.PauseInboundSourceRequest) (dispatcher.Result[*attunev1.InboundSource], error) {
	const where = "console.inbound.SyncNow"
	auth := ctx.Auth
	src, err := h.getOwnedSource(ctx, auth, req.GetId(), where)
	if err != nil {
		return dispatcher.Result[*attunev1.InboundSource]{}, err
	}
	if !src.Enabled {
		return dispatcher.Fail[*attunev1.InboundSource](http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, "source is paused; resume it first")
	}
	if h.syncTrigger != nil {
		h.syncTrigger(src.ID)
	}
	logext.Infof(ctx, "[%s] sync-now requested,tenant_id:%s,source_id:%s", where, auth.TenantID, src.ID)

	if err := h.recordAudit(ctx, auth.UserType, auth.UserID, auth.TenantID, "inbound_source.sync_now", src.ID, "Triggered immediate sync", nil, nil, map[string]any{
		"id":      src.ID,
		"channel": src.Channel,
	}); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,source_id:%s,err:%+v", where, auth.TenantID, src.ID, err.Error())
	}
	return dispatcher.OK(rowToProto(src))
}
