// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Pause handles POST /fb/v1/console/inbound/sources/{id}/pause.
func (h *Handler) Pause(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.PauseInboundSourceRequest) (dispatcher.Result[*attunev1.InboundSource], error) {
	return h.setEnabled(ctx, req.GetId(), false)
}

// Resume handles POST /fb/v1/console/inbound/sources/{id}/resume.
func (h *Handler) Resume(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ResumeInboundSourceRequest) (dispatcher.Result[*attunev1.InboundSource], error) {
	return h.setEnabled(ctx, req.GetId(), true)
}

func (h *Handler) setEnabled(ctx *dispatcher.RequestContext[*session.AuthCtx], id string, enabled bool) (dispatcher.Result[*attunev1.InboundSource], error) {
	const where = "console.inbound.setEnabled"
	auth := ctx.Auth
	_, err := h.getOwnedSource(ctx, auth, id, where)
	if err != nil {
		return dispatcher.Result[*attunev1.InboundSource]{}, err
	}
	reason := ""
	if !enabled {
		reason = "paused by admin"
	}
	if err := h.sources.SetEnabled(ctx, id, enabled, reason); err != nil {
		logext.Errorf(ctx, "[%s] SetEnabled failed,tenant_id:%s,id:%s,enabled:%v,err:%+v",
			where, auth.TenantID, id, enabled, err.Error())
		return dispatcher.Fail[*attunev1.InboundSource](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to update enabled flag")
	}
	// Reload so the response carries the freshly-mutated state.
	updated, err := h.sources.Get(ctx, id)
	if err != nil {
		logext.Errorf(ctx, "[%s] post-toggle reload failed,id:%s,err:%+v",
			where, id, err.Error())
		return dispatcher.Fail[*attunev1.InboundSource](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "toggle ok but reload failed")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s,enabled:%v",
		where, auth.TenantID, id, enabled)
	return dispatcher.OK(rowToProto(updated))
}
