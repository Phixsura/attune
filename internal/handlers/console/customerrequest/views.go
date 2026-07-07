// SPDX-License-Identifier: Apache-2.0

package customerrequest

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
	crrepo "github.com/Phixsura/attune/internal/repo/customerrequest"
	viewrepo "github.com/Phixsura/attune/internal/repo/customerrequestview"
	viewsvc "github.com/Phixsura/attune/internal/service/customerrequestview"
)

type savedViewService interface {
	List(ctx context.Context, tenantID, userID string) ([]viewsvc.View, error)
	Save(ctx context.Context, tenantID, userID string, input viewsvc.SaveInput) (*viewsvc.View, error)
	Delete(ctx context.Context, tenantID, userID, id, updatedBy string) error
}

func (h *Handler) ListSavedViews(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	_ *attunev1.ListCustomerRequestSavedViewsRequest,
) (dispatcher.Result[*attunev1.ListCustomerRequestSavedViewsResponse], error) {
	const where = "console.customerrequest.ListSavedViews"
	if h.views == nil {
		return dispatcher.Fail[*attunev1.ListCustomerRequestSavedViewsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "saved customer request views are unavailable")
	}
	views, err := h.views.List(ctx, ctx.Auth.TenantID, ctx.Auth.UserID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list failed,tenant_id:%s,user_id:%s,err:%+v", where, ctx.Auth.TenantID, ctx.Auth.UserID, err.Error())
		return dispatcher.Fail[*attunev1.ListCustomerRequestSavedViewsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list saved customer request views")
	}
	items := make([]*attunev1.CustomerRequestSavedView, 0, len(views))
	for _, view := range views {
		items = append(items, savedViewToProto(ptrext.Of(view)))
	}
	return dispatcher.OK(ptrext.Of(attunev1.ListCustomerRequestSavedViewsResponse{Views: items}))
}

func (h *Handler) CreateSavedView(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.CreateCustomerRequestSavedViewRequest,
) (dispatcher.Result[*attunev1.CustomerRequestSavedViewResponse], error) {
	return h.saveSavedView(ctx, "", req.GetName(), req.GetState())
}

func (h *Handler) UpdateSavedView(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.UpdateCustomerRequestSavedViewRequest,
) (dispatcher.Result[*attunev1.CustomerRequestSavedViewResponse], error) {
	return h.saveSavedView(ctx, req.GetId(), req.GetName(), req.GetState())
}

func (h *Handler) DeleteSavedView(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.DeleteCustomerRequestSavedViewRequest,
) (dispatcher.Result[*attunev1.DeleteCustomerRequestSavedViewResponse], error) {
	const where = "console.customerrequest.DeleteSavedView"
	if h.views == nil {
		return dispatcher.Fail[*attunev1.DeleteCustomerRequestSavedViewResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "saved customer request views are unavailable")
	}
	if err := h.views.Delete(ctx, ctx.Auth.TenantID, ctx.Auth.UserID, req.GetId(), ctx.Auth.UserID); err != nil {
		switch {
		case errors.Is(err, viewsvc.ErrValidation):
			return dispatcher.Fail[*attunev1.DeleteCustomerRequestSavedViewResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
		case errors.Is(err, viewrepo.ErrNotFound):
			return dispatcher.Fail[*attunev1.DeleteCustomerRequestSavedViewResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "saved view not found")
		default:
			logext.Errorf(ctx, "[%s] delete failed,tenant_id:%s,user_id:%s,id:%s,err:%+v", where, ctx.Auth.TenantID, ctx.Auth.UserID, req.GetId(), err.Error())
			return dispatcher.Fail[*attunev1.DeleteCustomerRequestSavedViewResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to delete saved customer request view")
		}
	}
	return dispatcher.OK(ptrext.Of(attunev1.DeleteCustomerRequestSavedViewResponse{}))
}

func (h *Handler) saveSavedView(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	id string,
	name string,
	state *attunev1.CustomerRequestSavedViewState,
) (dispatcher.Result[*attunev1.CustomerRequestSavedViewResponse], error) {
	const where = "console.customerrequest.saveSavedView"
	if h.views == nil {
		return dispatcher.Fail[*attunev1.CustomerRequestSavedViewResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "saved customer request views are unavailable")
	}
	parsedState, err := savedViewStateFromProto(state)
	if err != nil {
		return dispatcher.Fail[*attunev1.CustomerRequestSavedViewResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid saved view state")
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
			return dispatcher.Fail[*attunev1.CustomerRequestSavedViewResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, err.Error())
		case errors.Is(err, viewrepo.ErrConflict):
			return dispatcher.Fail[*attunev1.CustomerRequestSavedViewResponse](http.StatusConflict, attunev1.ErrorCode_CONFLICT, "saved view name already exists")
		case errors.Is(err, viewrepo.ErrNotFound):
			return dispatcher.Fail[*attunev1.CustomerRequestSavedViewResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "saved view not found")
		default:
			logext.Errorf(ctx, "[%s] save failed,tenant_id:%s,user_id:%s,id:%s,err:%+v", where, ctx.Auth.TenantID, ctx.Auth.UserID, id, err.Error())
			return dispatcher.Fail[*attunev1.CustomerRequestSavedViewResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to save saved customer request view")
		}
	}
	return dispatcher.OK(ptrext.Of(attunev1.CustomerRequestSavedViewResponse{View: savedViewToProto(view)}))
}

func savedViewStateFromProto(state *attunev1.CustomerRequestSavedViewState) (viewsvc.State, error) {
	if state == nil {
		return viewsvc.State{}, nil
	}
	statuses, err := statusesFromProto(state.GetStatus())
	if err != nil {
		return viewsvc.State{}, err
	}
	priorities, err := prioritiesFromProto(state.GetPriority())
	if err != nil {
		return viewsvc.State{}, err
	}
	out := viewsvc.State{
		Query:         state.GetQ(),
		Statuses:      statuses,
		Priorities:    priorities,
		OwnerMemberID: state.GetOwnerMemberId(),
		Visibility:    visibilityFromProto(state.GetVisibility()),
		Sort:          sortFromProto(state.GetSort()),
		Direction:     directionFromProto(state.GetDirection()),
	}
	if state.FeedbackId != nil {
		out.FeedbackID = state.GetFeedbackId()
	}
	return out, nil
}

func savedViewToProto(view *viewsvc.View) *attunev1.CustomerRequestSavedView {
	if view == nil {
		return nil
	}
	return ptrext.Of(attunev1.CustomerRequestSavedView{
		Id:        view.ID,
		Name:      view.Name,
		State:     savedViewStateToProto(view.State),
		CreatedAt: view.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: view.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

func savedViewStateToProto(state viewsvc.State) *attunev1.CustomerRequestSavedViewState {
	out := ptrext.Of(attunev1.CustomerRequestSavedViewState{
		Q:          state.Query,
		Status:     statusesToProto(state.Statuses),
		Priority:   prioritiesToProto(state.Priorities),
		Visibility: visibilityToProto(state.Visibility),
		Sort:       sortToProto(state.Sort),
		Direction:  directionToProto(state.Direction),
	})
	if state.OwnerMemberID != "" {
		out.OwnerMemberId = ptrext.Of(state.OwnerMemberID)
	}
	if state.FeedbackID > 0 {
		out.FeedbackId = ptrext.Of(state.FeedbackID)
	}
	return out
}

func statusesToProto(statuses []crrepo.Status) []attunev1.CustomerRequestStatus {
	out := make([]attunev1.CustomerRequestStatus, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, statusToProto(status))
	}
	return out
}

func prioritiesToProto(priorities []crrepo.Priority) []attunev1.CustomerRequestPriority {
	out := make([]attunev1.CustomerRequestPriority, 0, len(priorities))
	for _, priority := range priorities {
		out = append(out, priorityToProto(priority))
	}
	return out
}

func visibilityToProto(visibility crrepo.Visibility) attunev1.CustomerRequestVisibility {
	switch visibility {
	case crrepo.VisibilityMerged:
		return attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_MERGED
	case crrepo.VisibilityArchived:
		return attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_ARCHIVED
	case crrepo.VisibilityAll:
		return attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_ALL
	default:
		return attunev1.CustomerRequestVisibility_CUSTOMER_REQUEST_VISIBILITY_ACTIVE
	}
}

func sortToProto(sort crrepo.Sort) attunev1.CustomerRequestSort {
	switch sort {
	case crrepo.SortCustomerCount:
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_CUSTOMER_COUNT
	case crrepo.SortSupportingFeedbackCount:
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_SUPPORTING_FEEDBACK_COUNT
	case crrepo.SortLatestFeedbackAt:
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_LATEST_FEEDBACK_AT
	case crrepo.SortPriority:
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_PRIORITY
	case crrepo.SortRevenueImpact:
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_REVENUE_IMPACT
	case crrepo.SortDecisionScore:
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_DECISION_SCORE
	case crrepo.SortDeliveryHealth:
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_DELIVERY_HEALTH
	default:
		return attunev1.CustomerRequestSort_CUSTOMER_REQUEST_SORT_UPDATED_AT
	}
}

func directionToProto(direction crrepo.Direction) attunev1.SortDirection {
	if direction == crrepo.DirectionAsc {
		return attunev1.SortDirection_SORT_DIRECTION_ASC
	}
	return attunev1.SortDirection_SORT_DIRECTION_DESC
}
