// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	auditlogviewrepo "github.com/Phixsura/attune/internal/repo/auditlogview"
	auditlogviewsvc "github.com/Phixsura/attune/internal/service/auditlogview"
)

type savedViewService interface {
	List(ctx context.Context, tenantID, userID string) ([]auditlogviewsvc.View, error)
	Save(ctx context.Context, tenantID, userID string, input auditlogviewsvc.SaveInput) (*auditlogviewsvc.View, error)
	Delete(ctx context.Context, tenantID, userID, id, updatedBy string) error
}

func (h *Handler) ListSavedViews(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListSavedAuditLogViewsRequest) (dispatcher.Result[*attunev1.ListSavedAuditLogViewsResponse], error) {
	const where = "console.auditlog.ListSavedViews"
	if h.views == nil {
		return dispatcher.Fail[*attunev1.ListSavedAuditLogViewsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "saved audit log views are unavailable")
	}
	views, err := h.views.List(ctx, ctx.Auth.TenantID, ctx.Auth.UserID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,user_id:%s,err:%+v", where, ctx.Auth.TenantID, ctx.Auth.UserID, err.Error())
		return dispatcher.Fail[*attunev1.ListSavedAuditLogViewsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list saved audit log views")
	}
	items := make([]*attunev1.SavedAuditLogView, 0, len(views))
	for _, view := range views {
		items = append(items, toProtoSavedView(ptrext.Of(view)))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListSavedAuditLogViewsResponse{Items: items}))
}

func (h *Handler) CreateSavedView(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateSavedAuditLogViewRequest) (dispatcher.Result[*attunev1.SavedAuditLogViewResponse], error) {
	return h.saveSavedView(ctx, req.GetName(), req.GetState(), "")
}

func (h *Handler) UpdateSavedView(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.UpdateSavedAuditLogViewRequest) (dispatcher.Result[*attunev1.SavedAuditLogViewResponse], error) {
	return h.saveSavedView(ctx, req.GetName(), req.GetState(), req.GetId())
}

func (h *Handler) DeleteSavedView(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.DeleteSavedAuditLogViewRequest) (dispatcher.Result[*attunev1.DeleteSavedAuditLogViewResponse], error) {
	const where = "console.auditlog.DeleteSavedView"
	if h.views == nil {
		return dispatcher.Fail[*attunev1.DeleteSavedAuditLogViewResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "saved audit log views are unavailable")
	}
	if err := h.views.Delete(ctx, ctx.Auth.TenantID, ctx.Auth.UserID, req.GetId(), ctx.Auth.UserID); err != nil {
		switch {
		case errors.Is(err, auditlogviewsvc.ErrValidation):
			return dispatcher.Fail[*attunev1.DeleteSavedAuditLogViewResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
		case errors.Is(err, auditlogviewrepo.ErrNotFound):
			return dispatcher.Fail[*attunev1.DeleteSavedAuditLogViewResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "saved view not found")
		default:
			logext.Errorf(ctx, "[%s] delete failed,tenant_id:%s,user_id:%s,id:%s,err:%+v", where, ctx.Auth.TenantID, ctx.Auth.UserID, req.GetId(), err.Error())
			return dispatcher.Fail[*attunev1.DeleteSavedAuditLogViewResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to delete saved audit log view")
		}
	}
	return dispatcher.OK(ptrext.Of(attunev1.DeleteSavedAuditLogViewResponse{}))
}

func (h *Handler) saveSavedView(ctx *dispatcher.RequestContext[*session.AuthCtx], name string, state *attunev1.AuditLogViewState, id string) (dispatcher.Result[*attunev1.SavedAuditLogViewResponse], error) {
	const where = "console.auditlog.saveSavedView"
	if h.views == nil {
		return dispatcher.Fail[*attunev1.SavedAuditLogViewResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "saved audit log views are unavailable")
	}
	view, err := h.views.Save(ctx, ctx.Auth.TenantID, ctx.Auth.UserID, auditlogviewsvc.SaveInput{
		ID:        id,
		Name:      name,
		State:     fromProtoSavedViewState(state),
		UpdatedBy: ctx.Auth.UserID,
	})
	if err != nil {
		switch {
		case errors.Is(err, auditlogviewsvc.ErrValidation):
			return dispatcher.Fail[*attunev1.SavedAuditLogViewResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
		case errors.Is(err, auditlogviewrepo.ErrConflict):
			return dispatcher.Fail[*attunev1.SavedAuditLogViewResponse](http.StatusConflict, attunev1.ErrorCode_CONFLICT, "saved view name already exists")
		case errors.Is(err, auditlogviewrepo.ErrNotFound):
			return dispatcher.Fail[*attunev1.SavedAuditLogViewResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "saved view not found")
		default:
			logext.Errorf(ctx, "[%s] save failed,tenant_id:%s,user_id:%s,id:%s,err:%+v", where, ctx.Auth.TenantID, ctx.Auth.UserID, id, err.Error())
			return dispatcher.Fail[*attunev1.SavedAuditLogViewResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to save saved audit log view")
		}
	}
	return dispatcher.OK(ptrext.Of(attunev1.SavedAuditLogViewResponse{View: toProtoSavedView(view)}))
}

func fromProtoSavedViewState(state *attunev1.AuditLogViewState) auditlogviewsvc.State {
	if state == nil {
		return auditlogviewsvc.State{}
	}
	out := auditlogviewsvc.State{
		Actions:          append([]string{}, state.GetActions()...),
		ActorType:        state.GetActorType(),
		ActorID:          state.GetActorId(),
		TargetType:       state.GetTargetType(),
		TargetID:         state.GetTargetId(),
		From:             state.GetFrom(),
		To:               state.GetTo(),
		LocalQuery:       state.GetLocalQuery(),
		InspectedEntryID: state.GetInspectedEntryId(),
	}
	return out
}

func toProtoSavedView(view *auditlogviewsvc.View) *attunev1.SavedAuditLogView {
	if view == nil {
		return nil
	}
	return ptrext.Of(attunev1.SavedAuditLogView{
		Id:        view.ID,
		Name:      view.Name,
		State:     toProtoSavedViewState(view.State),
		CreatedAt: view.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: view.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func toProtoSavedViewState(state auditlogviewsvc.State) *attunev1.AuditLogViewState {
	proto := ptrext.Of(attunev1.AuditLogViewState{
		Actions:    append([]string{}, state.Actions...),
		ActorType:  state.ActorType,
		ActorId:    state.ActorID,
		TargetType: state.TargetType,
		TargetId:   state.TargetID,
		From:       state.From,
		To:         state.To,
		LocalQuery: state.LocalQuery,
	})
	if state.InspectedEntryID != "" {
		proto.InspectedEntryId = ptrext.Of(state.InspectedEntryID)
	}
	return proto
}
