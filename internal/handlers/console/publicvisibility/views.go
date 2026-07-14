// SPDX-License-Identifier: Apache-2.0

package publicvisibility

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	viewrepo "github.com/Phixsura/attune/internal/repo/publicvisibilityview"
	viewsvc "github.com/Phixsura/attune/internal/service/publicvisibilityview"
)

type savedViewService interface {
	List(ctx context.Context, tenantID, userID string) ([]viewsvc.View, error)
	Save(ctx context.Context, tenantID, userID string, input viewsvc.SaveInput) (*viewsvc.View, error)
	Delete(ctx context.Context, tenantID, userID, id, updatedBy string) error
}

func (h *Handler) SetSavedViewService(service savedViewService) {
	h.views = service
}

func (h *Handler) ListSavedViews(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.ListSavedPublicVisibilityViewsRequest,
) (dispatcher.Result[*attunev1.ListSavedPublicVisibilityViewsResponse], error) {
	if h.views == nil {
		return dispatcher.Fail[*attunev1.ListSavedPublicVisibilityViewsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "saved public visibility views are unavailable")
	}
	views, err := h.views.List(ctx, ctx.Auth.TenantID, ctx.Auth.UserID)
	if err != nil {
		return dispatcher.Fail[*attunev1.ListSavedPublicVisibilityViewsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list saved public visibility views")
	}
	items := make([]*attunev1.SavedPublicVisibilityView, 0, len(views))
	for _, view := range views {
		items = append(items, savedViewToProto(ptrext.Of(view)))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListSavedPublicVisibilityViewsResponse{Views: items}))
}

func (h *Handler) CreateSavedView(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.CreateSavedPublicVisibilityViewRequest,
) (dispatcher.Result[*attunev1.SavedPublicVisibilityViewResponse], error) {
	return h.saveSavedView(ctx, "", req.GetName(), req.GetState())
}

func (h *Handler) UpdateSavedView(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateSavedPublicVisibilityViewRequest,
) (dispatcher.Result[*attunev1.SavedPublicVisibilityViewResponse], error) {
	return h.saveSavedView(ctx, req.GetId(), req.GetName(), req.GetState())
}

func (h *Handler) DeleteSavedView(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.DeleteSavedPublicVisibilityViewRequest,
) (dispatcher.Result[*attunev1.DeleteSavedPublicVisibilityViewResponse], error) {
	if h.views == nil {
		return dispatcher.Fail[*attunev1.DeleteSavedPublicVisibilityViewResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "saved public visibility views are unavailable")
	}
	if err := h.views.Delete(ctx, ctx.Auth.TenantID, ctx.Auth.UserID, req.GetId(), ctx.Auth.UserID); err != nil {
		switch {
		case errors.Is(err, viewsvc.ErrValidation):
			return dispatcher.Fail[*attunev1.DeleteSavedPublicVisibilityViewResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
		case errors.Is(err, viewrepo.ErrNotFound):
			return dispatcher.Fail[*attunev1.DeleteSavedPublicVisibilityViewResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "saved view not found")
		default:
			return dispatcher.Fail[*attunev1.DeleteSavedPublicVisibilityViewResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to delete saved public visibility view")
		}
	}
	return dispatcher.OK(ptrext.Of(attunev1.DeleteSavedPublicVisibilityViewResponse{}))
}

func (h *Handler) saveSavedView(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	id string,
	name string,
	state *attunev1.PublicVisibilityViewState,
) (dispatcher.Result[*attunev1.SavedPublicVisibilityViewResponse], error) {
	if h.views == nil {
		return dispatcher.Fail[*attunev1.SavedPublicVisibilityViewResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "saved public visibility views are unavailable")
	}
	parsedState, err := savedViewStateFromProto(state)
	if err != nil {
		return dispatcher.Fail[*attunev1.SavedPublicVisibilityViewResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid saved view state")
	}
	view, err := h.views.Save(ctx, ctx.Auth.TenantID, ctx.Auth.UserID, viewsvc.SaveInput{
		ID:        id,
		Name:      name,
		State:     parsedState,
		UpdatedBy: ctx.Auth.UserID,
	})
	if err != nil {
		switch {
		case errors.Is(err, viewsvc.ErrValidation):
			return dispatcher.Fail[*attunev1.SavedPublicVisibilityViewResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
		case errors.Is(err, viewrepo.ErrConflict):
			return dispatcher.Fail[*attunev1.SavedPublicVisibilityViewResponse](http.StatusConflict, attunev1.ErrorCode_CONFLICT, "saved view name already exists")
		case errors.Is(err, viewrepo.ErrNotFound):
			return dispatcher.Fail[*attunev1.SavedPublicVisibilityViewResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "saved view not found")
		default:
			return dispatcher.Fail[*attunev1.SavedPublicVisibilityViewResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to save saved public visibility view")
		}
	}
	return dispatcher.OK(ptrext.Of(attunev1.SavedPublicVisibilityViewResponse{View: savedViewToProto(view)}))
}

func savedViewStateFromProto(state *attunev1.PublicVisibilityViewState) (viewsvc.State, error) {
	if state == nil {
		return viewsvc.State{}, nil
	}
	if !knownSurfacesProto(state.GetSurfaces()) {
		return viewsvc.State{}, errors.New("invalid public visibility surface")
	}
	return viewsvc.State{
		QueueView: state.GetQueueView(),
		Surfaces:  surfacesFromProto(state.GetSurfaces()),
	}, nil
}

func savedViewToProto(view *viewsvc.View) *attunev1.SavedPublicVisibilityView {
	if view == nil {
		return nil
	}
	return ptrext.Of(attunev1.SavedPublicVisibilityView{
		Id:        view.ID,
		Name:      view.Name,
		State:     savedViewStateToProto(view.State),
		CreatedAt: view.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: view.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func savedViewStateToProto(state viewsvc.State) *attunev1.PublicVisibilityViewState {
	return ptrext.Of(attunev1.PublicVisibilityViewState{
		QueueView: state.QueueView,
		Surfaces:  surfacesToProto(state.Surfaces),
	})
}

func surfacesToProto(values []pvrepo.Surface) []attunev1.PublicSurface {
	out := make([]attunev1.PublicSurface, 0, len(values))
	for _, value := range values {
		out = append(out, surfaceToProto(value))
	}
	return out
}
