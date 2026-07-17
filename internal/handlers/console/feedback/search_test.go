// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test fixtures build proto-request pointers

package feedback

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repofeedback "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	"github.com/Phixsura/attune/internal/service/semanticsearch"
)

func TestProtoFilterToRepoFilter_NilInput(t *testing.T) {
	t.Parallel()
	result := protoFilterToRepoFilter(nil)
	assert.Nil(t, result)
}

func TestProtoFilterToRepoFilter_EmptyFilter(t *testing.T) {
	t.Parallel()
	pf := ptrext.Of(attunev1.FeedbackFilter{})
	result := protoFilterToRepoFilter(pf)

	require.NotNil(t, result)
	assert.Empty(t, result.Attrs)
	assert.Nil(t, result.Urgent)
	assert.Empty(t, result.TagIDs)
	assert.Empty(t, result.WorkflowStateIDs)
	assert.Nil(t, result.WorkflowCategory)
	assert.Nil(t, result.EnrichmentStatus)
	assert.Nil(t, result.TerminalFailedOnly)
}

func TestProtoFilterToRepoFilter_FullFilter(t *testing.T) {
	t.Parallel()
	pf := ptrext.Of(attunev1.FeedbackFilter{
		Attrs: []*attunev1.AttrFilter{
			{Dim: "severity", Value: "high"},
			{Dim: "labels", Value: "bug"},
		},
		Urgent:             ptrext.Of(true),
		Q:                  ptrext.Of("search query"),
		TagId:              ptrext.Of("tag-123"),
		WorkflowStateId:    ptrext.Of("state-456"),
		WorkflowCategory:   ptrext.Of("open"),
		EnrichmentStatus:   ptrext.Of("failed"),
		TerminalFailedOnly: ptrext.Of(true),
	})
	result := protoFilterToRepoFilter(pf)

	require.NotNil(t, result)
	assert.Len(t, result.Attrs, 2)
	assert.Equal(t, "severity", result.Attrs[0].Dim)
	assert.Equal(t, "high", result.Attrs[0].Value)
	assert.Equal(t, "labels", result.Attrs[1].Dim)
	assert.Equal(t, "bug", result.Attrs[1].Value)
	require.NotNil(t, result.Urgent)
	assert.True(t, ptrext.Indirect(result.Urgent))
	assert.Equal(t, "search query", result.Q)
	assert.Equal(t, []string{"tag-123"}, result.TagIDs)
	assert.Equal(t, []string{"state-456"}, result.WorkflowStateIDs)
	require.NotNil(t, result.WorkflowCategory)
	assert.Equal(t, "open", ptrext.Indirect(result.WorkflowCategory))
	require.NotNil(t, result.EnrichmentStatus)
	assert.Equal(t, "failed", ptrext.Indirect(result.EnrichmentStatus))
	require.NotNil(t, result.TerminalFailedOnly)
	assert.True(t, ptrext.Indirect(result.TerminalFailedOnly))
}

func TestProtoFilterToRepoFilter_SkipsEmptyAttrs(t *testing.T) {
	t.Parallel()
	pf := ptrext.Of(attunev1.FeedbackFilter{
		Attrs: []*attunev1.AttrFilter{
			{Dim: "", Value: "value"},      // empty dim
			{Dim: "dim", Value: ""},        // empty value
			nil,                            // nil attr
			{Dim: "valid", Value: "value"}, // valid
		},
	})
	result := protoFilterToRepoFilter(pf)

	require.NotNil(t, result)
	assert.Len(t, result.Attrs, 1)
	assert.Equal(t, "valid", result.Attrs[0].Dim)
}

func TestProtoFilterToRepoFilter_SkipsEmptyScalarFilters(t *testing.T) {
	t.Parallel()
	pf := ptrext.Of(attunev1.FeedbackFilter{
		TagId:            ptrext.Of(""),
		WorkflowStateId:  ptrext.Of(""),
		WorkflowCategory: ptrext.Of(""),
		EnrichmentStatus: ptrext.Of(""),
	})

	result := protoFilterToRepoFilter(pf)

	require.NotNil(t, result)
	assert.Empty(t, result.TagIDs)
	assert.Empty(t, result.WorkflowStateIDs)
	assert.Nil(t, result.WorkflowCategory)
	assert.Nil(t, result.EnrichmentStatus)
}

func TestServiceResponseToProto_NilInput(t *testing.T) {
	t.Parallel()
	result := serviceResponseToProto(nil)
	require.NotNil(t, result)
	assert.Empty(t, result.GetHits())
	assert.Empty(t, result.GetEmbeddingModel())
	assert.Zero(t, result.GetTotalWithEmbeddings())
	assert.False(t, result.GetUsedKeywordFallback())
}

func TestServiceResponseToProto_EmptyResponse(t *testing.T) {
	t.Parallel()
	resp := ptrext.Of(semanticsearch.SearchResponse{})
	result := serviceResponseToProto(resp)

	require.NotNil(t, result)
	assert.Empty(t, result.GetHits())
}

