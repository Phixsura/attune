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

type searchOperations interface {
	RecordSearchRun(ctx context.Context, row repofeedback.SearchRunInsert) error
	RecordSearchResultEvent(ctx context.Context, row repofeedback.SearchResultEventInsert) error
	SearchQualityDashboard(ctx context.Context, opts repofeedback.SearchQualityQueryOpts) (*repofeedback.SearchQualityDashboard, error)
}

// SearchHandler handles POST /fb/v1/console/feedback/search.
type SearchHandler struct {
	service        searchService
	operations     searchOperations
	tagAssignments tagAssignmentReader
	workflowStates workflowStateReader
}

// NewSearchHandler creates a new semantic search handler.
func NewSearchHandler(service semanticsearch.Service) *SearchHandler {
	return ptrext.Of(SearchHandler{service: service})
}

// SetTagAssignments wires tag hydration for semantic search results.
func (h *SearchHandler) SetTagAssignments(r tagAssignmentReader) { h.tagAssignments = r }

// SetWorkflowStates wires workflow state hydration for semantic search results.
func (h *SearchHandler) SetWorkflowStates(r workflowStateReader) { h.workflowStates = r }

// SetSearchOperations wires search quality telemetry and operations endpoints.
func (h *SearchHandler) SetSearchOperations(r searchOperations) { h.operations = r }

// Search performs semantic search on feedback.
// Validates input, converts proto to service request, handles errors.
func (h *SearchHandler) Search(
	ctx *dispatcher.RequestContext[*session.AuthCtx],
	req *attunev1.SemanticSearchRequest,
) (dispatcher.Result[*attunev1.SemanticSearchResponse], error) {
	const where = "console.SearchHandler.Search"
	auth := ctx.Auth

	query, err := semanticsearch.NormalizeQuery(req.GetQ())
	if errors.Is(err, semanticsearch.ErrEmptyQuery) {
		return dispatcher.Fail[*attunev1.SemanticSearchResponse](
			http.StatusBadRequest,
			attunev1.ErrorCode_VALIDATION,
			"search query (q) is required",
		)
	}
	if errors.Is(err, semanticsearch.ErrQueryTooLong) {
		return dispatcher.Fail[*attunev1.SemanticSearchResponse](
			http.StatusBadRequest,
			attunev1.ErrorCode_VALIDATION,
			"search query must be at most 512 characters",
		)
	}
	if errors.Is(err, semanticsearch.ErrInvalidQuery) {
		return dispatcher.Fail[*attunev1.SemanticSearchResponse](
			http.StatusBadRequest,
			attunev1.ErrorCode_VALIDATION,
			"search query contains unsupported control characters",
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
		where, auth.TenantID, len([]rune(query)), req.GetLimit())

	// Build service request.
	svcReq := ptrext.Of(semanticsearch.SearchRequest{
		TenantID:       auth.TenantID,
		Query:          query,
		Limit:          int(req.GetLimit()),
		MinSimilarity:  float64(req.GetMinSimilarity()),
		SemanticWeight: float64(req.GetSemanticWeight()),
		KeywordWeight:  float64(req.GetKeywordWeight()),
		Filter:         protoFilterToRepoFilter(req.GetFilter()),
	})

	// Call service.
	runID := newSearchRunID()
	startedAt := time.Now()
	resp, err := h.service.Search(ctx, svcReq)
	latency := time.Since(startedAt)
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
		if errors.Is(err, semanticsearch.ErrQueryTooLong) {
			return dispatcher.Fail[*attunev1.SemanticSearchResponse](
				http.StatusBadRequest,
				attunev1.ErrorCode_VALIDATION,
				"search query must be at most 512 characters",
			)
		}
		if errors.Is(err, semanticsearch.ErrInvalidQuery) {
			return dispatcher.Fail[*attunev1.SemanticSearchResponse](
				http.StatusBadRequest,
				attunev1.ErrorCode_VALIDATION,
				"search query contains unsupported control characters",
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
	protoResp.RunId = runID
	h.recordSearchRun(ctx, auth, req, query, runID, latency, resp)
	searchRows := searchResponseRows(resp)
	protoItems := semanticSearchProtoFeedbacks(protoResp)
	enrichFeedbackItemsWithTags(ctx, where, auth.TenantID, searchRows, protoItems, h.tagAssignments)
	enrichFeedbackItemsWithWorkflowState(ctx, where, auth.TenantID, searchRows, protoItems, h.workflowStates)

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
	appendProtoAttrFilters(filter, pf.GetAttrs())
	applyProtoScalarFilters(filter, pf)
	return filter
}

func appendProtoAttrFilters(filter *repofeedback.FeedbackFilter, attrs []*attunev1.AttrFilter) {
	for _, attr := range attrs {
		if attr != nil && attr.GetDim() != "" && attr.GetValue() != "" {
			filter.Attrs = append(filter.Attrs, repofeedback.AttrFilter{
				Dim:   attr.GetDim(),
				Value: attr.GetValue(),
			})
		}
	}
}

func applyProtoScalarFilters(filter *repofeedback.FeedbackFilter, pf *attunev1.FeedbackFilter) {
	if pf.Urgent != nil {
		u := pf.GetUrgent()
		filter.Urgent = ptrext.Of(u)
	}
	if pf.TagId != nil && pf.GetTagId() != "" {
		filter.TagIDs = []string{pf.GetTagId()}
	}
	if pf.WorkflowStateId != nil && pf.GetWorkflowStateId() != "" {
		filter.WorkflowStateIDs = []string{pf.GetWorkflowStateId()}
	}
	if pf.WorkflowCategory != nil && pf.GetWorkflowCategory() != "" {
		cat := pf.GetWorkflowCategory()
		filter.WorkflowCategory = ptrext.Of(cat)
	}
	if pf.EnrichmentStatus != nil && pf.GetEnrichmentStatus() != "" {
		status := pf.GetEnrichmentStatus()
		filter.EnrichmentStatus = ptrext.Of(status)
	}
	if pf.TerminalFailedOnly != nil {
		filter.TerminalFailedOnly = ptrext.Of(pf.GetTerminalFailedOnly())
	}
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
			Feedback:       searchFeedbackToProto(hit.Feedback),
			Similarity:     float32(hit.Similarity),
			KeywordScore:   float32(hit.KeywordScore),
			MatchType:      hit.MatchType,
			SemanticRank:   int32(hit.SemanticRank),
			LexicalRank:    int32(hit.LexicalRank),
			FusedScore:     float32(hit.FusedScore),
			Evidence:       searchEvidenceToProto(hit.Evidence),
			RankingSignals: hit.RankingSignals,
		}))
	}

	protoResp := ptrext.Of(attunev1.SemanticSearchResponse{
		Hits:                hits,
		EmbeddingModel:      resp.EmbeddingModel,
		TotalWithEmbeddings: int32(resp.TotalWithEmbeddings),
		UsedKeywordFallback: resp.UsedKeywordFallback,
		RankingVersion:      resp.RankingVersion,
		Coverage:            searchCoverageToProto(resp.Coverage),
	})
	if resp.FallbackReason != "" {
		protoResp.FallbackReason = ptrext.Of(resp.FallbackReason)
	}
	return protoResp
}

