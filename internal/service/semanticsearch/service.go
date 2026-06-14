// SPDX-License-Identifier: Apache-2.0

package semanticsearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/service/llmrouter"
)

var (
	// ErrRateLimited indicates the tenant has exceeded the search rate limit.
	ErrRateLimited = errors.New("search rate limit exceeded")
	// ErrEmptyQuery indicates an empty search query.
	ErrEmptyQuery = errors.New("search query is empty")
)

// FeedbackStore is the subset of feedback.FeedbackRepo needed for search.
type FeedbackStore interface {
	SemanticSearch(ctx context.Context, params *feedback.SemanticSearchParams) ([]feedback.SemanticSearchHit, error)
	KeywordSearch(ctx context.Context, params *feedback.KeywordSearchParams) ([]*feedback.SearchFeedback, error)
	HasEmbedding(ctx context.Context, tenantID string) (bool, error)
	GetEmbeddingStats(ctx context.Context, tenantID string) (*feedback.EmbeddingStats, error)
}

// service implements Service.
type service struct {
	feedbackStore FeedbackStore
	router        *llmrouter.Router
	rateLimiter   ratelimit.SlidingLimiter
	cache         EmbeddingCache
}

// New creates a new semantic search service.
func New(
	feedbackStore FeedbackStore,
	router *llmrouter.Router,
	rateLimiter ratelimit.SlidingLimiter,
	cache EmbeddingCache,
) Service {
	return ptrext.Of(service{
		feedbackStore: feedbackStore,
		router:        router,
		rateLimiter:   rateLimiter,
		cache:         cache,
	})
}