func TestServiceResponseToProto_WithHits(t *testing.T) {
	t.Parallel()
	now := time.Now()
	resp := ptrext.Of(semanticsearch.SearchResponse{
		Hits: []*semanticsearch.SearchHit{
			{
				Feedback: ptrext.Of(repofeedback.SearchFeedback{
					ID:               1,
					Content:          "test content",
					Source:           "widget",
					EnrichedTitle:    "Test Title",
					IsUrgent:         true,
					EnrichmentStatus: "done",
					CreatedAt:        now,
				}),
				Similarity:   0.95,
				KeywordScore: 0.8,
				MatchType:    "hybrid",
				SemanticRank: 1,
				LexicalRank:  2,
				FusedScore:   0.0162,
				Evidence: []semanticsearch.SearchEvidence{
					{Field: "content", Snippet: "test content", Reason: "lexical_match"},
				},
				RankingSignals: []string{"semantic", "lexical", "rrf"},
			},
			nil, // should be skipped
			{
				Feedback: nil, // should be skipped
			},
		},
		EmbeddingModel:      "text-embedding-3-small",
		TotalWithEmbeddings: 100,
		UsedKeywordFallback: false,
		RankingVersion:      semanticsearch.RankingVersion,
		Coverage: ptrext.Of(semanticsearch.SearchCoverage{
			TotalLiveFeedback:   120,
			TotalWithEmbeddings: 100,
			EmbeddingModel:      "text-embedding-3-small",
		}),
	})
	result := serviceResponseToProto(resp)

	require.NotNil(t, result)
	assert.Len(t, result.GetHits(), 1)

	hit := result.GetHits()[0]
	assert.Equal(t, int64(1), hit.GetFeedback().GetId())
	assert.Equal(t, "test content", hit.GetFeedback().GetContent())
	assert.Equal(t, "widget", hit.GetFeedback().GetSource())
	assert.Equal(t, "Test Title", hit.GetFeedback().GetEnrichedTitle())
	assert.True(t, hit.GetFeedback().GetIsUrgent())
	assert.Equal(t, "done", hit.GetFeedback().GetEnrichmentStatus())
	assert.InDelta(t, float32(0.95), hit.GetSimilarity(), 0.001)
	assert.InDelta(t, float32(0.8), hit.GetKeywordScore(), 0.001)
	assert.Equal(t, "hybrid", hit.GetMatchType())
	assert.Equal(t, int32(1), hit.GetSemanticRank())
	assert.Equal(t, int32(2), hit.GetLexicalRank())
	assert.InDelta(t, float32(0.0162), hit.GetFusedScore(), 0.0001)
	require.Len(t, hit.GetEvidence(), 1)
	assert.Equal(t, "content", hit.GetEvidence()[0].GetField())
	assert.Equal(t, "test content", hit.GetEvidence()[0].GetSnippet())
	assert.Equal(t, "lexical_match", hit.GetEvidence()[0].GetReason())
	assert.Equal(t, []string{"semantic", "lexical", "rrf"}, hit.GetRankingSignals())

	assert.Equal(t, "text-embedding-3-small", result.GetEmbeddingModel())
	assert.Equal(t, int32(100), result.GetTotalWithEmbeddings())
	assert.False(t, result.GetUsedKeywordFallback())
	assert.Equal(t, semanticsearch.RankingVersion, result.GetRankingVersion())
	require.NotNil(t, result.GetCoverage())
	assert.Equal(t, int32(120), result.GetCoverage().GetTotalLiveFeedback())
	assert.Equal(t, int32(100), result.GetCoverage().GetTotalWithEmbeddings())
	assert.Equal(t, "text-embedding-3-small", result.GetCoverage().GetEmbeddingModel())
}

func TestServiceResponseToProto_KeywordFallback(t *testing.T) {
	t.Parallel()
	resp := ptrext.Of(semanticsearch.SearchResponse{
		Hits: []*semanticsearch.SearchHit{
			{
				Feedback: ptrext.Of(repofeedback.SearchFeedback{
					ID:      1,
					Content: "test",
				}),
				KeywordScore: 1.0,
				MatchType:    "keyword",
			},
		},
		UsedKeywordFallback: true,
		FallbackReason:      "no_embeddings",
		RankingVersion:      semanticsearch.RankingVersion,
	})
	result := serviceResponseToProto(resp)

	require.NotNil(t, result)
	assert.True(t, result.GetUsedKeywordFallback())
	assert.Equal(t, "no_embeddings", result.GetFallbackReason())
	assert.Equal(t, semanticsearch.RankingVersion, result.GetRankingVersion())
	assert.Len(t, result.GetHits(), 1)
	assert.Equal(t, "keyword", result.GetHits()[0].GetMatchType())
}

func TestSearchFeedbackToProto_NilInput(t *testing.T) {
	t.Parallel()
	result := searchFeedbackToProto(nil)
	assert.Nil(t, result)
}

