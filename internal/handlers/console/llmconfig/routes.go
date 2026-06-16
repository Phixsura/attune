// SPDX-License-Identifier: Apache-2.0

package llmconfig

import (
	"errors"
	"net/http"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	llmrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
	llmsvc "github.com/Phixsura/attune/internal/service/llmconfig"
)

func (h *Handler) ListRoutes(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.ListLLMRoutesRequest,
) (dispatcher.Result[*attunev1.ListLLMRoutesResponse], error) {
	rows, err := h.svc.ListRoutes(ctx)
	if err != nil {
		return dispatcher.Fail[*attunev1.ListLLMRoutesResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list llm routes")
	}
	items := make([]*attunev1.LLMRoute, 0, len(rows))
	for _, row := range rows {
		items = append(items, toRouteProto(row))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListLLMRoutesResponse{Items: items}))
}

func (h *Handler) UpsertRoute(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpsertLLMRouteRequest,
) (dispatcher.Result[*attunev1.LLMRoute], error) {
	before := h.findRoute(ctx, req.GetTenantId(), req.GetPurpose())
	row, err := h.svc.UpsertRoute(ctx, llmsvc.RouteInput{
		TenantID:     req.GetTenantId(),
		Purpose:      req.GetPurpose(),
		LogicalModel: req.GetLogicalModel(),
		Enabled:      optionalBoolDefault(req.Enabled, true),
	})
	if err != nil {
		if isValidation(err) {
			return dispatcher.Fail[*attunev1.LLMRoute](
				http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
		}
		return dispatcher.Fail[*attunev1.LLMRoute](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to upsert llm route")
	}
	var beforeSnapshot any
	if before != nil {
		beforeValue := ptrext.Indirect(before)
		beforeSnapshot = routeAuditSnapshot(beforeValue)
	}
	rowValue := ptrext.Indirect(row)
	if err := h.recordAudit(ctx, "llm_route.upsert", "llm_route",
		llmTargetID(rowValue.ID.String(), rowValue.TenantID, rowValue.Purpose),
		llmRouteSummary("Upserted LLM route", rowValue), beforeSnapshot, routeAuditSnapshot(rowValue)); err != nil {
		return dispatcher.Fail[*attunev1.LLMRoute](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(toRouteProto(rowValue))
}

func (h *Handler) DeleteRoute(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.DeleteLLMRouteRequest,
) (dispatcher.Result[*attunev1.DeleteLLMRouteResponse], error) {
	before := h.findRoute(ctx, req.GetTenantId(), req.GetPurpose())
	if err := h.svc.DeleteRoute(ctx, req.GetTenantId(), req.GetPurpose()); err != nil {
		if errors.Is(err, llmrepo.ErrRouteNotFound) {
			return dispatcher.Fail[*attunev1.DeleteLLMRouteResponse](
				http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "llm route not found")
		}
		if isValidation(err) {
			return dispatcher.Fail[*attunev1.DeleteLLMRouteResponse](
				http.StatusBadRequest, attunev1.ErrorCode_VALIDATION, err.Error())
		}
		return dispatcher.Fail[*attunev1.DeleteLLMRouteResponse](
			http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to delete llm route")
	}
	var beforeSnapshot any
	summary := "Deleted LLM route"
	if before != nil {
		beforeValue := ptrext.Indirect(before)
		beforeSnapshot = routeAuditSnapshot(beforeValue)
		summary = llmRouteSummary(summary, beforeValue)
	}
	if err := h.recordAudit(ctx, "llm_route.delete", "llm_route",
		llmTargetID("", req.GetTenantId(), req.GetPurpose()), summary, beforeSnapshot, nil); err != nil {
		return dispatcher.Fail[*attunev1.DeleteLLMRouteResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to write audit log")
	}
	return dispatcher.OK(ptrext.Of(attunev1.DeleteLLMRouteResponse{}))
}
