// SPDX-License-Identifier: Apache-2.0

package semanticsearch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// mockFeedbackStore implements FeedbackStore for testing.
type mockFeedbackStore struct {
	hasEmbedding      bool
	hasEmbeddingErr   error
	stats             *feedback.EmbeddingStats
	statsErr          error
	semanticHits      []feedback.SemanticSearchHit
	semanticErr       error
	lexicalResults    []feedback.LexicalSearchHit
	lexicalErr        error
	keywordResults    []*feedback.SearchFeedback
	keywordErr        error
	semanticCallCount int
	lexicalCallCount  int
	keywordCallCount  int
	semanticParams    *feedback.SemanticSearchParams
	lexicalParams     *feedback.LexicalSearchParams
}

func (m *mockFeedbackStore) SemanticSearch(_ context.Context, params *feedback.SemanticSearchParams) ([]feedback.SemanticSearchHit, error) {
	m.semanticCallCount++
	m.semanticParams = params
	return m.semanticHits, m.semanticErr
}

func (m *mockFeedbackStore) KeywordSearch(_ context.Context, _ *feedback.KeywordSearchParams) ([]*feedback.SearchFeedback, error) {
	m.keywordCallCount++
	return m.keywordResults, m.keywordErr
}

func (m *mockFeedbackStore) LexicalSearch(_ context.Context, params *feedback.LexicalSearchParams) ([]feedback.LexicalSearchHit, error) {
	m.lexicalCallCount++
	m.lexicalParams = params
	if m.lexicalErr != nil {
		return nil, m.lexicalErr
	}
	if m.keywordErr != nil {
		return nil, m.keywordErr
	}
	if m.lexicalResults != nil {
		return m.lexicalResults, nil
	}
	hits := make([]feedback.LexicalSearchHit, 0, len(m.keywordResults))
	for i, fb := range m.keywordResults {
		hits = append(hits, feedback.LexicalSearchHit{
			Feedback: fb,
			Rank:     i + 1,
		})
	}
	return hits, nil
}

func (m *mockFeedbackStore) HasEmbedding(_ context.Context, _ string) (bool, error) {
	return m.hasEmbedding, m.hasEmbeddingErr
}

func (m *mockFeedbackStore) GetEmbeddingStats(_ context.Context, _ string) (*feedback.EmbeddingStats, error) {
	return m.stats, m.statsErr
}

func lexicalHitsFromFeedback(results []feedback.SearchFeedback) []feedback.LexicalSearchHit {
	hits := make([]feedback.LexicalSearchHit, 0, len(results))
	for i, fb := range results {
		hits = append(hits, feedback.LexicalSearchHit{
			Feedback: ptrext.Of(fb),
			Rank:     i + 1,
		})
	}
	return hits
}

// mockCache implements EmbeddingCache for testing with controllable behavior.
type mockCache struct {
	getEmbedding []float32
	getModel     string
	getFound     bool
	getErr       error
	setErr       error
	getCalls     int
	setCalls     int
}

func (m *mockCache) Get(_ context.Context, _, _ string) ([]float32, string, bool, error) {
	m.getCalls++
	return m.getEmbedding, m.getModel, m.getFound, m.getErr
}

func (m *mockCache) Set(_ context.Context, _, _ string, _ []float32, _ string, _ time.Duration) error {
	m.setCalls++
	return m.setErr
}

// mockRateLimiter implements ratelimit.SlidingLimiter for testing.
type mockRateLimiter struct {
	allowed    bool
	retryAfter time.Duration
	err        error
}

func (m *mockRateLimiter) Allow(_ context.Context, _ string, _ int, _ time.Duration) (bool, time.Duration, error) {
	return m.allowed, m.retryAfter, m.err
}

func (m *mockRateLimiter) AllowWithInfo(_ context.Context, _ string, _ int, _ time.Duration) (bool, ratelimit.RateLimitInfo, error) {
	return m.allowed, ratelimit.RateLimitInfo{}, m.err
}

func TestSearch_EmptyQuery(t *testing.T) {
	t.Parallel()
	svc := New(nil, nil, nil, nil)

	_, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "",
	}))

	require.ErrorIs(t, err, ErrEmptyQuery)
}

func TestSearch_WhitespaceQuery(t *testing.T) {
	t.Parallel()
	svc := New(nil, nil, nil, nil)

	_, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "   ",
	}))

	require.ErrorIs(t, err, ErrEmptyQuery)
}

func TestSearch_NilRequest(t *testing.T) {
	t.Parallel()
	svc := New(nil, nil, nil, nil)

	_, err := svc.Search(context.Background(), nil)

	require.ErrorIs(t, err, ErrEmptyQuery)
}

func TestSearch_QueryTooLong(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{})
	svc := New(store, nil, nil, nil)

	_, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    strings.Repeat("a", MaxQueryRunes+1),
	}))

	require.ErrorIs(t, err, ErrQueryTooLong)
	require.Zero(t, store.lexicalCallCount)
	require.Zero(t, store.semanticCallCount)
}

func TestSearch_QueryWithUnsupportedControlCharacter(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{})
	svc := New(store, nil, nil, nil)

	_, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "checkout\x00failure",
	}))

	require.ErrorIs(t, err, ErrInvalidQuery)
	require.Zero(t, store.lexicalCallCount)
	require.Zero(t, store.semanticCallCount)
}

