// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test fixtures build proto-request pointers

package feedback

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers/console/internal/dispatchtest"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	repofeedback "github.com/Phixsura/attune/internal/repo/feedback"
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
}

func TestProtoFilterToRepoFilter_FullFilter(t *testing.T) {
	t.Parallel()
	pf := ptrext.Of(attunev1.FeedbackFilter{
		Attrs: []*attunev1.AttrFilter{
			{Dim: "severity", Value: "high"},
			{Dim: "labels", Value: "bug"},
		},
		Urgent:           ptrext.Of(true),
		Q:                ptrext.Of("search query"),
		TagId:            ptrext.Of("tag-123"),
		WorkflowStateId:  ptrext.Of("state-456"),
		WorkflowCategory: ptrext.Of("open"),
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
			},
			nil, // should be skipped
			{
				Feedback: nil, // should be skipped
			},
		},
		EmbeddingModel:      "text-embedding-3-small",
		TotalWithEmbeddings: 100,
		UsedKeywordFallback: false,
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

	assert.Equal(t, "text-embedding-3-small", result.GetEmbeddingModel())
	assert.Equal(t, int32(100), result.GetTotalWithEmbeddings())
	assert.False(t, result.GetUsedKeywordFallback())
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
	})
	result := serviceResponseToProto(resp)

	require.NotNil(t, result)
	assert.True(t, result.GetUsedKeywordFallback())
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

// fakeSearchService implements searchService for testing.
type fakeSearchService struct {
	resp *semanticsearch.SearchResponse
	err  error
}

func (f *fakeSearchService) Search(_ context.Context, _ *semanticsearch.SearchRequest) (*semanticsearch.SearchResponse, error) {
	return f.resp, f.err
}

func (f *fakeSearchService) GetEmbedding(_ context.Context, _, _ string) ([]float32, string, error) {
	return nil, "", nil
}

func newSearchHandler(svc searchService) http.HandlerFunc {
	h := &SearchHandler{service: svc}
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
			`{"q":"  "}`)) // whitespace-only may pass handler validation but fail service

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
		handler := newSearchHandler(&fakeSearchService{
			resp: &semanticsearch.SearchResponse{
				Hits:                []*semanticsearch.SearchHit{},
				EmbeddingModel:      "text-embedding-3-small",
				TotalWithEmbeddings: 50,
				UsedKeywordFallback: false,
			},
		})

		w := httptest.NewRecorder()
		handler(w, dispatchtest.Request(http.MethodPost, "/feedback/search",
			`{"q":"test","limit":10,"min_similarity":0.7,"filter":{"urgent":true}}`))

		require.Equal(t, http.StatusOK, w.Code)
		body, err := dispatchtest.DecodeJSON(w.Body)
		require.NoError(t, err)

		hits, ok := body["hits"].([]any)
		require.True(t, ok)
		assert.Empty(t, hits)
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
