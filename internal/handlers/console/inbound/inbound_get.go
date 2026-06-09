// SPDX-License-Identifier: Apache-2.0

package inbound

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/pkg/logext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/inboundsource"
)

// Get handles GET /fb/v1/console/inbound/sources/{id}.
func (h *Handler) Get(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.GetInboundSourceRequest) (dispatcher.Result[*attunev1.InboundSource], error) {
	const where = "console.inbound.Get"
	src, err := h.getOwnedSource(ctx, ctx.Auth, req.GetId(), where)
	if err != nil {
		return dispatcher.Result[*attunev1.InboundSource]{}, err
	}
	return dispatcher.OK(rowToProto(src))
}

func (h *Handler) getOwnedSource(ctx context.Context, auth *session.AuthCtx, id, where string) (inbound.Source, error) {
	if !isUUID(id) {
		logext.Warnf(ctx, "[%s] reject: bad id,tenant_id:%s", where, auth.TenantID)
		return inbound.Source{}, dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "id is not a UUID")
	}
	src, err := h.sources.Get(ctx, id)
	if err != nil {
		if errors.Is(err, inboundsource.ErrNotFound) {
			return inbound.Source{}, dispatcher.NewError(http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "inbound source not found")
		}
		logext.Errorf(ctx, "[%s] sources.Get failed,tenant_id:%s,id:%s,err:%+v",
			where, auth.TenantID, id, err.Error())
		return inbound.Source{}, dispatcher.NewError(http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to load inbound source")
	}
	if src.TenantID != auth.TenantID {
		return inbound.Source{}, dispatcher.NewError(http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "inbound source not found")
	}
	return src, nil
}

// isUUID — quick UUID-shape gate before issuing DB ops.
func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