func TestNormalizeQuery_AllowsWhitespaceSeparators(t *testing.T) {
	t.Parallel()
	query, err := NormalizeQuery("  checkout\ninvoice\tfailure  ")

	require.NoError(t, err)
	require.Equal(t, "checkout\ninvoice\tfailure", query)
}

func TestSearch_RateLimited(t *testing.T) {
	t.Parallel()
	limiter := ratelimit.NewMemorySlidingLimiter()
	store := ptrext.Of(mockFeedbackStore{})
	svc := New(store, nil, limiter, nil)

	// Exhaust the rate limit.
	ctx := context.Background()
	for i := 0; i < SearchRateLimit; i++ {
		_, _ = svc.Search(ctx, ptrext.Of(SearchRequest{
			TenantID: "tenant1",
			Query:    "test",
		}))
	}

	// Next request should be rate limited.
	_, err := svc.Search(ctx, ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
	}))

	require.ErrorIs(t, err, ErrRateLimited)
}

func TestSearch_RateLimiterError_FailsOpen(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		hasEmbedding: false,
		keywordResults: []*feedback.SearchFeedback{
			{ID: 1, Content: "result"},
		},
	})
	limiter := ptrext.Of(mockRateLimiter{
		err: errors.New("redis connection refused"),
	})
	svc := New(store, nil, limiter, nil)

	resp, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
	}))

	require.NoError(t, err)
	require.Len(t, resp.Hits, 1)
}

func TestSearch_NoEmbeddings_FallsBackToKeyword(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		hasEmbedding: false,
		keywordResults: []*feedback.SearchFeedback{
			{ID: 1, Content: "test content", EnrichedTitle: "Test"},
			{ID: 2, Content: "another test", EnrichedTitle: "Another"},
		},
	})
	svc := New(store, nil, nil, nil)

	resp, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
	}))
	require.NoError(t, err)
	require.True(t, resp.UsedKeywordFallback)
	require.Equal(t, fallbackReasonNoEmbeddings, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
	require.Len(t, resp.Hits, 2)
	require.Equal(t, "keyword", resp.Hits[0].MatchType)
	require.Equal(t, 1, store.lexicalCallCount)
}

func TestSearch_NormalizesRequestBeforeKeywordFallback(t *testing.T) {
	t.Parallel()
	filter := ptrext.Of(feedback.FeedbackFilter{Q: "checkout"})
	store := ptrext.Of(mockFeedbackStore{
		hasEmbedding: false,
		keywordResults: []*feedback.SearchFeedback{
			{ID: 1, Content: "checkout"},
		},
	})
	svc := New(store, nil, nil, nil)

	resp, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "  checkout issue  ",
		Filter:   filter,
	}))

	require.NoError(t, err)
	require.True(t, resp.UsedKeywordFallback)
	require.Equal(t, 1, store.lexicalCallCount)
	require.NotNil(t, store.lexicalParams)
	require.Equal(t, "tenant1", store.lexicalParams.TenantID)
	require.Equal(t, "checkout issue", store.lexicalParams.Query)
	require.Equal(t, DefaultLimit, store.lexicalParams.Limit)
	require.Same(t, filter, store.lexicalParams.Filter)
}

func TestSearch_HasEmbeddingError_FallsBackToKeyword(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		hasEmbeddingErr: errors.New("db error"),
		keywordResults: []*feedback.SearchFeedback{
			{ID: 1, Content: "test", EnrichedTitle: "Test"},
		},
	})
	svc := New(store, nil, nil, nil)

	resp, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
	}))
	require.NoError(t, err)
	require.True(t, resp.UsedKeywordFallback)
	require.Equal(t, fallbackReasonEmbeddingCheckFailed, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
}

func TestSearch_StatsError_UsesEmptyStats(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		hasEmbedding: true,
		statsErr:     errors.New("stats query failed"),
		keywordResults: []*feedback.SearchFeedback{
			{ID: 1, Content: "test"},
		},
	})
	// stats error -> stats.EmbeddingModel == "" -> keyword fallback.
	svc := New(store, nil, nil, nil)

	resp, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
	}))
	require.NoError(t, err)
	require.True(t, resp.UsedKeywordFallback)
	require.Equal(t, fallbackReasonEmbeddingStatsFailed, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
}

func TestSearch_EmptyEmbeddingModel_FallsBackToKeyword(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		hasEmbedding: true,
		stats: ptrext.Of(feedback.EmbeddingStats{
			EmbeddedCount:  10,
			EmbeddingModel: "", // no model set
		}),
		keywordResults: []*feedback.SearchFeedback{
			{ID: 1, Content: "test"},
		},
	})
	svc := New(store, nil, nil, nil)

	resp, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
	}))
	require.NoError(t, err)
	require.True(t, resp.UsedKeywordFallback)
	require.Equal(t, fallbackReasonNoEmbeddings, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
}

func TestSearch_EmbeddingGenerationFails_FallsBackToKeyword(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		hasEmbedding: true,
		stats: ptrext.Of(feedback.EmbeddingStats{
			EmbeddedCount:  10,
			EmbeddingModel: "text-embedding-3-small",
		}),
		keywordResults: []*feedback.SearchFeedback{
			{ID: 1, Content: "test"},
		},
	})
	// nil router -> getOrGenerateEmbedding returns error -> keyword fallback.
	svc := New(store, nil, nil, nil)

	resp, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
	}))
	require.NoError(t, err)
	require.True(t, resp.UsedKeywordFallback)
	require.Equal(t, fallbackReasonEmbeddingGenerationFail, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
}