func TestSearchFeedbackToProto_EmptyStrings(t *testing.T) {
	t.Parallel()
	fb := ptrext.Of(repofeedback.SearchFeedback{
		ID:               1,
		Content:          "content",
		Source:           "api",
		EnrichedTitle:    "",
		EnrichmentStatus: "pending",
		CreatedAt:        time.Now(),
	})
	result := searchFeedbackToProto(fb)

	require.NotNil(t, result)
	assert.Equal(t, int64(1), result.GetId())
	assert.Nil(t, result.EnrichedTitle) // empty string becomes nil
}

func TestSearchFeedbackToProto_AllListFields(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 2, 10, 30, 0, 0, time.UTC)
	retryAt := now.Add(15 * time.Minute)
	confidence := 0.87
	fb := ptrext.Of(repofeedback.SearchFeedback{
		ID:                               42,
		Content:                          "checkout fails after invoice billing",
		Source:                           "web",
		Type:                             "bug",
		UserID:                           "user-42",
		Language:                         "en",
		PageURL:                          "https://example.test/checkout",
		EnrichedTitle:                    "Invoice checkout failure",
		EnrichedDisplayTitle:             "Invoice checkout failure",
		EnrichedDisplayLocale:            "en-US",
		EnrichedAttrs:                    []byte(`{"severity":"P0"}`),
		IsUrgent:                         true,
		ClassificationConfidence:         ptrext.Of(confidence),
		EnrichmentStatus:                 "failed",
		CreatedAt:                        now,
		WorkflowStateID:                  ptrext.Of("state-open"),
		EnrichmentAttempts:               5,
		EnrichmentNextRetryAt:            ptrext.Of(retryAt),
		TerminalFailureReasonClass:       "llm_err",
		TerminalFailureModel:             "gpt-4.1",
		TerminalFailureChannelID:         "chan-primary",
		TerminalFailureChannelName:       "Primary",
		TerminalFailureConfigFingerprint: "sha256:abc123",
		TerminalFailurePromptVersion:     "v2",
	})

	result := searchFeedbackToProto(fb)

	require.NotNil(t, result)
	assert.Equal(t, int64(42), result.GetId())
	assert.Equal(t, "bug", result.GetType())
	assert.Equal(t, "user-42", result.GetUserId())
	assert.Equal(t, "en", result.GetLanguage())
	assert.Equal(t, "https://example.test/checkout", result.GetPageUrl())
	assert.Equal(t, "P0", result.GetEnrichedAttrs().GetFields()["severity"].GetStringValue())
	assert.True(t, result.GetIsUrgent())
	assert.InDelta(t, confidence, result.GetClassificationConfidence(), 0.0001)
	assert.Equal(t, "failed", result.GetEnrichmentStatus())
	assert.Equal(t, now.Format(time.RFC3339), result.GetCreatedAt())
	assert.Equal(t, int32(5), result.GetEnrichmentAttempts())
	assert.Equal(t, retryAt.Format(time.RFC3339), result.GetEnrichmentNextRetryAt())
	assert.Equal(t, "llm_err", result.GetEnrichmentFailureReasonClass())
	assert.Equal(t, "gpt-4.1", result.GetEnrichmentFailureModel())
	assert.Equal(t, "chan-primary", result.GetEnrichmentFailureChannelId())
	assert.Equal(t, "Primary", result.GetEnrichmentFailureChannelName())
	assert.Equal(t, "sha256:abc123", result.GetEnrichmentFailureConfigFingerprint())
	assert.Equal(t, "v2", result.GetEnrichmentFailurePromptVersion())
}

// fakeSearchService implements searchService for testing.
type fakeSearchService struct {
	resp *semanticsearch.SearchResponse
	err  error
	req  *semanticsearch.SearchRequest
}

func (f *fakeSearchService) Search(_ context.Context, req *semanticsearch.SearchRequest) (*semanticsearch.SearchResponse, error) {
	f.req = req
	return f.resp, f.err
}

func (f *fakeSearchService) GetEmbedding(_ context.Context, _, _ string) ([]float32, string, error) {
	return nil, "", nil
}

func TestNewSearchHandlerStoresService(t *testing.T) {
	t.Parallel()

	svc := ptrext.Of(fakeSearchService{})
	h := NewSearchHandler(svc)

	require.NotNil(t, h)
	require.Same(t, svc, h.service)
}

type testSearchTagReader struct {
	byFeedback map[int64][]feedbacktagassignment.TagInfo
}

func (r *testSearchTagReader) ListByFeedback(_ context.Context, _ string, feedbackID int64) ([]feedbacktagassignment.TagInfo, error) {
	return r.byFeedback[feedbackID], nil
}

func (r *testSearchTagReader) ListByFeedbackBatch(_ context.Context, _ string, _ []int64) (map[int64][]feedbacktagassignment.TagInfo, error) {
	return r.byFeedback, nil
}

type fakeSearchOperations struct {
	run           *repofeedback.SearchRunInsert
	event         *repofeedback.SearchResultEventInsert
	dashboard     *repofeedback.SearchQualityDashboard
	dashboardOpts *repofeedback.SearchQualityQueryOpts
	runErr        error
	eventErr      error
	dashboardErr  error
}

