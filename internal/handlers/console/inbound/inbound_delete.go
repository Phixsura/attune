// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Delete handles DELETE /fb/v1/console/inbound/sources/{id}. The
// SourceStore interface deliberately stops at SetEnabled (the adapters
// only ever need to toggle), so we issue the DELETE inline here.
func (h *Handler) Delete(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.DeleteInboundSourceRequest) (dispatcher.Result[*attunev1.DeleteInboundSourceResponse], error) {
	const where = "console.inbound.Delete"
	auth := ctx.Auth
	id := req.GetId()
	// Look up first to enforce tenant ownership in a single round-trip
	// before issuing the destructive SQL.
	src, err := h.getOwnedSource(ctx, auth, id, where)
	if err != nil {
		return dispatcher.Result[*attunev1.DeleteInboundSourceResponse]{}, err
	}

	if h.pool == nil {
		return dispatcher.Fail[*attunev1.DeleteInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "inbound: pool not configured")
	}
	tag, err := h.pool.Exec(ctx, `DELETE FROM inbound_sources WHERE id = $1`, id)
	if err != nil {
		logext.Errorf(ctx, "[%s] delete failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.DeleteInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to delete inbound source")
	}
	if tag.RowsAffected() == 0 {
		// Race: deleted between Get and Exec. Treat as 404 — the
		// admin's intent is reached either way.
		return dispatcher.Fail[*attunev1.DeleteInboundSourceResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "inbound source not found")
	}
	if err := h.recordAudit(ctx, auth.UserType, auth.UserID, auth.TenantID, "inbound_source.delete", src.ID, "Deleted inbound source", ctx.Request(), map[string]any{
		"id":      src.ID,
		"channel": src.Channel,
		"name":    src.Name,
		"slug":    src.Slug,
		"enabled": src.Enabled,
	}, nil); err != nil {
		logext.Errorf(ctx, "[%s] audit write failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return dispatcher.Fail[*attunev1.DeleteInboundSourceResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,id:%s", where, auth.TenantID, id)
	return dispatcher.OK(ptrext.Of(attunev1.DeleteInboundSourceResponse{}))
}