func TestSearch_KeywordScoreDescending(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		hasEmbedding: false,
		keywordResults: []*feedback.SearchFeedback{
			{ID: 1, Content: "first", EnrichedTitle: "First"},
			{ID: 2, Content: "second", EnrichedTitle: "Second"},
			{ID: 3, Content: "third", EnrichedTitle: "Third"},
		},
	})
	svc := New(store, nil, nil, nil)

	resp, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
	}))
	require.NoError(t, err)

	// First result should have highest keyword score.
	require.Greater(t, resp.Hits[0].KeywordScore, resp.Hits[1].KeywordScore)
	require.Greater(t, resp.Hits[1].KeywordScore, resp.Hits[2].KeywordScore)
}

func TestSearch_NilRateLimiter_Proceeds(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		hasEmbedding: false,
		keywordResults: []*feedback.SearchFeedback{
			{ID: 1, Content: "test"},
		},
	})
	svc := New(store, nil, nil, nil)

	resp, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
	}))

	require.NoError(t, err)
	require.Len(t, resp.Hits, 1)
}

func TestSearch_ModelMismatch_FallsBackToKeyword(t *testing.T) {
	t.Parallel()
	cachedEmbedding := make([]float32, EmbeddingDims)
	store := ptrext.Of(mockFeedbackStore{
		hasEmbedding: true,
		stats: ptrext.Of(feedback.EmbeddingStats{
			EmbeddedCount:  50,
			EmbeddingModel: "text-embedding-3-large",
		}),
		keywordResults: []*feedback.SearchFeedback{
			{ID: 1, Content: "keyword result"},
		},
	})
	cache := ptrext.Of(mockCache{
		getEmbedding: cachedEmbedding,
		getModel:     "text-embedding-3-small",
		getFound:     true,
	})
	svc := New(store, nil, nil, cache)

	resp, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
	}))

	require.NoError(t, err)
	require.True(t, resp.UsedKeywordFallback)
	require.Equal(t, fallbackReasonEmbeddingModelMismatch, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
	require.Len(t, resp.Hits, 1)
}

func TestNormalizeRequest_Defaults(t *testing.T) {
	t.Parallel()
	req := ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "  test  ",
	})

	normalized := normalizeRequest(req)

	require.Equal(t, "test", normalized.Query)
	require.Equal(t, DefaultLimit, normalized.Limit)
	require.InDelta(t, DefaultMinSimilarity, normalized.MinSimilarity, 0.001)
	require.InDelta(t, DefaultSemanticWeight, normalized.SemanticWeight, 0.001)
	require.InDelta(t, DefaultKeywordWeight, normalized.KeywordWeight, 0.001)
}

func TestNormalizeRequest_LimitCap(t *testing.T) {
	t.Parallel()
	req := ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
		Limit:    500,
	})

	normalized := normalizeRequest(req)

	require.Equal(t, MaxLimit, normalized.Limit)
}

func TestNormalizeRequest_PreservesCustomValues(t *testing.T) {
	t.Parallel()
	filter := ptrext.Of(feedback.FeedbackFilter{Q: "bug"})
	req := ptrext.Of(SearchRequest{
		TenantID:       "tenant1",
		Query:          "test",
		Limit:          15,
		MinSimilarity:  0.8,
		Filter:         filter,
		SemanticWeight: 0.5,
		KeywordWeight:  0.5,
	})

	normalized := normalizeRequest(req)

	require.Equal(t, 15, normalized.Limit)
	require.InDelta(t, 0.8, normalized.MinSimilarity, 0.001)
	require.InDelta(t, 0.5, normalized.SemanticWeight, 0.001)
	require.InDelta(t, 0.5, normalized.KeywordWeight, 0.001)
	require.Equal(t, filter, normalized.Filter)
	require.Equal(t, "tenant1", normalized.TenantID)
	require.Equal(t, "test", normalized.Query)
}

func TestNormalizeRequest_NegativeLimit(t *testing.T) {
	t.Parallel()
	req := ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
		Limit:    -5,
	})

	normalized := normalizeRequest(req)
	require.Equal(t, DefaultLimit, normalized.Limit)
}

func TestKeywordOnlySearch_Error(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		keywordErr: errors.New("keyword search db error"),
	})
	svc := ptrext.Of(service{feedbackStore: store})

	resp, err := svc.keywordOnlySearch(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
		Limit:    10,
	}))

	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "keyword search")
}

func TestKeywordOnlySearch_EmptyResults(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		keywordResults: []*feedback.SearchFeedback{},
	})
	svc := ptrext.Of(service{feedbackStore: store})

	resp, err := svc.keywordOnlySearch(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
		Limit:    10,
	}))

	require.NoError(t, err)
	require.Empty(t, resp.Hits)
	require.True(t, resp.UsedKeywordFallback)
	require.Equal(t, fallbackReasonSemanticUnavailable, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
	require.Equal(t, "", resp.EmbeddingModel)
	require.Equal(t, 0, resp.TotalWithEmbeddings)
}