func (f *fakeSearchOperations) RecordSearchRun(_ context.Context, row repofeedback.SearchRunInsert) error {
	f.run = ptrext.Of(row)
	return f.runErr
}

func (f *fakeSearchOperations) RecordSearchResultEvent(_ context.Context, row repofeedback.SearchResultEventInsert) error {
	f.event = ptrext.Of(row)
	return f.eventErr
}

func (f *fakeSearchOperations) SearchQualityDashboard(
	_ context.Context,
	opts repofeedback.SearchQualityQueryOpts,
) (*repofeedback.SearchQualityDashboard, error) {
	f.dashboardOpts = ptrext.Of(opts)
	return f.dashboard, f.dashboardErr
}

func bindSearchHandler(h *SearchHandler) http.HandlerFunc {
	return dispatcher.Bind(
		"console.SearchHandler.Search",
		dispatcher.JSON(func() *attunev1.SemanticSearchRequest {
			return ptrext.Of(attunev1.SemanticSearchRequest{})
		}),
		h.Search,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.SemanticSearchRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func newSearchHandler(svc searchService) http.HandlerFunc {
	return bindSearchHandler(&SearchHandler{service: svc})
}

func bindSearchQualityHandler(h *SearchHandler) http.HandlerFunc {
	return dispatcher.Bind(
		"console.SearchHandler.GetSearchQuality",
		dispatcher.Query(
			func() *attunev1.GetSearchQualityRequest {
				return ptrext.Of(attunev1.GetSearchQualityRequest{})
			},
			BindSearchQualityRequest,
		),
		h.GetSearchQuality,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.GetSearchQualityRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func bindSearchEventHandler(h *SearchHandler) http.HandlerFunc {
	return dispatcher.Bind(
		"console.SearchHandler.RecordSearchEvent",
		dispatcher.JSON(func() *attunev1.RecordSearchEventRequest {
			return ptrext.Of(attunev1.RecordSearchEventRequest{})
		}),
		h.RecordSearchEvent,
		dispatcher.WithAuth(func(r *http.Request, _ *attunev1.RecordSearchEventRequest) (*session.AuthCtx, error) {
			return dispatchtest.Auth(r.Context()), nil
		}),
	)
}

func TestSearchHandler_Search(t *testing.T) {
	t.Parallel()

	t.Run("200 OK with semantic search hits", func(t *testing.T) {
		now := time.Now()
		handler := newSearchHandler(&fakeSearchService{
			resp: &semanticsearch.SearchResponse{
				Hits: []*semanticsearch.SearchHit{
					{
						Feedback: ptrext.Of(repofeedback.SearchFeedback{
							ID:               1,
							Content:          "login button not working",
							Source:           "widget",
							EnrichedTitle:    "Login Issue",
							IsUrgent:         true,
							EnrichmentStatus: "done",
							CreatedAt:        now,
						}),
						Similarity:   0.92,
						KeywordScore: 0.3,
						MatchType:    "hybrid",
					},
					{
						Feedback: ptrext.Of(repofeedback.SearchFeedback{
							ID:               2,
							Content:          "authentication fails",
							Source:           "api",
							EnrichedTitle:    "Auth Failure",
							IsUrgent:         false,
							EnrichmentStatus: "done",
							CreatedAt:        now,
						}),
						Similarity:   0.85,
						KeywordScore: 0.2,
						MatchType:    "semantic",
					},
				},
				EmbeddingModel:      "text-embedding-3-small",
				TotalWithEmbeddings: 150,
				UsedKeywordFallback: false,
			},
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"login problem"}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)

		hits, ok := body["hits"].([]any)
		require.True(t, ok)
		require.Len(t, hits, 2)

		assert.Equal(t, "text-embedding-3-small", body["embeddingModel"])
		assert.Equal(t, float64(150), body["totalWithEmbeddings"])
		assert.Equal(t, false, body["usedKeywordFallback"])
	})

	t.Run("200 OK with keyword fallback", func(t *testing.T) {
		now := time.Now()
		handler := newSearchHandler(&fakeSearchService{
			resp: &semanticsearch.SearchResponse{
				Hits: []*semanticsearch.SearchHit{
					{
						Feedback: ptrext.Of(repofeedback.SearchFeedback{
							ID:               5,
							Content:          "payment processing delay",
							Source:           "email",
							EnrichedTitle:    "Payment Delay",
							EnrichmentStatus: "done",
							CreatedAt:        now,
						}),
						Similarity:   0.0,
						KeywordScore: 0.9,
						MatchType:    "keyword",
					},
				},
				EmbeddingModel:      "",
				TotalWithEmbeddings: 0,
				UsedKeywordFallback: true,
			},
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"payment delay"}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)

		assert.Equal(t, true, body["usedKeywordFallback"])

		hits, ok := body["hits"].([]any)
		require.True(t, ok)
		require.Len(t, hits, 1)

		hit := hits[0].(map[string]any)
		assert.Equal(t, "keyword", hit["matchType"])
	})

	t.Run("400 Bad Request for empty query", func(t *testing.T) {
		handler := newSearchHandler(&fakeSearchService{})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":""}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		assert.Contains(t, body["message"], "query")
	})

	t.Run("400 Bad Request for missing query", func(t *testing.T) {
		handler := newSearchHandler(&fakeSearchService{})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400 Bad Request for whitespace query", func(t *testing.T) {
		svc := &fakeSearchService{}
		handler := newSearchHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"  "}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Nil(t, svc.req)
	})

	t.Run("400 Bad Request for query over max runes", func(t *testing.T) {
		svc := &fakeSearchService{}
		handler := newSearchHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"`+strings.Repeat("a", semanticsearch.MaxQueryRunes+1)+`"}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		assert.Contains(t, body["message"], "at most")
		assert.Nil(t, svc.req)
	})

	t.Run("400 Bad Request for unsupported control character", func(t *testing.T) {
		svc := &fakeSearchService{}
		handler := newSearchHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			"{\"q\":\"checkout\\u0000failure\"}"))

		require.Equal(t, http.StatusBadRequest, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		assert.Contains(t, body["message"], "control characters")
		assert.Nil(t, svc.req)
	})

	t.Run("trims query before calling service", func(t *testing.T) {
		svc := &fakeSearchService{
			resp: &semanticsearch.SearchResponse{},
		}
		handler := newSearchHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"  test query  "}`))

		require.Equal(t, http.StatusOK, w.Code)
		require.NotNil(t, svc.req)
		assert.Equal(t, "test query", svc.req.Query)
	})

	t.Run("400 Bad Request for limit exceeding max", func(t *testing.T) {
		handler := newSearchHandler(&fakeSearchService{})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"test","limit":150}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		assert.Contains(t, body["message"], "limit")
	})

	t.Run("429 Too Many Requests rate limit", func(t *testing.T) {
		handler := newSearchHandler(&fakeSearchService{
			err: semanticsearch.ErrRateLimited,
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"test query"}`))

		require.Equal(t, http.StatusTooManyRequests, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		assert.Contains(t, body["message"], "rate limit")
	})

	t.Run("400 Bad Request for service empty query error", func(t *testing.T) {
		handler := newSearchHandler(&fakeSearchService{
			err: semanticsearch.ErrEmptyQuery,
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"test"}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400 Bad Request for service query too long error", func(t *testing.T) {
		handler := newSearchHandler(&fakeSearchService{
			err: semanticsearch.ErrQueryTooLong,
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"test"}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400 Bad Request for service invalid query error", func(t *testing.T) {
		handler := newSearchHandler(&fakeSearchService{
			err: semanticsearch.ErrInvalidQuery,
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"test"}`))

		require.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("500 Internal Server Error", func(t *testing.T) {
		handler := newSearchHandler(&fakeSearchService{
			err: errors.New("database connection failed"),
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"test query"}`))

		require.Equal(t, http.StatusInternalServerError, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		assert.Contains(t, body["message"], "search failed")
	})

	t.Run("200 OK with filter options", func(t *testing.T) {
		svc := &fakeSearchService{
			resp: &semanticsearch.SearchResponse{
				Hits:                []*semanticsearch.SearchHit{},
				EmbeddingModel:      "text-embedding-3-small",
				TotalWithEmbeddings: 50,
				UsedKeywordFallback: false,
			},
		}
		handler := newSearchHandler(svc)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"test","limit":10,"min_similarity":0.7,"filter":{"urgent":true,"enrichment_status":"failed","terminal_failed_only":true}}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)

		hits, ok := body["hits"].([]any)
		require.True(t, ok)
		assert.Empty(t, hits)
		require.NotNil(t, svc.req)
		require.NotNil(t, svc.req.Filter)
		require.NotNil(t, svc.req.Filter.EnrichmentStatus)
		assert.Equal(t, "failed", ptrext.Indirect(svc.req.Filter.EnrichmentStatus))
		require.NotNil(t, svc.req.Filter.TerminalFailedOnly)
		assert.True(t, ptrext.Indirect(svc.req.Filter.TerminalFailedOnly))
	})

	t.Run("200 OK hydrates tags and workflow on hits", func(t *testing.T) {
		now := time.Now()
		searchHandler := &SearchHandler{
			service: &fakeSearchService{
				resp: &semanticsearch.SearchResponse{
					Hits: []*semanticsearch.SearchHit{
						{
							Feedback: ptrext.Of(repofeedback.SearchFeedback{
								ID:               9,
								Content:          "billing fails",
								Source:           "web",
								EnrichedTitle:    "Billing failure",
								EnrichmentStatus: "done",
								CreatedAt:        now,
								WorkflowStateID:  ptrext.Of("state-open"),
							}),
							Similarity: 0.75,
							MatchType:  "semantic",
						},
					},
				},
			},
		}
		searchHandler.SetTagAssignments(&testSearchTagReader{
			byFeedback: map[int64][]feedbacktagassignment.TagInfo{
				9: {
					{
						TagID:        uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"),
						Name:         "billing",
						Color:        "#38bdf8",
						TagCreatedAt: now,
						TagUpdatedAt: now,
					},
				},
			},
		})
		searchHandler.SetWorkflowStates(&fakeWorkflowStateReader{
			states: []workflowstate.WorkflowState{
				{ID: "state-open", Name: "Open", Color: "#3b82f6", Category: "open", CreatedAt: now, UpdatedAt: now},
				{ID: "state-done", Name: "Done", Color: "#10b981", Category: "closed", CreatedAt: now, UpdatedAt: now},
			},
			transitions: []workflowstate.Transition{
				{FromStateID: "state-open", ToStateID: "state-done"},
			},
		})
		handler := bindSearchHandler(searchHandler)

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search", `{"q":"billing"}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)
		hits, ok := body["hits"].([]any)
		require.True(t, ok)
		require.Len(t, hits, 1)
		hit := hits[0].(map[string]any)
		item := hit["feedback"].(map[string]any)
		tags := item["tags"].([]any)
		require.Len(t, tags, 1)
		assert.Equal(t, "billing", tags[0].(map[string]any)["name"])
		state := item["workflowState"].(map[string]any)
		assert.Equal(t, "Open", state["name"])
		allowed := item["allowedNextStates"].([]any)
		require.Len(t, allowed, 1)
		assert.Equal(t, "Done", allowed[0].(map[string]any)["name"])
	})

	t.Run("200 OK empty results", func(t *testing.T) {
		handler := newSearchHandler(&fakeSearchService{
			resp: &semanticsearch.SearchResponse{
				Hits:                []*semanticsearch.SearchHit{},
				EmbeddingModel:      "text-embedding-3-small",
				TotalWithEmbeddings: 100,
				UsedKeywordFallback: false,
			},
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"xyznonexistent123"}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)

		hits, ok := body["hits"].([]any)
		require.True(t, ok)
		assert.Empty(t, hits)
	})
}

