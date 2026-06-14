// SPDX-License-Identifier: Apache-2.0

package semanticsearch

import (
	"context"
	"errors"
	"testing"
	"time"

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
	keywordResults    []*feedback.SearchFeedback
	keywordErr        error
	semanticCallCount int
	keywordCallCount  int
}

func (m *mockFeedbackStore) SemanticSearch(ctx context.Context, params *feedback.SemanticSearchParams) ([]feedback.SemanticSearchHit, error) {
	m.semanticCallCount++
	return m.semanticHits, m.semanticErr
}

func (m *mockFeedbackStore) KeywordSearch(ctx context.Context, params *feedback.KeywordSearchParams) ([]*feedback.SearchFeedback, error) {
	m.keywordCallCount++
	return m.keywordResults, m.keywordErr
}

func (m *mockFeedbackStore) HasEmbedding(ctx context.Context, tenantID string) (bool, error) {
	return m.hasEmbedding, m.hasEmbeddingErr
}

func (m *mockFeedbackStore) GetEmbeddingStats(ctx context.Context, tenantID string) (*feedback.EmbeddingStats, error) {
	return m.stats, m.statsErr
}

func TestSearch_EmptyQuery(t *testing.T) {
	svc := New(nil, nil, nil, nil)

	_, err := svc.Search(context.Background(), ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "",
	}))

	if !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("expected ErrEmptyQuery, got %v", err)
	}
}

func TestSearch_NilRequest(t *testing.T) {
	svc := New(nil, nil, nil, nil)

	_, err := svc.Search(context.Background(), nil)

	if !errors.Is(err, ErrEmptyQuery) {
		t.Errorf("expected ErrEmptyQuery, got %v", err)
	}
}

func TestSearch_RateLimited(t *testing.T) {
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

	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("expected ErrRateLimited, got %v", err)
	}
}

func TestSearch_NoEmbeddings_FallsBackToKeyword(t *testing.T) {
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.UsedKeywordFallback {
		t.Error("expected UsedKeywordFallback=true")
	}
	if len(resp.Hits) != 2 {
		t.Errorf("expected 2 hits, got %d", len(resp.Hits))
	}
	if resp.Hits[0].MatchType != "keyword" {
		t.Errorf("expected match_type='keyword', got %q", resp.Hits[0].MatchType)
	}
	if store.keywordCallCount != 1 {
		t.Errorf("expected 1 keyword search call, got %d", store.keywordCallCount)
	}
}

func TestSearch_HasEmbeddingError_FallsBackToKeyword(t *testing.T) {
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.UsedKeywordFallback {
		t.Error("expected UsedKeywordFallback=true")
	}
}

func TestSearch_KeywordScoreDescending(t *testing.T) {
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
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// First result should have highest keyword score.
	if resp.Hits[0].KeywordScore <= resp.Hits[1].KeywordScore {
		t.Errorf("first hit score (%f) should be > second (%f)",
			resp.Hits[0].KeywordScore, resp.Hits[1].KeywordScore)
	}
	if resp.Hits[1].KeywordScore <= resp.Hits[2].KeywordScore {
		t.Errorf("second hit score (%f) should be > third (%f)",
			resp.Hits[1].KeywordScore, resp.Hits[2].KeywordScore)
	}
}

func TestNormalizeRequest_Defaults(t *testing.T) {
	req := ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
	})

	normalized := normalizeRequest(req)

	if normalized.Limit != DefaultLimit {
		t.Errorf("expected Limit=%d, got %d", DefaultLimit, normalized.Limit)
	}
	if normalized.MinSimilarity != DefaultMinSimilarity {
		t.Errorf("expected MinSimilarity=%f, got %f", DefaultMinSimilarity, normalized.MinSimilarity)
	}
	if normalized.SemanticWeight != DefaultSemanticWeight {
		t.Errorf("expected SemanticWeight=%f, got %f", DefaultSemanticWeight, normalized.SemanticWeight)
	}
	if normalized.KeywordWeight != DefaultKeywordWeight {
		t.Errorf("expected KeywordWeight=%f, got %f", DefaultKeywordWeight, normalized.KeywordWeight)
	}
}

func TestNormalizeRequest_LimitCap(t *testing.T) {
	req := ptrext.Of(SearchRequest{
		TenantID: "tenant1",
		Query:    "test",
		Limit:    500,
	})

	normalized := normalizeRequest(req)

	if normalized.Limit != MaxLimit {
		t.Errorf("expected Limit=%d, got %d", MaxLimit, normalized.Limit)
	}
}