func TestKeywordOnlySearch_ScoreFloorAt01(t *testing.T) {
	t.Parallel()
	// Build enough results so that position-based scoring would go below 0.1
	// if not clamped. Position 19: 1.0 - 19*0.05 = 0.05 -> clamped to 0.1.
	results := make([]*feedback.SearchFeedback, 25)
	for i := range results {
		results[i] = ptrext.Of(feedback.SearchFeedback{ID: int64(i + 1), Content: "item"})
	}
	store := ptrext.Of(mockFeedbackStore{keywordResults: results})
	svc := ptrext.Of(service{feedbackStore: store})

	resp, err := svc.keywordOnlySearch(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
		Limit:    30,
	}))

	require.NoError(t, err)
	require.Len(t, resp.Hits, 25)
	// Last hit (position 24) would be 1.0 - 24*0.05 = -0.2, clamped to 0.1.
	require.InDelta(t, 0.1, resp.Hits[24].KeywordScore, 0.001)
	// Position 18: 1.0 - 18*0.05 = 0.1.
	require.InDelta(t, 0.1, resp.Hits[18].KeywordScore, 0.001)
}

func TestKeywordOnlySearchWithMetrics_Error(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		keywordErr: errors.New("db error"),
	})
	svc := ptrext.Of(service{feedbackStore: store})

	resp, err := svc.keywordOnlySearchWithMetrics(
		context.Background(),
		ptrext.Of(SearchRequest{TenantID: "t1", Query: "q", Limit: 10}),
		time.Now(),
	)

	require.Error(t, err)
	require.Nil(t, resp)
}

func TestHybridSearch_Success(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		semanticHits: []feedback.SemanticSearchHit{
			{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "semantic hit"}), Similarity: 0.9},
		},
		keywordResults: []*feedback.SearchFeedback{
			{ID: 2, Content: "keyword hit"},
		},
	})
	svc := ptrext.Of(service{feedbackStore: store})

	resp, err := svc.hybridSearch(
		context.Background(),
		ptrext.Of(SearchRequest{
			TenantID:       "tenant1",
			Query:          "test",
			Limit:          10,
			MinSimilarity:  0.5,
			SemanticWeight: 0.7,
			KeywordWeight:  0.3,
		}),
		[]float32{0.1, 0.2},
		"text-embedding-3-small",
		100,
		nil,
	)

	require.NoError(t, err)
	require.Len(t, resp.Hits, 2)
	require.Equal(t, "text-embedding-3-small", resp.EmbeddingModel)
	require.Equal(t, 100, resp.TotalWithEmbeddings)
	require.False(t, resp.UsedKeywordFallback)
	require.Empty(t, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
	require.Equal(t, 1, store.semanticCallCount)
	require.Equal(t, 1, store.lexicalCallCount)
}

func TestSearch_NormalizesRequestBeforeHybridRepos(t *testing.T) {
	t.Parallel()
	embedding := make([]float32, EmbeddingDims)
	filter := ptrext.Of(feedback.FeedbackFilter{Q: "invoice"})
	store := ptrext.Of(mockFeedbackStore{
		hasEmbedding: true,
		stats: ptrext.Of(feedback.EmbeddingStats{
			EmbeddedCount:  5,
			EmbeddingModel: "model-a",
		}),
		semanticHits: []feedback.SemanticSearchHit{
			{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "semantic"}), Similarity: 0.9},
		},
		lexicalResults: []feedback.LexicalSearchHit{
			{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 2, Content: "lexical"}), Rank: 1, Score: 0.8},
		},
	})
	cache := ptrext.Of(mockCache{
		getEmbedding: embedding,
		getModel:     "model-a",
		getFound:     true,
	})
	svc := New(store, nil, nil, cache)

	resp, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID:      "tenant1",
		Query:         "  invoice latency  ",
		Limit:         7,
		MinSimilarity: 0.42,
		Filter:        filter,
	}))

	require.NoError(t, err)
	require.False(t, resp.UsedKeywordFallback)
	require.Equal(t, 1, store.semanticCallCount)
	require.Equal(t, 1, store.lexicalCallCount)
	require.NotNil(t, store.semanticParams)
	require.Equal(t, "tenant1", store.semanticParams.TenantID)
	require.Equal(t, candidateLimit(7), store.semanticParams.Limit)
	require.InDelta(t, 0.42, store.semanticParams.MinSimilarity, 0.0001)
	require.Equal(t, "model-a", store.semanticParams.EmbeddingModel)
	require.Same(t, filter, store.semanticParams.Filter)
	require.NotNil(t, store.lexicalParams)
	require.Equal(t, "invoice latency", store.lexicalParams.Query)
	require.Equal(t, candidateLimit(7), store.lexicalParams.Limit)
	require.Same(t, filter, store.lexicalParams.Filter)
}

func TestHybridSearch_SemanticError_FallsBackToKeyword(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		semanticErr: errors.New("vector search failed"),
		keywordResults: []*feedback.SearchFeedback{
			{ID: 1, Content: "keyword fallback"},
		},
	})
	svc := ptrext.Of(service{feedbackStore: store})

	resp, err := svc.hybridSearch(
		context.Background(),
		ptrext.Of(SearchRequest{
			TenantID:       "tenant1",
			Query:          "test",
			Limit:          10,
			SemanticWeight: 0.7,
			KeywordWeight:  0.3,
		}),
		[]float32{0.1, 0.2},
		"text-embedding-3-small",
		50,
		nil,
	)

	require.NoError(t, err)
	require.Len(t, resp.Hits, 1)
	require.True(t, resp.UsedKeywordFallback)
	require.Equal(t, fallbackReasonSemanticSearchFailed, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
	require.Equal(t, "keyword", resp.Hits[0].MatchType)
	require.Equal(t, 1, store.semanticCallCount)
}