// Search performs semantic search with keyword fallback.
func (s *service) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	const where = "semanticsearch.Service.Search"
	start := time.Now()

	if req == nil || req.Query == "" {
		return nil, ErrEmptyQuery
	}

	logext.Infof(ctx, "[%s] semantic search started,tenant_id:%s,query_length:%d,limit:%d,has_filter:%v",
		where, req.TenantID, len(req.Query), req.Limit, req.Filter != nil)

	// Check rate limit.
	if s.rateLimiter != nil {
		key := fmt.Sprintf(RateLimitKey, req.TenantID)
		allowed, retryAfter, err := s.rateLimiter.Allow(ctx, key, SearchRateLimit, SearchRateWindow)
		if err != nil {
			logext.Warnf(ctx, "[%s] rate limiter error,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
			// Continue on rate limiter errors - fail open.
		} else if !allowed {
			logext.Infof(ctx, "[%s] rate limited,tenant_id:%s,retry_after:%s", where, req.TenantID, retryAfter)
			return nil, ErrRateLimited
		}
	}

	// Normalize request parameters.
	req = normalizeRequest(req)

	// Check if tenant has embeddings.
	hasEmbeddings, err := s.feedbackStore.HasEmbedding(ctx, req.TenantID)
	if err != nil {
		logext.Warnf(ctx, "[%s] check embeddings failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		// Fall back to keyword search on error.
		return s.keywordOnlySearchWithMetrics(ctx, req, start)
	}

	// Get embedding stats for metadata.
	stats, err := s.feedbackStore.GetEmbeddingStats(ctx, req.TenantID)
	if err != nil {
		logext.Warnf(ctx, "[%s] get stats failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		stats = ptrext.Of(feedback.EmbeddingStats{})
	}

	// If no embeddings, use keyword search.
	if !hasEmbeddings || stats.EmbeddingModel == "" {
		logext.Infof(ctx, "[%s] no embeddings available,tenant_id:%s", where, req.TenantID)
		return s.keywordOnlySearchWithMetrics(ctx, req, start)
	}

	// Generate query embedding.
	embedding, model, err := s.getOrGenerateEmbedding(ctx, req.TenantID, req.Query)
	if err != nil {
		logext.Warnf(ctx, "[%s] embedding generation failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		// Fall back to keyword search.
		return s.keywordOnlySearchWithMetrics(ctx, req, start)
	}

	// Check model compatibility.
	if model != stats.EmbeddingModel {
		logext.Warnf(ctx, "[%s] model mismatch,query_model:%s,stored_model:%s,tenant_id:%s",
			where, model, stats.EmbeddingModel, req.TenantID)
		// Fall back to keyword search if models don't match.
		return s.keywordOnlySearchWithMetrics(ctx, req, start)
	}

	// Perform hybrid search.
	return s.hybridSearchWithMetrics(ctx, req, embedding, model, int(stats.EmbeddedCount), start)
}

// GetEmbedding generates an embedding for text.
func (s *service) GetEmbedding(ctx context.Context, tenantID, text string) ([]float32, string, error) {
	return s.getOrGenerateEmbedding(ctx, tenantID, text)
}

// normalizeRequest applies defaults to request parameters.
func normalizeRequest(req *SearchRequest) *SearchRequest {
	normalized := ptrext.Of(SearchRequest{
		TenantID:       req.TenantID,
		Query:          req.Query,
		Limit:          req.Limit,
		MinSimilarity:  req.MinSimilarity,
		Filter:         req.Filter,
		SemanticWeight: req.SemanticWeight,
		KeywordWeight:  req.KeywordWeight,
	})

	if normalized.Limit <= 0 {
		normalized.Limit = DefaultLimit
	}
	if normalized.Limit > MaxLimit {
		normalized.Limit = MaxLimit
	}
	if normalized.MinSimilarity <= 0 {
		normalized.MinSimilarity = DefaultMinSimilarity
	}
	if normalized.SemanticWeight <= 0 {
		normalized.SemanticWeight = DefaultSemanticWeight
	}
	if normalized.KeywordWeight <= 0 {
		normalized.KeywordWeight = DefaultKeywordWeight
	}

	return normalized
}

// keywordOnlySearch performs keyword-only search.
func (s *service) keywordOnlySearch(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	const where = "semanticsearch.Service.keywordOnlySearch"

	results, err := s.feedbackStore.KeywordSearch(ctx, ptrext.Of(feedback.KeywordSearchParams{
		TenantID: req.TenantID,
		Query:    req.Query,
		Limit:    req.Limit,
		Filter:   req.Filter,
	}))
	if err != nil {
		logext.Errorf(ctx, "[%s] keyword search failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		return nil, fmt.Errorf("keyword search: %w", err)
	}

	hits := make([]*SearchHit, 0, len(results))
	for i, fb := range results {
		// Assign descending scores based on position (keyword search is already ranked).
		keywordScore := 1.0 - float64(i)*0.05
		if keywordScore < 0.1 {
			keywordScore = 0.1
		}
		hits = append(hits, ptrext.Of(SearchHit{
			Feedback:     fb,
			Similarity:   0,
			KeywordScore: keywordScore,
			MatchType:    "keyword",
		}))
	}

	return ptrext.Of(SearchResponse{
		Hits:                hits,
		EmbeddingModel:      "",
		TotalWithEmbeddings: 0,
		UsedKeywordFallback: true,
	}), nil
}

// keywordOnlySearchWithMetrics performs keyword-only search and records metrics.
func (s *service) keywordOnlySearchWithMetrics(ctx context.Context, req *SearchRequest, start time.Time) (*SearchResponse, error) {
	const where = "semanticsearch.Service.keywordOnlySearchWithMetrics"

	resp, err := s.keywordOnlySearch(ctx, req)
	if err != nil {
		return nil, err
	}

	elapsed := time.Since(start)
	logext.Infof(ctx, "[%s] semantic search completed,tenant_id:%s,result_count:%d,used_keyword_fallback:true,duration_ms:%d",
		where, req.TenantID, len(resp.Hits), elapsed.Milliseconds())

	// Record metrics.
	metrics.SearchQueriesTotal.WithLabelValues(req.TenantID, "keyword_fallback").Inc()
	metrics.SearchQueryDuration.WithLabelValues(req.TenantID, "keyword_fallback").Observe(elapsed.Seconds())
	metrics.SearchResultsCount.WithLabelValues(req.TenantID).Observe(float64(len(resp.Hits)))

	return resp, nil
}

// hybridSearch performs combined semantic and keyword search.
func (s *service) hybridSearch(
	ctx context.Context,
	req *SearchRequest,
	embedding []float32,
	model string,
	totalWithEmbeddings int,
) (*SearchResponse, error) {
	const where = "semanticsearch.Service.hybridSearch"

	// Perform semantic search.
	semanticHits, err := s.feedbackStore.SemanticSearch(ctx, ptrext.Of(feedback.SemanticSearchParams{
		TenantID:       req.TenantID,
		Embedding:      embedding,
		EmbeddingModel: model,
		Limit:          req.Limit,
		MinSimilarity:  req.MinSimilarity,
		Filter:         req.Filter,
	}))
	if err != nil {
		logext.Warnf(ctx, "[%s] semantic search failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		// Fall back to keyword search.
		return s.keywordOnlySearch(ctx, req)
	}

	// Perform keyword search for hybrid results.
	keywordResults, err := s.feedbackStore.KeywordSearch(ctx, ptrext.Of(feedback.KeywordSearchParams{
		TenantID: req.TenantID,
		Query:    req.Query,
		Limit:    req.Limit,
		Filter:   req.Filter,
	}))
	if err != nil {
		logext.Warnf(ctx, "[%s] keyword search failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		// Continue with semantic results only.
		keywordResults = nil
	}

	// Merge and score results.
	hits := s.mergeResults(semanticHits, keywordResults, req.SemanticWeight, req.KeywordWeight, req.Limit)

	// If no semantic results, mark as fallback.
	usedFallback := len(semanticHits) == 0

	return ptrext.Of(SearchResponse{
		Hits:                hits,
		EmbeddingModel:      model,
		TotalWithEmbeddings: totalWithEmbeddings,
		UsedKeywordFallback: usedFallback,
	}), nil
}

// hybridSearchWithMetrics performs hybrid search and records metrics.
func (s *service) hybridSearchWithMetrics(
	ctx context.Context,
	req *SearchRequest,
	embedding []float32,
	model string,
	totalWithEmbeddings int,
	start time.Time,
) (*SearchResponse, error) {
	const where = "semanticsearch.Service.hybridSearchWithMetrics"

	resp, err := s.hybridSearch(ctx, req, embedding, model, totalWithEmbeddings)
	if err != nil {
		return nil, err
	}

	elapsed := time.Since(start)
	logext.Infof(ctx, "[%s] semantic search completed,tenant_id:%s,result_count:%d,used_keyword_fallback:%v,duration_ms:%d",
		where, req.TenantID, len(resp.Hits), resp.UsedKeywordFallback, elapsed.Milliseconds())

	// Determine search type for metrics.
	searchType := "semantic"
	if resp.UsedKeywordFallback {
		searchType = "keyword_fallback"
	} else {
		// Check if any results are hybrid (have both semantic and keyword scores).
		for _, hit := range resp.Hits {
			if hit.MatchType == "hybrid" {
				searchType = "hybrid"
				break
			}
		}
	}

	// Record metrics.
	metrics.SearchQueriesTotal.WithLabelValues(req.TenantID, searchType).Inc()
	metrics.SearchQueryDuration.WithLabelValues(req.TenantID, searchType).Observe(elapsed.Seconds())
	metrics.SearchResultsCount.WithLabelValues(req.TenantID).Observe(float64(len(resp.Hits)))

	return resp, nil
}

// mergeResults combines semantic and keyword results with deduplication.
func (s *service) mergeResults(
	semanticHits []feedback.SemanticSearchHit,
	keywordResults []*feedback.SearchFeedback,
	semanticWeight float64,
	keywordWeight float64,
	limit int,
) []*SearchHit {
	// Build a map of feedback ID -> combined hit.
	hitMap := make(map[int64]*SearchHit)

	// Add semantic results.
	for _, hit := range semanticHits {
		if hit.Feedback == nil {
			continue
		}
		hitMap[hit.Feedback.ID] = ptrext.Of(SearchHit{
			Feedback:     hit.Feedback,
			Similarity:   hit.Similarity,
			KeywordScore: 0,
			MatchType:    "semantic",
		})
	}

	// Add keyword results, merging with existing semantic hits.
	for i, fb := range keywordResults {
		if fb == nil {
			continue
		}
		// Assign descending keyword scores based on position.
		keywordScore := 1.0 - float64(i)*0.05
		if keywordScore < 0.1 {
			keywordScore = 0.1
		}

		if existing, ok := hitMap[fb.ID]; ok {
			// This item was found by both searches.
			existing.KeywordScore = keywordScore
			existing.MatchType = "hybrid"
		} else {
			hitMap[fb.ID] = ptrext.Of(SearchHit{
				Feedback:     fb,
				Similarity:   0,
				KeywordScore: keywordScore,
				MatchType:    "keyword",
			})
		}
	}

	// Convert to slice and compute combined scores.
	hits := make([]*SearchHit, 0, len(hitMap))
	for _, hit := range hitMap {
		hits = append(hits, hit)
	}

	// Sort by combined weighted score.
	sort.Slice(hits, func(i, j int) bool {
		scoreI := hits[i].Similarity*semanticWeight + hits[i].KeywordScore*keywordWeight
		scoreJ := hits[j].Similarity*semanticWeight + hits[j].KeywordScore*keywordWeight
		return scoreI > scoreJ
	})

	// Trim to limit.
	if len(hits) > limit {
		hits = hits[:limit]
	}

	return hits
}

// getOrGenerateEmbedding gets a cached embedding or generates a new one.
func (s *service) getOrGenerateEmbedding(ctx context.Context, tenantID, text string) ([]float32, string, error) {
	const where = "semanticsearch.Service.getOrGenerateEmbedding"

	// Compute query hash for caching.
	queryHash := hashQuery(tenantID, text)

	// Try cache first.
	if s.cache != nil {
		emb, found, err := s.cache.Get(ctx, tenantID, queryHash)
		if err != nil {
			logext.Warnf(ctx, "[%s] cache get error,tenant_id:%s,err:%+v", where, tenantID, err.Error())
			// Continue without cache.
		} else if found && len(emb) == EmbeddingDims {
			// Cache hit - we need to return the model too, but cache doesn't store it.
			// For now, get stats to determine model (this is a limitation).
			stats, err := s.feedbackStore.GetEmbeddingStats(ctx, tenantID)
			if err == nil && stats.EmbeddingModel != "" {
				metrics.EmbeddingCacheHits.WithLabelValues(tenantID, "hit").Inc()
				return emb, stats.EmbeddingModel, nil
			}
			// Fall through to regenerate if we can't determine the model.
		}
	}

	// Cache miss - generate embedding via LLM router.
	metrics.EmbeddingCacheHits.WithLabelValues(tenantID, "miss").Inc()

	if s.router == nil {
		return nil, "", fmt.Errorf("llm router not configured")
	}

	resp, err := s.router.Embed(ctx, llmrouter.EmbeddingRequest{
		TenantID:   tenantID,
		Input:      []string{text},
		Dimensions: EmbeddingDims,
		UserID:     fmt.Sprintf("search:%s", tenantID),
	})
	if err != nil {
		return nil, "", fmt.Errorf("embedding generation: %w", err)
	}

	if len(resp.Embeddings) == 0 || len(resp.Embeddings[0]) == 0 {
		return nil, "", fmt.Errorf("empty embedding response")
	}

	embedding := resp.Embeddings[0]
	model := resp.Route.ProviderModel

	// Cache the embedding.
	if s.cache != nil {
		if err := s.cache.Set(ctx, tenantID, queryHash, embedding, QueryCacheTTL); err != nil {
			logext.Warnf(ctx, "[%s] cache set error,tenant_id:%s,err:%+v", where, tenantID, err.Error())
			// Continue without caching.
		}
	}

	return embedding, model, nil
}

// hashQuery generates a hash key for caching query embeddings.
func hashQuery(tenantID, query string) string {
	h := sha256.New()
	h.Write([]byte(tenantID))
	h.Write([]byte(":"))
	h.Write([]byte(query))
	return hex.EncodeToString(h.Sum(nil))[:32]
}
