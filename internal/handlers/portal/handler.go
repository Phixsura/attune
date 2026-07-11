// SPDX-License-Identifier: Apache-2.0

// Package portal implements public customer-facing portal endpoints.
package portal

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	pvsvc "github.com/Phixsura/attune/internal/service/publicvisibility"
)

const publicRequestCacheControl = "no-store"

type service interface {
	ListPublicRequests(ctx context.Context, tenantSlug string, limit int, cursor string) (pvsvc.PublicRequestList, error)
	GetPublicRequest(ctx context.Context, tenantSlug string, publicSlug string) (pvsvc.PublicRequest, error)
	ListPublicRoadmap(ctx context.Context, tenantSlug string, limit int, cursor string) (pvsvc.PublicRequestList, error)
}

type Handler struct {
	service service
}

func NewHandler(service service) *Handler {
	return ptrext.Of(Handler{service: service})
}

func NoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", publicRequestCacheControl)
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) ListPublicCustomerRequests(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.ListPublicCustomerRequestsRequest,
) (dispatcher.Result[*attunev1.ListPublicCustomerRequestsResponse], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	if h.service == nil {
		return dispatcher.Fail[*attunev1.ListPublicCustomerRequestsResponse](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "portal not configured")
	}
	result, err := h.service.ListPublicRequests(ctx, req.GetTenantSlug(), int(req.GetLimit()), req.GetCursor())
	if err != nil {
		return portalError[*attunev1.ListPublicCustomerRequestsResponse](err)
	}
	if result.NoIndex {
		ctx.SetHeader("X-Robots-Tag", "noindex")
	}
	return dispatcher.OK(publicRequestListToProto(result))
}

func (h *Handler) GetPublicCustomerRequest(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.GetPublicCustomerRequestRequest,
) (dispatcher.Result[*attunev1.PublicCustomerRequestDetail], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	if h.service == nil {
		return dispatcher.Fail[*attunev1.PublicCustomerRequestDetail](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "portal not configured")
	}
	result, err := h.service.GetPublicRequest(ctx, req.GetTenantSlug(), req.GetPublicSlug())
	if err != nil {
		return portalError[*attunev1.PublicCustomerRequestDetail](err)
	}
	if result.NoIndex {
		ctx.SetHeader("X-Robots-Tag", "noindex")
	}
	return dispatcher.OK(publicRequestToProto(result))
}

func (h *Handler) ListPublicRoadmap(
	ctx *dispatcher.RequestContext[struct{}],
	req *attunev1.ListPublicRoadmapRequest,
) (dispatcher.Result[*attunev1.ListPublicRoadmapResponse], error) {
	ctx.SetHeader("Cache-Control", publicRequestCacheControl)
	if h.service == nil {
		return dispatcher.Fail[*attunev1.ListPublicRoadmapResponse](http.StatusNotImplemented, attunev1.ErrorCode_INTERNAL, "portal not configured")
	}
	result, err := h.service.ListPublicRoadmap(ctx, req.GetTenantSlug(), int(req.GetLimit()), req.GetCursor())
	if err != nil {
		return portalError[*attunev1.ListPublicRoadmapResponse](err)
	}
	if result.NoIndex {
		ctx.SetHeader("X-Robots-Tag", "noindex")
	}
	return dispatcher.OK(publicRoadmapToProto(result))
}

func portalError[Resp proto.Message](err error) (dispatcher.Result[Resp], error) {
	switch {
	case errors.Is(err, pvsvc.ErrNotFound), errors.Is(err, pvrepo.ErrNotFound):
		return dispatcher.Fail[Resp](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "public request not found")
	case errors.Is(err, pvsvc.ErrValidation):
		return dispatcher.Fail[Resp](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid public request")
	default:
		return dispatcher.Fail[Resp](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "public request failed")
	}
}

func publicRequestToProto(result pvsvc.PublicRequest) *attunev1.PublicCustomerRequestDetail {
	return ptrext.Of(attunev1.PublicCustomerRequestDetail{
		Request: publicRequestSummaryToProto(result),
		Links:   []string{},
	})
}

func publicRequestListToProto(result pvsvc.PublicRequestList) *attunev1.ListPublicCustomerRequestsResponse {
	out := ptrext.Of(attunev1.ListPublicCustomerRequestsResponse{
		Requests: make([]*attunev1.PublicCustomerRequestSummary, 0, len(result.Requests)),
	})
	for _, item := range result.Requests {
		out.Requests = append(out.Requests, publicRequestSummaryToProto(item))
	}
	if result.NextCursor != "" {
		out.NextCursor = ptrext.Of(result.NextCursor)
	}
	return out
}

func publicRoadmapToProto(result pvsvc.PublicRequestList) *attunev1.ListPublicRoadmapResponse {
	out := ptrext.Of(attunev1.ListPublicRoadmapResponse{})
	columnsByName := map[string]*attunev1.PublicRoadmapColumn{}
	for _, item := range result.Requests {
		name := item.Summary.RoadmapColumn
		column, ok := columnsByName[name]
		if !ok {
			column = ptrext.Of(attunev1.PublicRoadmapColumn{Name: name})
			columnsByName[name] = column
			out.Columns = append(out.Columns, column)
		}
		column.Requests = append(column.Requests, publicRequestSummaryToProto(item))
	}
	if result.NextCursor != "" {
		out.NextCursor = ptrext.Of(result.NextCursor)
	}
	return out
}

func publicRequestSummaryToProto(result pvsvc.PublicRequest) *attunev1.PublicCustomerRequestSummary {
	summary := ptrext.Of(attunev1.PublicCustomerRequestSummary{
		Id:            result.Summary.ID.String(),
		Slug:          result.Summary.PublicSlug,
		Title:         result.Summary.PublicTitle,
		Summary:       result.Summary.PublicSummary,
		State:         result.Summary.PublicState,
		RoadmapColumn: result.Summary.RoadmapColumn,
	})
	if result.Policy.ShowVoteCount {
		summary.VoteCount = ptrext.Of(uint32(nonNegative(result.Votes)))
	}
	if result.Policy.ShowCommentCount {
		summary.CommentCount = ptrext.Of(uint32(nonNegative(result.Comments)))
	}
	if result.Policy.ShowSubmitterDisplay && result.SubmitterDisplay != "" {
		summary.SubmittedByDisplay = ptrext.Of(result.SubmitterDisplay)
	}
	if !result.Policy.HidePublicTimestamps {
		summary.CreatedAt = optionalTime(result.Summary.CreatedAt)
		summary.UpdatedAt = optionalTime(result.Summary.UpdatedAt)
	}
	return summary
}

func optionalTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	return ptrext.Of(t.UTC().Format(time.RFC3339Nano))
}

func BindListCustomerRequests(r *http.Request, req *attunev1.ListPublicCustomerRequestsRequest) error {
	limit, cursor, err := bindPublicListQuery(r)
	if err != nil {
		return err
	}
	req.Limit = limit
	req.Cursor = cursor
	return nil
}

func BindListRoadmap(r *http.Request, req *attunev1.ListPublicRoadmapRequest) error {
	limit, cursor, err := bindPublicListQuery(r)
	if err != nil {
		return err
	}
	req.Limit = limit
	req.Cursor = cursor
	return nil
}

func bindPublicListQuery(r *http.Request) (uint32, string, error) {
	q := r.URL.Query()
	var limit uint32
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return 0, "", dispatcher.NewError(http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid public list limit")
		}
		limit = uint32(parsed)
	}
	return limit, strings.TrimSpace(q.Get("cursor")), nil
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