func TestHybridSearch_KeywordError_ContinuesWithSemanticOnly(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		semanticHits: []feedback.SemanticSearchHit{
			{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "semantic"}), Similarity: 0.9},
		},
		keywordErr: errors.New("keyword search failed"),
	})
	svc := ptrext.Of(service{feedbackStore: store})

	resp, err := svc.hybridSearch(
		context.Background(),
		ptrext.Of(SearchRequest{
			TenantID:       "tenant1",
			Query:          "test",
			Limit:          10,
			SemanticWeight: 0.7,
			KeywordWeight:  0.3,
		}),
		[]float32{0.1, 0.2},
		"text-embedding-3-small",
		50,
		nil,
	)

	require.NoError(t, err)
	require.Len(t, resp.Hits, 1)
	require.Equal(t, "semantic", resp.Hits[0].MatchType)
	require.False(t, resp.UsedKeywordFallback)
	require.Empty(t, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
}

func TestHybridSearch_NoSemanticResults_MarksAsFallback(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		semanticHits: []feedback.SemanticSearchHit{}, // empty
		keywordResults: []*feedback.SearchFeedback{
			{ID: 1, Content: "keyword only"},
		},
	})
	svc := ptrext.Of(service{feedbackStore: store})

	resp, err := svc.hybridSearch(
		context.Background(),
		ptrext.Of(SearchRequest{
			TenantID:       "tenant1",
			Query:          "test",
			Limit:          10,
			SemanticWeight: 0.7,
			KeywordWeight:  0.3,
		}),
		[]float32{0.1},
		"model",
		10,
		nil,
	)

	require.NoError(t, err)
	require.True(t, resp.UsedKeywordFallback)
	require.Equal(t, fallbackReasonNoSemanticMatches, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
	require.Contains(t, resp.Hits[0].RankingSignals, "keyword_fallback")
}

func TestHybridSearch_BothErrors_ReturnsKeywordError(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		semanticErr: errors.New("vector fail"),
		keywordErr:  errors.New("keyword fail"),
	})
	svc := ptrext.Of(service{feedbackStore: store})

	resp, err := svc.hybridSearch(
		context.Background(),
		ptrext.Of(SearchRequest{
			TenantID:       "tenant1",
			Query:          "test",
			Limit:          10,
			SemanticWeight: 0.7,
			KeywordWeight:  0.3,
		}),
		[]float32{0.1},
		"model",
		5,
		nil,
	)

	// Semantic error triggers keyword fallback, which also fails.
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "keyword search")
}

func TestHybridSearchWithMetrics_SemanticType(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		semanticHits: []feedback.SemanticSearchHit{
			{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1}), Similarity: 0.9},
		},
		keywordResults: []*feedback.SearchFeedback{},
	})
	svc := ptrext.Of(service{feedbackStore: store})

	resp, err := svc.hybridSearchWithMetrics(
		context.Background(),
		ptrext.Of(SearchRequest{
			TenantID:       "tenant1",
			Query:          "test",
			Limit:          10,
			SemanticWeight: 0.7,
			KeywordWeight:  0.3,
		}),
		[]float32{0.1},
		"model",
		5,
		time.Now(),
		nil,
	)

	require.NoError(t, err)
	require.False(t, resp.UsedKeywordFallback)
	require.Empty(t, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
	// All hits are "semantic" type.
	for _, hit := range resp.Hits {
		require.Equal(t, "semantic", hit.MatchType)
	}
}

func TestHybridSearchWithMetrics_HybridType(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		semanticHits: []feedback.SemanticSearchHit{
			{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1}), Similarity: 0.9},
		},
		keywordResults: []*feedback.SearchFeedback{
			{ID: 1}, // same ID -> hybrid
		},
	})
	svc := ptrext.Of(service{feedbackStore: store})

	resp, err := svc.hybridSearchWithMetrics(
		context.Background(),
		ptrext.Of(SearchRequest{
			TenantID:       "tenant1",
			Query:          "test",
			Limit:          10,
			SemanticWeight: 0.7,
			KeywordWeight:  0.3,
		}),
		[]float32{0.1},
		"model",
		5,
		time.Now(),
		nil,
	)

	require.NoError(t, err)
	require.Len(t, resp.Hits, 1)
	require.Equal(t, "hybrid", resp.Hits[0].MatchType)
	require.Empty(t, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
}

func TestHybridSearchWithMetrics_FallbackType(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		semanticHits:   []feedback.SemanticSearchHit{},
		keywordResults: []*feedback.SearchFeedback{{ID: 1}},
	})
	svc := ptrext.Of(service{feedbackStore: store})

	resp, err := svc.hybridSearchWithMetrics(
		context.Background(),
		ptrext.Of(SearchRequest{
			TenantID:       "tenant1",
			Query:          "test",
			Limit:          10,
			SemanticWeight: 0.7,
			KeywordWeight:  0.3,
		}),
		[]float32{0.1},
		"model",
		5,
		time.Now(),
		nil,
	)

	require.NoError(t, err)
	require.True(t, resp.UsedKeywordFallback)
	require.Equal(t, fallbackReasonNoSemanticMatches, resp.FallbackReason)
	require.Equal(t, RankingVersion, resp.RankingVersion)
}