func TestSearchHandler_SearchRecordsRunTelemetry(t *testing.T) {
	t.Parallel()
	ops := &fakeSearchOperations{}
	handler := &SearchHandler{
		service: &fakeSearchService{
			resp: &semanticsearch.SearchResponse{
				Hits: []*semanticsearch.SearchHit{
					{
						Feedback: ptrext.Of(repofeedback.SearchFeedback{
							ID:               10,
							Content:          "login fails",
							Source:           "api",
							EnrichmentStatus: "done",
							CreatedAt:        time.Now(),
						}),
						MatchType: "hybrid",
					},
				},
				EmbeddingModel:      "text-embedding-3-small",
				TotalWithEmbeddings: 8,
				RankingVersion:      semanticsearch.RankingVersion,
				Coverage: &semanticsearch.SearchCoverage{
					TotalLiveFeedback:   10,
					TotalWithEmbeddings: 8,
					EmbeddingModel:      "text-embedding-3-small",
				},
			},
		},
	}
	handler.SetSearchOperations(ops)

	w := httptest.NewRecorder()
	bindSearchHandler(handler)(w, dispatchtest.Request(
		http.MethodPost,
		"/feedback/search",
		`{"q":"  Login fails  ","filter":{"urgent":true}}`,
	))

	require.Equal(t, http.StatusOK, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	runID, ok := body["runId"].(string)
	require.True(t, ok)
	_, err = uuid.Parse(runID)
	require.NoError(t, err)

	require.NotNil(t, ops.run)
	assert.Equal(t, runID, ops.run.RunID)
	assert.Equal(t, "tenant-1", ops.run.TenantID)
	assert.Equal(t, "Login fails", ops.run.QueryPreview)
	assert.Len(t, ops.run.QueryHash, 64)
	assert.Len(t, ops.run.FilterHash, 64)
	assert.Equal(t, 1, ops.run.ResultCount)
	assert.Equal(t, 10, ops.run.TotalLiveFeedback)
	assert.Equal(t, 8, ops.run.TotalWithEmbeddings)
	assert.InDelta(t, 0.8, ops.run.CoverageRatio, 0.001)
	assert.Equal(t, semanticsearch.RankingVersion, ops.run.RankingVersion)
}

func TestSearchHandler_GetSearchQuality(t *testing.T) {
	t.Parallel()
	ops := &fakeSearchOperations{
		dashboard: &repofeedback.SearchQualityDashboard{
			Summary: repofeedback.SearchQualitySummary{
				QueryCount:         10,
				ZeroResultCount:    2,
				FallbackCount:      1,
				ClickCount:         6,
				ClickedRunCount:    4,
				AverageResultCount: 7.5,
				P95LatencyMS:       500,
			},
			Queries: []repofeedback.SearchQualityQueryAggregate{
				{
					QueryHash:          strings.Repeat("a", 64),
					QueryPreview:       "login failure",
					QueryCount:         4,
					ClickedRunCount:    2,
					AverageResultCount: 8,
					P95LatencyMS:       300,
					LastSeenAt:         time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
				},
			},
			FallbackBreakdown: []repofeedback.SearchFallbackAggregate{
				{Reason: "no_embeddings", Count: 1, Share: 1},
			},
			IndexHealth: repofeedback.SearchIndexHealth{
				TotalLiveFeedback:   100,
				TotalWithEmbeddings: 90,
				EmbeddingModel:      "text-embedding-3-small",
			},
		},
	}
	handler := &SearchHandler{}
	handler.SetSearchOperations(ops)

	w := httptest.NewRecorder()
	bindSearchQualityHandler(handler)(w, dispatchtest.Request(
		http.MethodGet,
		"/feedback/search/quality?current_from=2026-06-25T00:00:00Z&current_to=2026-07-02T00:00:00Z&bucket_width=day&limit=5",
		"",
	))

	require.Equal(t, http.StatusOK, w.Code)
	body, err := dispatchtest.DecodeJSON(w.Body)
	require.NoError(t, err)
	summary := body["summary"].(map[string]any)
	assert.Equal(t, "10", summary["queryCount"])
	assert.InDelta(t, 0.2, summary["zeroResultRate"], 0.001)
	assert.Equal(t, "alert", summary["worstSeverity"])
	queries := body["queries"].([]any)
	require.Len(t, queries, 1)
	assert.Equal(t, "login failure", queries[0].(map[string]any)["queryPreview"])
	require.NotNil(t, ops.dashboardOpts)
	assert.Equal(t, 5, ops.dashboardOpts.Limit)
	assert.Equal(t, "tenant-1", ops.dashboardOpts.TenantID)
}

func TestSearchHandler_RecordSearchEvent(t *testing.T) {
	t.Parallel()
	runID := uuid.NewString()
	ops := &fakeSearchOperations{}
	handler := &SearchHandler{}
	handler.SetSearchOperations(ops)
	httpHandler := bindSearchEventHandler(handler)

	w := httptest.NewRecorder()
	httpHandler(w, dispatchtest.Request(
		http.MethodPost,
		"/feedback/search/events",
		`{"run_id":"`+runID+`","feedback_id":77,"action":"open","rank":2,"match_type":"hybrid"}`,
	))

	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, ops.event)
	assert.Equal(t, runID, ops.event.RunID)
	assert.Equal(t, int64(77), ops.event.FeedbackID)
	assert.Equal(t, "open", ops.event.Action)
	assert.Equal(t, 2, ops.event.Rank)
	assert.Equal(t, "hybrid", ops.event.MatchType)
	assert.Equal(t, "tenant-1", ops.event.TenantID)

	w = httptest.NewRecorder()
	httpHandler(w, dispatchtest.Request(
		http.MethodPost,
		"/feedback/search/events",
		`{"run_id":"`+runID+`","feedback_id":77,"action":"delete","rank":2}`,
	))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSearchHandler_GetSearchQualityEdges(t *testing.T) {
	t.Parallel()

	ctx := replyWorkflowTestCtx()
	t.Run("operations missing", func(t *testing.T) {
		t.Parallel()
		_, err := (&SearchHandler{}).GetSearchQuality(ctx, ptrext.Of(attunev1.GetSearchQualityRequest{}))

		requireDispatcherError(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	})

	t.Run("validation", func(t *testing.T) {
		t.Parallel()
		h := &SearchHandler{operations: &fakeSearchOperations{}}

		_, err := h.GetSearchQuality(ctx, ptrext.Of(attunev1.GetSearchQualityRequest{BucketWidth: "week"}))

		requireDispatcherError(t, err, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)
	})

	t.Run("dashboard error", func(t *testing.T) {
		t.Parallel()
		h := &SearchHandler{operations: &fakeSearchOperations{dashboardErr: errors.New("dashboard failed")}}

		_, err := h.GetSearchQuality(ctx, ptrext.Of(attunev1.GetSearchQualityRequest{}))

		requireDispatcherError(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	})

	t.Run("bind bad limit", func(t *testing.T) {
		t.Parallel()
		req := ptrext.Of(attunev1.GetSearchQualityRequest{})

		err := BindSearchQualityRequest(httptest.NewRequest(http.MethodGet, "/search-quality?limit=nope", nil), req)

		requireDispatcherError(t, err, http.StatusBadRequest, attunev1.ErrorCode_VALIDATION)
	})
}

func TestSearchHandler_RecordSearchEventEdges(t *testing.T) {
	t.Parallel()

	ctx := replyWorkflowTestCtx()
	valid := ptrext.Of(attunev1.RecordSearchEventRequest{
		RunId:      uuid.NewString(),
		FeedbackId: 77,
		Action:     "open",
	})
	t.Run("operations missing", func(t *testing.T) {
		t.Parallel()
		_, err := (&SearchHandler{}).RecordSearchEvent(ctx, valid)

		requireDispatcherError(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	})

	t.Run("run not found", func(t *testing.T) {
		t.Parallel()
		h := &SearchHandler{operations: &fakeSearchOperations{eventErr: repofeedback.ErrSearchRunNotFound}}

		_, err := h.RecordSearchEvent(ctx, valid)

		requireDispatcherError(t, err, http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND)
	})

	t.Run("insert error", func(t *testing.T) {
		t.Parallel()
		h := &SearchHandler{operations: &fakeSearchOperations{eventErr: errors.New("insert failed")}}

		_, err := h.RecordSearchEvent(ctx, valid)

		requireDispatcherError(t, err, http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL)
	})
}

func TestSearchHandlerRecordSearchRunEdges(t *testing.T) {
	t.Parallel()

	ctx := replyWorkflowTestCtx()
	auth := ctx.Auth
	req := ptrext.Of(attunev1.SemanticSearchRequest{Filter: ptrext.Of(attunev1.FeedbackFilter{
		Urgent: ptrext.Of(true),
	})})
	(&SearchHandler{}).recordSearchRun(ctx, auth, req, "query", "run-1", time.Second, nil)
	(&SearchHandler{operations: &fakeSearchOperations{}}).recordSearchRun(ctx, auth, req, "query", "run-1", time.Second, nil)

	ops := &fakeSearchOperations{runErr: errors.New("telemetry failed")}
	h := &SearchHandler{operations: ops}
	h.recordSearchRun(ctx, auth, req, "  Query  ", "run-2", 1500*time.Millisecond, &semanticsearch.SearchResponse{
		Hits: []*semanticsearch.SearchHit{{
			Feedback: ptrext.Of(repofeedback.SearchFeedback{ID: 1}),
		}},
		EmbeddingModel:      "text-embedding-3-small",
		TotalWithEmbeddings: 3,
		RankingVersion:      semanticsearch.RankingVersion,
		UsedKeywordFallback: true,
		FallbackReason:      "embedding_unavailable",
	})

	require.NotNil(t, ops.run)
	require.Equal(t, "run-2", ops.run.RunID)
	require.Equal(t, "Query", ops.run.QueryPreview)
	require.Equal(t, 1, ops.run.ResultCount)
	require.Equal(t, 0, ops.run.TotalLiveFeedback)
	require.Equal(t, 3, ops.run.TotalWithEmbeddings)
	require.InDelta(t, 1.0, ops.run.CoverageRatio, 0.001)
	require.Equal(t, "embedding_unavailable", ops.run.FallbackReason)

	okOps := &fakeSearchOperations{}
	(&SearchHandler{operations: okOps}).recordSearchRun(
		ctx,
		auth,
		ptrext.Of(attunev1.SemanticSearchRequest{}),
		"query",
		"run-3",
		time.Millisecond,
		&semanticsearch.SearchResponse{},
	)
	require.NotNil(t, okOps.run)
	require.Empty(t, okOps.run.EmbeddingModel)

	coverageOps := &fakeSearchOperations{}
	(&SearchHandler{operations: coverageOps}).recordSearchRun(
		ctx,
		auth,
		ptrext.Of(attunev1.SemanticSearchRequest{}),
		"query",
		"run-4",
		time.Millisecond,
		&semanticsearch.SearchResponse{
			Coverage: &semanticsearch.SearchCoverage{
				TotalLiveFeedback:   10,
				TotalWithEmbeddings: 5,
				EmbeddingModel:      "coverage-model",
			},
		},
	)
	require.NotNil(t, coverageOps.run)
	require.Equal(t, "coverage-model", coverageOps.run.EmbeddingModel)
	require.Equal(t, 10, coverageOps.run.TotalLiveFeedback)
	require.Equal(t, 5, coverageOps.run.TotalWithEmbeddings)
}

func TestSearchResponseRowsAndProtoFeedbacksSkipNilEntries(t *testing.T) {
	t.Parallel()

	require.Nil(t, searchResponseRows(nil))
	rows := searchResponseRows(&semanticsearch.SearchResponse{
		Hits: []*semanticsearch.SearchHit{
			nil,
			{},
			{Feedback: ptrext.Of(repofeedback.SearchFeedback{
				ID:               42,
				Content:          "login failed",
				Source:           "api",
				EnrichmentStatus: "done",
				CreatedAt:        time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC),
			})},
		},
	})
	require.Len(t, rows, 1)
	require.Equal(t, int64(42), rows[0].ID)

	require.Nil(t, semanticSearchProtoFeedbacks(nil))
	items := semanticSearchProtoFeedbacks(ptrext.Of(attunev1.SemanticSearchResponse{
		Hits: []*attunev1.SemanticSearchHit{
			nil,
			{},
			{Feedback: ptrext.Of(attunev1.Feedback{Id: 42})},
		},
	}))
	require.Len(t, items, 1)
	require.Equal(t, int64(42), items[0].GetId())
}

func TestSearchEvidenceToProtoSkipsEmptyEvidence(t *testing.T) {
	t.Parallel()

	got := searchEvidenceToProto([]semanticsearch.SearchEvidence{
		{},
		{Field: "content", Snippet: "needle"},
	})

	require.Len(t, got, 1)
	require.Equal(t, "content", got[0].GetField())
	require.Equal(t, "needle", got[0].GetSnippet())
}