func searchEvidenceToProto(items []semanticsearch.SearchEvidence) []*attunev1.SearchEvidence {
	if len(items) == 0 {
		return nil
	}
	out := make([]*attunev1.SearchEvidence, 0, len(items))
	for _, item := range items {
		if item.Field == "" && item.Snippet == "" && item.Reason == "" {
			continue
		}
		out = append(out, ptrext.Of(attunev1.SearchEvidence{
			Field:   item.Field,
			Snippet: item.Snippet,
			Reason:  item.Reason,
		}))
	}
	return out
}

func searchCoverageToProto(coverage *semanticsearch.SearchCoverage) *attunev1.SearchCoverage {
	if coverage == nil {
		return nil
	}
	return ptrext.Of(attunev1.SearchCoverage{
		TotalLiveFeedback:   int32(coverage.TotalLiveFeedback),
		TotalWithEmbeddings: int32(coverage.TotalWithEmbeddings),
		EmbeddingModel:      coverage.EmbeddingModel,
	})
}

// searchFeedbackToProto converts repo SearchFeedback to proto Feedback.
func searchFeedbackToProto(fb *repofeedback.SearchFeedback) *attunev1.Feedback {
	if fb == nil {
		return nil
	}
	return toProtoFeedback(searchFeedbackToConsoleListRow(fb))
}

func searchFeedbackToConsoleListRow(fb *repofeedback.SearchFeedback) repofeedback.ConsoleListRow {
	row := repofeedback.ConsoleListRow{
		ID:                               fb.ID,
		Content:                          fb.Content,
		Source:                           fb.Source,
		Type:                             fb.Type,
		UserID:                           fb.UserID,
		Language:                         fb.Language,
		PageURL:                          fb.PageURL,
		EnrichedTitle:                    fb.EnrichedTitle,
		EnrichedDisplayTitle:             fb.EnrichedDisplayTitle,
		EnrichedDisplayLocale:            fb.EnrichedDisplayLocale,
		EnrichedAttrs:                    fb.EnrichedAttrs,
		IsUrgent:                         fb.IsUrgent,
		ClassificationConfidence:         fb.ClassificationConfidence,
		EnrichmentStatus:                 fb.EnrichmentStatus,
		CreatedAt:                        fb.CreatedAt,
		WorkflowStateID:                  fb.WorkflowStateID,
		EnrichmentAttempts:               fb.EnrichmentAttempts,
		EnrichmentNextRetryAt:            fb.EnrichmentNextRetryAt,
		TerminalFailureReasonClass:       fb.TerminalFailureReasonClass,
		TerminalFailureModel:             fb.TerminalFailureModel,
		TerminalFailureChannelID:         fb.TerminalFailureChannelID,
		TerminalFailureChannelName:       fb.TerminalFailureChannelName,
		TerminalFailureConfigFingerprint: fb.TerminalFailureConfigFingerprint,
		TerminalFailurePromptVersion:     fb.TerminalFailurePromptVersion,
	}
	return row
}

func searchResponseRows(resp *semanticsearch.SearchResponse) []repofeedback.ConsoleListRow {
	if resp == nil {
		return nil
	}
	rows := make([]repofeedback.ConsoleListRow, 0, len(resp.Hits))
	for _, hit := range resp.Hits {
		if hit == nil || hit.Feedback == nil {
			continue
		}
		rows = append(rows, searchFeedbackToConsoleListRow(hit.Feedback))
	}
	return rows
}

func semanticSearchProtoFeedbacks(resp *attunev1.SemanticSearchResponse) []*attunev1.Feedback {
	if resp == nil {
		return nil
	}
	items := make([]*attunev1.Feedback, 0, len(resp.GetHits()))
	for _, hit := range resp.GetHits() {
		if hit == nil || hit.GetFeedback() == nil {
			continue
		}
		items = append(items, hit.GetFeedback())
	}
	return items
}