func TestHybridSearchWithMetrics_Error(t *testing.T) {
	t.Parallel()
	store := ptrext.Of(mockFeedbackStore{
		semanticErr: errors.New("vector fail"),
		keywordErr:  errors.New("keyword fail"),
	})
	svc := ptrext.Of(service{feedbackStore: store})

	resp, err := svc.hybridSearchWithMetrics(
		context.Background(),
		ptrext.Of(SearchRequest{
			TenantID:       "tenant1",
			Query:          "test",
			Limit:          10,
			SemanticWeight: 0.7,
			KeywordWeight:  0.3,
		}),
		[]float32{0.1},
		"model",
		5,
		time.Now(),
		nil,
	)

	require.Error(t, err)
	require.Nil(t, resp)
}

func TestRecordSearchMetrics_RecordsFallbackReasonAndCoverage(t *testing.T) {
	tenantID := "metrics-tenant-fallback-coverage"
	beforeFallback := metricValue(t,
		metrics.SearchFallbackReasonsTotal.WithLabelValues(tenantID, fallbackReasonNoEmbeddings),
	)

	recordSearchMetrics(tenantID, "keyword_fallback", time.Millisecond, ptrext.Of(SearchResponse{
		Hits: []*SearchHit{
			{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1})},
		},
		FallbackReason: fallbackReasonNoEmbeddings,
		Coverage: ptrext.Of(SearchCoverage{
			TotalLiveFeedback:   10,
			TotalWithEmbeddings: 7,
			EmbeddingModel:      "model-a",
		}),
	}))

	require.InDelta(t, beforeFallback+1, metricValue(t,
		metrics.SearchFallbackReasonsTotal.WithLabelValues(tenantID, fallbackReasonNoEmbeddings),
	), 0.0001)
	require.InDelta(t, 0.7, metricValue(t,
		metrics.SearchEmbeddingCoverageRatio.WithLabelValues(tenantID, "model-a"),
	), 0.0001)
}

func TestRecordSearchHealthMetrics_ClampsCoverageAndNormalizesEmptyModel(t *testing.T) {
	tenantID := "metrics-tenant-coverage-clamp"

	recordSearchHealthMetrics(tenantID, ptrext.Of(SearchResponse{
		Coverage: ptrext.Of(SearchCoverage{
			TotalLiveFeedback:   10,
			TotalWithEmbeddings: 25,
		}),
	}))

	require.Equal(t, 1.0, metricValue(t,
		metrics.SearchEmbeddingCoverageRatio.WithLabelValues(tenantID, "none"),
	))
}

func metricValue(t *testing.T, metric interface{ Write(*dto.Metric) error }) float64 {
	t.Helper()
	pb := ptrext.Of(dto.Metric{})
	require.NoError(t, metric.Write(pb))
	if pb.Counter != nil {
		return pb.Counter.GetValue()
	}
	if pb.Gauge != nil {
		return pb.Gauge.GetValue()
	}
	t.Fatalf("metric has neither counter nor gauge value")
	return 0
}

func TestMergeResults_Deduplication(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(service{})

	semanticHits := []feedback.SemanticSearchHit{
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "a"}), Similarity: 0.9},
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 2, Content: "b"}), Similarity: 0.8},
	}
	lexicalResults := lexicalHitsFromFeedback([]feedback.SearchFeedback{
		{ID: 2, Content: "b"}, // duplicate
		{ID: 3, Content: "c"},
	})

	merged := svc.mergeResults(semanticHits, lexicalResults, 0.7, 0.3, 10)

	require.Len(t, merged, 3)

	// Check that ID=2 is marked as hybrid.
	var found bool
	for _, hit := range merged {
		if hit.Feedback.ID == 2 {
			found = true
			require.Equal(t, "hybrid", hit.MatchType)
			require.NotZero(t, hit.Similarity)
			require.NotZero(t, hit.KeywordScore)
		}
	}
	require.True(t, found, "ID=2 not found in merged results")
}

func TestMergeResults_DuplicateSemanticHitsKeepFirstRank(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(service{})

	semanticHits := []feedback.SemanticSearchHit{
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "first"}), Similarity: 0.9},
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "duplicate"}), Similarity: 0.99},
	}

	merged := svc.mergeResults(semanticHits, nil, 0.7, 0.3, 10)

	require.Len(t, merged, 1)
	require.Equal(t, int64(1), merged[0].Feedback.ID)
	require.Equal(t, "semantic", merged[0].MatchType)
	require.Equal(t, 1, merged[0].SemanticRank)
	require.InDelta(t, 0.9, merged[0].Similarity, 0.0001)
	require.InDelta(t, rrfScore(1, 0.7), merged[0].FusedScore, 0.0001)
}

func TestMergeResults_DuplicateLexicalHitsDoNotInflateOrBecomeHybrid(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(service{})

	lexicalResults := []feedback.LexicalSearchHit{
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "first"}), Rank: 1, Score: 0.9},
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "duplicate"}), Rank: 2, Score: 0.8},
	}

	merged := svc.mergeResults(nil, lexicalResults, 0.7, 0.3, 10)

	require.Len(t, merged, 1)
	require.Equal(t, int64(1), merged[0].Feedback.ID)
	require.Equal(t, "keyword", merged[0].MatchType)
	require.Equal(t, 1, merged[0].LexicalRank)
	require.InDelta(t, 0.9, merged[0].KeywordScore, 0.0001)
	require.InDelta(t, rrfScore(1, 0.3), merged[0].FusedScore, 0.0001)
	require.NotContains(t, merged[0].RankingSignals, "hybrid")
}

