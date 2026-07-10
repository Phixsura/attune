// SPDX-License-Identifier: Apache-2.0

// Package portal implements public customer-facing portal endpoints.
package portal

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	pvrepo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	pvsvc "github.com/Phixsura/attune/internal/service/publicvisibility"
)

const publicRequestCacheControl = "no-store"

type service interface {
	GetPublicRequest(ctx context.Context, tenantSlug string, publicSlug string) (pvsvc.PublicRequest, error)
}

type Handler struct {
	service service
}

func NewHandler(service service) *Handler {
	return ptrext.Of(Handler{service: service})
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
		return portalError(err)
	}
	if result.NoIndex {
		ctx.SetHeader("X-Robots-Tag", "noindex")
	}
	return dispatcher.OK(publicRequestToProto(result))
}

func portalError(err error) (dispatcher.Result[*attunev1.PublicCustomerRequestDetail], error) {
	switch {
	case errors.Is(err, pvsvc.ErrNotFound), errors.Is(err, pvrepo.ErrNotFound):
		return dispatcher.Fail[*attunev1.PublicCustomerRequestDetail](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "public request not found")
	case errors.Is(err, pvsvc.ErrValidation):
		return dispatcher.Fail[*attunev1.PublicCustomerRequestDetail](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "invalid public request")
	default:
		return dispatcher.Fail[*attunev1.PublicCustomerRequestDetail](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "public request failed")
	}
}

func publicRequestToProto(result pvsvc.PublicRequest) *attunev1.PublicCustomerRequestDetail {
	return ptrext.Of(attunev1.PublicCustomerRequestDetail{
		Request: publicRequestSummaryToProto(result),
		Links:   []string{},
	})
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

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