func TestMergeResults_Deduplication(t *testing.T) {
	svc := ptrext.Of(service{})

	semanticHits := []feedback.SemanticSearchHit{
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1, Content: "a"}), Similarity: 0.9},
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 2, Content: "b"}), Similarity: 0.8},
	}
	keywordResults := []*feedback.SearchFeedback{
		{ID: 2, Content: "b"}, // duplicate
		{ID: 3, Content: "c"},
	}

	merged := svc.mergeResults(semanticHits, keywordResults, 0.7, 0.3, 10)

	// Should have 3 unique items.
	if len(merged) != 3 {
		t.Errorf("expected 3 merged results, got %d", len(merged))
	}

	// Check that ID=2 is marked as hybrid.
	var found bool
	for _, hit := range merged {
		if hit.Feedback.ID == 2 {
			found = true
			if hit.MatchType != "hybrid" {
				t.Errorf("expected ID=2 match_type='hybrid', got %q", hit.MatchType)
			}
			if hit.Similarity == 0 {
				t.Error("expected ID=2 to have semantic similarity")
			}
			if hit.KeywordScore == 0 {
				t.Error("expected ID=2 to have keyword score")
			}
		}
	}
	if !found {
		t.Error("ID=2 not found in merged results")
	}
}

func TestMergeResults_SortedByWeightedScore(t *testing.T) {
	svc := ptrext.Of(service{})

	// High semantic, low keyword.
	semanticHits := []feedback.SemanticSearchHit{
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1}), Similarity: 0.9},
	}
	// Low semantic (not found), high keyword.
	keywordResults := []*feedback.SearchFeedback{
		{ID: 2},
	}

	// With default weights (0.7 semantic, 0.3 keyword):
	// ID=1: 0.9*0.7 + 0*0.3 = 0.63
	// ID=2: 0*0.7 + 1.0*0.3 = 0.30
	merged := svc.mergeResults(semanticHits, keywordResults, 0.7, 0.3, 10)

	if merged[0].Feedback.ID != 1 {
		t.Errorf("expected ID=1 first (higher weighted score), got ID=%d", merged[0].Feedback.ID)
	}
}

func TestMergeResults_Limit(t *testing.T) {
	svc := ptrext.Of(service{})

	semanticHits := []feedback.SemanticSearchHit{
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 1}), Similarity: 0.9},
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 2}), Similarity: 0.8},
		{Feedback: ptrext.Of(feedback.SearchFeedback{ID: 3}), Similarity: 0.7},
	}
	var keywordResults []*feedback.SearchFeedback

	merged := svc.mergeResults(semanticHits, keywordResults, 0.7, 0.3, 2)

	if len(merged) != 2 {
		t.Errorf("expected 2 results after limit, got %d", len(merged))
	}
}

func TestHashQuery_Consistent(t *testing.T) {
	hash1 := hashQuery("tenant1", "test query")
	hash2 := hashQuery("tenant1", "test query")
	hash3 := hashQuery("tenant2", "test query")

	if hash1 != hash2 {
		t.Error("same input should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("different tenant should produce different hash")
	}
	if len(hash1) != 32 {
		t.Errorf("expected hash length 32, got %d", len(hash1))
	}
}

func TestMemoryCache_Basic(t *testing.T) {
	cache := NewMemoryCache()
	ctx := context.Background()

	// Initially empty.
	_, found, err := cache.Get(ctx, "tenant1", "hash1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected not found")
	}

	// Set a value.
	testEmb := []float32{0.1, 0.2, 0.3}
	err = cache.Set(ctx, "tenant1", "hash1", testEmb, time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should find it now.
	emb, found, err := cache.Get(ctx, "tenant1", "hash1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Error("expected found")
	}
	if len(emb) != 3 {
		t.Errorf("expected 3 elements, got %d", len(emb))
	}
}

func TestMemoryCache_Expiration(t *testing.T) {
	cache := ptrext.Of(memoryCache{entries: make(map[string]memoryCacheEntry)})
	ctx := context.Background()

	// Set with very short TTL.
	testEmb := []float32{0.1, 0.2, 0.3}
	err := cache.Set(ctx, "tenant1", "hash1", testEmb, -time.Second) // Already expired
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not find expired entry.
	_, found, err := cache.Get(ctx, "tenant1", "hash1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected expired entry not found")
	}
}

func TestVecToString(t *testing.T) {
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
			result := vecToString(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestParseVecString(t *testing.T) {
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
			result, err := parseVecString(tt.input)
			if tt.expectErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if len(result) != tt.expectedLen {
				t.Errorf("expected length %d, got %d", tt.expectedLen, len(result))
			}
		})
	}
}