func TestMergeResults_DuplicateLexicalHitsDoNotInflateHybridScore(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(service{})

	semanticHits := []feedback.SemanticSearchHit{
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "semantic"}), Similarity: 0.9},
	}
	lexicalResults := []feedback.LexicalSearchHit{
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "lexical"}), Rank: 1, Score: 0.9},
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "duplicate"}), Rank: 2, Score: 0.8},
	}

	merged := svc.mergeResults(semanticHits, lexicalResults, 0.7, 0.3, 10)

	require.Len(t, merged, 1)
	require.Equal(t, "hybrid", merged[0].MatchType)
	require.InDelta(t, rrfScore(1, 0.7)+rrfScore(1, 0.3), merged[0].FusedScore, 0.0001)
}

func TestMergeResults_NilSemanticFeedback_Skipped(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(service{})

	semanticHits := []feedback.SemanticSearchHit{
		{Feedback: nil, Similarity: 0.9},
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "real"}), Similarity: 0.8},
	}
	var lexicalResults []feedback.LexicalSearchHit

	merged := svc.mergeResults(semanticHits, lexicalResults, 0.7, 0.3, 10)

	require.Len(t, merged, 1)
	require.Equal(t, int64(1), merged[0].Feedback.ID)
}

func TestMergeResults_NilKeywordFeedback_Skipped(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(service{})

	var semanticHits []feedback.SemanticSearchHit
	lexicalResults := []feedback.LexicalSearchHit{
		{Feedback: nil, Rank: 1},
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "real"}), Rank: 2},
		{Feedback: nil, Rank: 3},
	}

	merged := svc.mergeResults(semanticHits, lexicalResults, 0.7, 0.3, 10)

	require.Len(t, merged, 1)
	require.Equal(t, int64(1), merged[0].Feedback.ID)
}

func TestMergeResults_SortedByRRFScore(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(service{})

	semanticHits := []feedback.SemanticSearchHit{
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1}), Similarity: 0.9},
	}
	lexicalResults := lexicalHitsFromFeedback([]feedback.SearchFeedback{{ID: 2}})

	merged := svc.mergeResults(semanticHits, lexicalResults, 0.7, 0.3, 10)

	require.Equal(t, int64(1), merged[0].Feedback.ID)
	require.Greater(t, merged[0].FusedScore, merged[1].FusedScore)
}

func TestMergeResults_Limit(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(service{})

	semanticHits := []feedback.SemanticSearchHit{
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1}), Similarity: 0.9},
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 2}), Similarity: 0.8},
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 3}), Similarity: 0.7},
	}
	var lexicalResults []feedback.LexicalSearchHit

	merged := svc.mergeResults(semanticHits, lexicalResults, 0.7, 0.3, 2)

	require.Len(t, merged, 2)
}

func TestMergeResults_EmptyInputs(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(service{})

	merged := svc.mergeResults(nil, nil, 0.7, 0.3, 10)

	require.Empty(t, merged)
}

func TestMergeResults_KeywordScoreFloor(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(service{})

	var semanticHits []feedback.SemanticSearchHit
	// 25 keyword results: position 24 -> 1.0 - 24*0.05 = -0.2 -> clamped to 0.1.
	lexicalResults := make([]feedback.LexicalSearchHit, 25)
	for i := range lexicalResults {
		lexicalResults[i] = feedback.LexicalSearchHit{
			Feedback: ptrext.Of(feedback.SearchFeedback{ID: int64(i + 1)}),
			Rank:     i + 1,
		}
	}

	merged := svc.mergeResults(semanticHits, lexicalResults, 0.7, 0.3, 30)

	require.Len(t, merged, 25)
	// All scores should be >= 0.1.
	for _, hit := range merged {
		require.GreaterOrEqual(t, hit.KeywordScore, 0.1)
	}
}

func TestGetOrGenerateEmbedding_NilRouter(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(service{router: nil})

	_, _, err := svc.getOrGenerateEmbedding(context.Background(), "tenant1", "test query")

	require.Error(t, err)
	require.Contains(t, err.Error(), "llm router not configured")
}

func TestGetOrGenerateEmbedding_CacheHit(t *testing.T) {
	t.Parallel()
	cachedEmb := make([]float32, EmbeddingDims)
	for i := range cachedEmb {
		cachedEmb[i] = float32(i) * 0.01
	}
	cache := ptrext.Of(mockCache{
		getEmbedding: cachedEmb,
		getModel:     "text-embedding-3-small",
		getFound:     true,
	})
	svc := ptrext.Of(service{cache: cache, router: nil})

	emb, model, err := svc.getOrGenerateEmbedding(context.Background(), "tenant1", "test query")

	require.NoError(t, err)
	require.Len(t, emb, EmbeddingDims)
	require.Equal(t, "text-embedding-3-small", model)
	require.Equal(t, 1, cache.getCalls)
	require.Equal(t, 0, cache.setCalls) // no set needed on hit
}

func TestGetOrGenerateEmbedding_CacheHit_WrongDims(t *testing.T) {
	t.Parallel()
	// Cache returns embedding with wrong dimensions -- should be treated as miss.
	cache := ptrext.Of(mockCache{
		getEmbedding: []float32{0.1, 0.2, 0.3}, // only 3 dims, not EmbeddingDims
		getModel:     "text-embedding-3-small",
		getFound:     true,
	})
	svc := ptrext.Of(service{cache: cache, router: nil})

	// Falls through to router which is nil -> error.
	_, _, err := svc.getOrGenerateEmbedding(context.Background(), "tenant1", "test query")

	require.Error(t, err)
	require.Contains(t, err.Error(), "llm router not configured")
}

