// SPDX-License-Identifier: Apache-2.0

package feedback

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
	repofeedback "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/service/semanticsearch"
)

// searchService is the interface for semantic search.
type searchService interface {
	Search(ctx context.Context, req *semanticsearch.SearchRequest) (*semanticsearch.SearchResponse, error)
}

// SearchHandler handles POST /fb/v1/console/feedback/search.
type SearchHandler struct {
	service searchService
}

// NewSearchHandler creates a new semantic search handler.
func NewSearchHandler(service semanticsearch.Service) *SearchHandler {
	return ptrext.Of(SearchHandler{service: service})
}

// Search performs semantic search on feedback.
// Validates input, converts proto to service request, handles errors.
func (h *SearchHandler) Search(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.SemanticSearchRequest,
) (dispatcher.Result[*attunev1.SemanticSearchResponse], error) {
	const where = "console.SearchHandler.Search"
	auth := ctx.Auth

	// Validate query is non-empty.
	if req.GetQ() == "" {
		return dispatcher.Fail[*attunev1.SemanticSearchResponse](
			http.StatusBadRequest,
			attunev1.ErrorCode_VALIDATION,
			"search query (q) is required",
		)
	}

	// Validate limit if provided.
	if req.Limit != nil && req.GetLimit() > 100 {
		return dispatcher.Fail[*attunev1.SemanticSearchResponse](
			http.StatusBadRequest,
			attunev1.ErrorCode_VALIDATION,
			"limit must be <= 100",
		)
	}

	logext.Infof(ctx, "[%s] start,tenant_id:%s,q_len:%d,limit:%d",
		where, auth.TenantID, len(req.GetQ()), req.GetLimit())

	// Build service request.
	svcReq := ptrext.Of(semanticsearch.SearchRequest{
		TenantID:       auth.TenantID,
		Query:          req.GetQ(),
		Limit:          int(req.GetLimit()),
		MinSimilarity:  float64(req.GetMinSimilarity()),
		SemanticWeight: float64(req.GetSemanticWeight()),
		KeywordWeight:  float64(req.GetKeywordWeight()),
		Filter:         protoFilterToRepoFilter(req.GetFilter()),
	})

	// Call service.
	resp, err := h.service.Search(ctx, svcReq)
	if err != nil {
		if errors.Is(err, semanticsearch.ErrRateLimited) {
			logext.Infof(ctx, "[%s] rate limited,tenant_id:%s", where, auth.TenantID)
			ctx.SetHeader("Retry-After", "60")
			return dispatcher.Fail[*attunev1.SemanticSearchResponse](
				http.StatusTooManyRequests,
				attunev1.ErrorCode_RATE_LIMITED,
				"search rate limit exceeded, please try again later",
			)
		}
		if errors.Is(err, semanticsearch.ErrEmptyQuery) {
			return dispatcher.Fail[*attunev1.SemanticSearchResponse](
				http.StatusBadRequest,
				attunev1.ErrorCode_VALIDATION,
				"search query is empty",
			)
		}
		logext.Errorf(ctx, "[%s] search failed,tenant_id:%s,err:%+v",
			where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.SemanticSearchResponse](
			http.StatusInternalServerError,
			attunev1.ErrorCode_INTERNAL,
			"search failed",
		)
	}

	// Convert service response to proto.
	protoResp := serviceResponseToProto(resp)

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,hits:%d,fallback:%v",
		where, auth.TenantID, len(protoResp.GetHits()), protoResp.GetUsedKeywordFallback())

	return dispatcher.OK(protoResp)
}

// protoFilterToRepoFilter converts proto FeedbackFilter to repo FeedbackFilter.
func protoFilterToRepoFilter(pf *attunev1.FeedbackFilter) *repofeedback.FeedbackFilter {
	if pf == nil {
		return nil
	}

	filter := ptrext.Of(repofeedback.FeedbackFilter{
		Q: pf.GetQ(),
	})

	// Convert attrs.
	for _, attr := range pf.GetAttrs() {
		if attr != nil && attr.GetDim() != "" && attr.GetValue() != "" {
			filter.Attrs = append(filter.Attrs, repofeedback.AttrFilter{
				Dim:   attr.GetDim(),
				Value: attr.GetValue(),
			})
		}
	}

	// Convert urgent.
	if pf.Urgent != nil {
		u := pf.GetUrgent()
		filter.Urgent = ptrext.Of(u)
	}

	// Convert tag ID.
	if pf.TagId != nil && pf.GetTagId() != "" {
		filter.TagIDs = []string{pf.GetTagId()}
	}

	// Convert workflow state ID.
	if pf.WorkflowStateId != nil && pf.GetWorkflowStateId() != "" {
		filter.WorkflowStateIDs = []string{pf.GetWorkflowStateId()}
	}

	// Convert workflow category.
	if pf.WorkflowCategory != nil && pf.GetWorkflowCategory() != "" {
		cat := pf.GetWorkflowCategory()
		filter.WorkflowCategory = ptrext.Of(cat)
	}

	return filter
}

// serviceResponseToProto converts service SearchResponse to proto SemanticSearchResponse.
func serviceResponseToProto(resp *semanticsearch.SearchResponse) *attunev1.SemanticSearchResponse {
	if resp == nil {
		return ptrext.Of(attunev1.SemanticSearchResponse{})
	}

	hits := make([]*attunev1.SemanticSearchHit, 0, len(resp.Hits))
	for _, hit := range resp.Hits {
		if hit == nil || hit.Feedback == nil {
			continue
		}
		hits = append(hits, ptrext.Of(attunev1.SemanticSearchHit{
			Feedback:     searchFeedbackToProto(hit.Feedback),
			Similarity:   float32(hit.Similarity),
			KeywordScore: float32(hit.KeywordScore),
			MatchType:    hit.MatchType,
		}))
	}

	return ptrext.Of(attunev1.SemanticSearchResponse{
		Hits:                hits,
		EmbeddingModel:      resp.EmbeddingModel,
		TotalWithEmbeddings: int32(resp.TotalWithEmbeddings),
		UsedKeywordFallback: resp.UsedKeywordFallback,
	})
}

// searchFeedbackToProto converts repo SearchFeedback to proto Feedback.
func searchFeedbackToProto(fb *repofeedback.SearchFeedback) *attunev1.Feedback {
	if fb == nil {
		return nil
	}
	return ptrext.Of(attunev1.Feedback{
		Id:                   fb.ID,
		Content:              fb.Content,
		Source:               fb.Source,
		EnrichedTitle:        nullableString(fb.EnrichedTitle),
		EnrichedDisplayTitle: nullableString(fb.EnrichedDisplayTitle),
		IsUrgent:             fb.IsUrgent,
		EnrichmentStatus:     fb.EnrichmentStatus,
		CreatedAt:            fb.CreatedAt.UTC().Format(time.RFC3339),
	})
}