func TestGetOrGenerateEmbedding_CacheHit_EmptyModel(t *testing.T) {
	t.Parallel()
	cachedEmb := make([]float32, EmbeddingDims)
	cache := ptrext.Of(mockCache{
		getEmbedding: cachedEmb,
		getModel:     "", // empty model
		getFound:     true,
	})
	svc := ptrext.Of(service{cache: cache, router: nil})

	// Falls through to router which is nil -> error.
	_, _, err := svc.getOrGenerateEmbedding(context.Background(), "tenant1", "test query")

	require.Error(t, err)
	require.Contains(t, err.Error(), "llm router not configured")
}

func TestGetOrGenerateEmbedding_CacheGetError_ContinuesWithoutCache(t *testing.T) {
	t.Parallel()
	cache := ptrext.Of(mockCache{
		getErr: errors.New("redis timeout"),
	})
	// Router is nil, so it fails at the "generate" step, proving we got past the cache error.
	svc := ptrext.Of(service{cache: cache, router: nil})

	_, _, err := svc.getOrGenerateEmbedding(context.Background(), "tenant1", "test query")

	require.Error(t, err)
	require.Contains(t, err.Error(), "llm router not configured")
	require.Equal(t, 1, cache.getCalls)
}

func TestGetOrGenerateEmbedding_NilCache_NilRouter(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(service{cache: nil, router: nil})

	_, _, err := svc.getOrGenerateEmbedding(context.Background(), "tenant1", "test query")

	require.Error(t, err)
	require.Contains(t, err.Error(), "llm router not configured")
}

func TestGetEmbedding_DelegatesToGetOrGenerate(t *testing.T) {
	t.Parallel()
	cachedEmb := make([]float32, EmbeddingDims)
	cache := ptrext.Of(mockCache{
		getEmbedding: cachedEmb,
		getModel:     "text-embedding-3-small",
		getFound:     true,
	})
	svc := New(nil, nil, nil, cache)

	emb, model, err := svc.GetEmbedding(context.Background(), "tenant1", "test query")

	require.NoError(t, err)
	require.Len(t, emb, EmbeddingDims)
	require.Equal(t, "text-embedding-3-small", model)
}

func TestGetEmbedding_Error(t *testing.T) {
	t.Parallel()
	svc := New(nil, nil, nil, nil)

	_, _, err := svc.GetEmbedding(context.Background(), "tenant1", "test query")

	require.Error(t, err)
}

func TestHashQuery_Consistent(t *testing.T) {
	t.Parallel()
	hash1 := hashQuery("tenant1", "test query")
	hash2 := hashQuery("tenant1", "test query")
	hash3 := hashQuery("tenant2", "test query")

	require.Equal(t, hash1, hash2, "same input should produce same hash")
	require.NotEqual(t, hash1, hash3, "different tenant should produce different hash")
	require.Len(t, hash1, 32)
}

func TestHashQuery_DifferentQueries(t *testing.T) {
	t.Parallel()
	hash1 := hashQuery("tenant1", "query A")
	hash2 := hashQuery("tenant1", "query B")

	require.NotEqual(t, hash1, hash2, "different queries should produce different hashes")
}

func TestMemoryCache_Basic(t *testing.T) {
	t.Parallel()
	cache := NewMemoryCache()
	ctx := context.Background()

	// Initially empty.
	_, _, found, err := cache.Get(ctx, "tenant1", "hash1")
	require.NoError(t, err)
	require.False(t, found)

	// Set a value.
	testEmb := []float32{0.1, 0.2, 0.3}
	testModel := "text-embedding-3-small"
	err = cache.Set(ctx, "tenant1", "hash1", testEmb, testModel, time.Hour)
	require.NoError(t, err)

	// Should find it now.
	emb, model, found, err := cache.Get(ctx, "tenant1", "hash1")
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, emb, 3)
	require.Equal(t, testModel, model)
}

func TestMemoryCache_Expiration(t *testing.T) {
	t.Parallel()
	cache := ptrext.Of(memoryCache{entries: make(map[string]memoryCacheEntry)})
	ctx := context.Background()

	// Set with negative TTL (already expired).
	testEmb := []float32{0.1, 0.2, 0.3}
	err := cache.Set(ctx, "tenant1", "hash1", testEmb, "text-embedding-3-small", -time.Second)
	require.NoError(t, err)

	// Should not find expired entry.
	_, _, found, err := cache.Get(ctx, "tenant1", "hash1")
	require.NoError(t, err)
	require.False(t, found)
}

func TestVecToString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    []float32
		expected string
	}{
		{"empty", nil, "[]"},
		{"single", []float32{1.5}, "[1.5]"},
		{"multiple", []float32{1.0, 2.5, 3.0}, "[1,2.5,3]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := vecToString(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestParseVecString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		input       string
		expectedLen int
		expectErr   bool
	}{
		{"empty brackets", "[]", 0, false},
		{"single", "[1.5]", 1, false},
		{"multiple", "[1.0,2.5,3.0]", 3, false},
		{"with spaces", "[ 1.0 , 2.0 ]", 2, false},
		{"invalid format", "1,2,3", 0, true},
		{"invalid float", "[1.0,abc]", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := parseVecString(tt.input)
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, result, tt.expectedLen)
		})
	}
}
