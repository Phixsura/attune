// SPDX-License-Identifier: Apache-2.0

package semanticsearch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
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
	// ErrQueryTooLong indicates the query exceeds MaxQueryRunes.
	ErrQueryTooLong = errors.New("search query is too long")
	// ErrInvalidQuery indicates the query contains unsupported control characters.
	ErrInvalidQuery = errors.New("search query contains unsupported control characters")
)

const (
	fallbackReasonSemanticUnavailable     = "semantic_unavailable"
	fallbackReasonEmbeddingCheckFailed    = "embedding_check_failed"
	fallbackReasonEmbeddingStatsFailed    = "embedding_stats_failed"
	fallbackReasonNoEmbeddings            = "no_embeddings"
	fallbackReasonEmbeddingGenerationFail = "embedding_generation_failed"
	fallbackReasonEmbeddingModelMismatch  = "embedding_model_mismatch"
	fallbackReasonSemanticSearchFailed    = "semantic_search_failed"
	fallbackReasonNoSemanticMatches       = "no_semantic_matches"
)

// FeedbackStore is the subset of feedback.FeedbackRepo needed for search.
type FeedbackStore interface {
	SemanticSearch(ctx context.Context, params *feedback.SemanticSearchParams) ([]feedback.SemanticSearchHit, error)
	LexicalSearch(ctx context.Context, params *feedback.LexicalSearchParams) ([]feedback.LexicalSearchHit, error)
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

	query := ""
	if req != nil {
		var err error
		query, err = NormalizeQuery(req.Query)
		if err != nil {
			return nil, err
		}
	}
	if query == "" {
		return nil, ErrEmptyQuery
	}
	req.Query = query

	logext.Infof(ctx, "[%s] semantic search started,tenant_id:%s,query_length:%d,limit:%d,has_filter:%v",
		where, req.TenantID, len([]rune(query)), req.Limit, req.Filter != nil)

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
		return s.keywordOnlySearchWithMetricsReason(ctx, req, start, fallbackReasonEmbeddingCheckFailed, nil)
	}

	// Get embedding stats for metadata.
	stats, err := s.feedbackStore.GetEmbeddingStats(ctx, req.TenantID)
	if err != nil {
		logext.Warnf(ctx, "[%s] get stats failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		stats = ptrext.Of(feedback.EmbeddingStats{})
	}
	coverage := coverageFromStats(stats)

	// If no embeddings, use keyword search.
	if !hasEmbeddings || stats.EmbeddingModel == "" {
		logext.Infof(ctx, "[%s] no embeddings available,tenant_id:%s", where, req.TenantID)
		reason := fallbackReasonNoEmbeddings
		if err != nil {
			reason = fallbackReasonEmbeddingStatsFailed
		}
		return s.keywordOnlySearchWithMetricsReason(ctx, req, start, reason, coverage)
	}

	// Generate query embedding.
	embedding, model, err := s.getOrGenerateEmbedding(ctx, req.TenantID, req.Query)
	if err != nil {
		logext.Warnf(ctx, "[%s] embedding generation failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		// Fall back to keyword search.
		return s.keywordOnlySearchWithMetricsReason(ctx, req, start, fallbackReasonEmbeddingGenerationFail, coverage)
	}

	// Check model compatibility.
	if model != stats.EmbeddingModel {
		logext.Warnf(ctx, "[%s] model mismatch,query_model:%s,stored_model:%s,tenant_id:%s",
			where, model, stats.EmbeddingModel, req.TenantID)
		// Fall back to keyword search if models don't match.
		return s.keywordOnlySearchWithMetricsReason(ctx, req, start, fallbackReasonEmbeddingModelMismatch, coverage)
	}

	// Perform hybrid search.
	return s.hybridSearchWithMetrics(ctx, req, embedding, model, int(stats.EmbeddedCount), start, coverage)
}

// GetEmbedding generates an embedding for text.
func (s *service) GetEmbedding(ctx context.Context, tenantID, text string) ([]float32, string, error) {
	return s.getOrGenerateEmbedding(ctx, tenantID, text)
}

// normalizeRequest applies defaults to request parameters.
func normalizeRequest(req *SearchRequest) *SearchRequest {
	normalized := ptrext.Of(SearchRequest{
		TenantID:       req.TenantID,
		Query:          strings.TrimSpace(req.Query),
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
	return s.keywordOnlySearchWithReason(ctx, req, fallbackReasonSemanticUnavailable, nil)
}

func (s *service) keywordOnlySearchWithReason(
	ctx context.Context,
	req *SearchRequest,
	reason string,
	coverage *SearchCoverage,
) (*SearchResponse, error) {
	const where = "semanticsearch.Service.keywordOnlySearch"

	results, err := s.feedbackStore.LexicalSearch(ctx, ptrext.Of(feedback.LexicalSearchParams{
		TenantID: req.TenantID,
		Query:    req.Query,
		Limit:    req.Limit,
		Filter:   req.Filter,
	}))
	if err != nil {
		logext.Errorf(ctx, "[%s] keyword search failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		return nil, fmt.Errorf("keyword search: %w", err)
	}

	hits := s.mergeResults(nil, results, req.SemanticWeight, req.KeywordWeight, req.Limit)
	for _, hit := range hits {
		hit.RankingSignals = appendUniqueString(hit.RankingSignals, "keyword_fallback")
	}
	totalWithEmbeddings := 0
	if coverage != nil {
		totalWithEmbeddings = coverage.TotalWithEmbeddings
	}

	return ptrext.Of(SearchResponse{
		Hits:                hits,
		EmbeddingModel:      "",
		TotalWithEmbeddings: totalWithEmbeddings,
		UsedKeywordFallback: true,
		FallbackReason:      reason,
		RankingVersion:      RankingVersion,
		Coverage:            coverage,
	}), nil
}

// keywordOnlySearchWithMetrics performs keyword-only search and records metrics.
func (s *service) keywordOnlySearchWithMetrics(ctx context.Context, req *SearchRequest, start time.Time) (*SearchResponse, error) {
	return s.keywordOnlySearchWithMetricsReason(ctx, req, start, fallbackReasonSemanticUnavailable, nil)
}

func (s *service) keywordOnlySearchWithMetricsReason(
	ctx context.Context,
	req *SearchRequest,
	start time.Time,
	reason string,
	coverage *SearchCoverage,
) (*SearchResponse, error) {
	const where = "semanticsearch.Service.keywordOnlySearchWithMetrics"

	resp, err := s.keywordOnlySearchWithReason(ctx, req, reason, coverage)
	if err != nil {
		return nil, err
	}

	elapsed := time.Since(start)
	logext.Infof(ctx, "[%s] semantic search completed,tenant_id:%s,result_count:%d,used_keyword_fallback:true,duration_ms:%d",
		where, req.TenantID, len(resp.Hits), elapsed.Milliseconds())

	// Record metrics.
	recordSearchMetrics(req.TenantID, "keyword_fallback", elapsed, resp)

	return resp, nil
}

// hybridSearch performs combined semantic and lexical search.
func (s *service) hybridSearch(
	ctx context.Context,
	req *SearchRequest,
	embedding []float32,
	model string,
	totalWithEmbeddings int,
	coverage *SearchCoverage,
) (*SearchResponse, error) {
	const where = "semanticsearch.Service.hybridSearch"
	candidates := candidateLimit(req.Limit)

	// Perform semantic search.
	semanticHits, err := s.feedbackStore.SemanticSearch(ctx, ptrext.Of(feedback.SemanticSearchParams{
		TenantID:       req.TenantID,
		Embedding:      embedding,
		EmbeddingModel: model,
		Limit:          candidates,
		MinSimilarity:  req.MinSimilarity,
		Filter:         req.Filter,
	}))
	if err != nil {
		logext.Warnf(ctx, "[%s] semantic search failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		// Fall back to keyword search.
		return s.keywordOnlySearchWithReason(ctx, req, fallbackReasonSemanticSearchFailed, coverage)
	}

	// Perform lexical search for hybrid results.
	lexicalResults, err := s.feedbackStore.LexicalSearch(ctx, ptrext.Of(feedback.LexicalSearchParams{
		TenantID: req.TenantID,
		Query:    req.Query,
		Limit:    candidates,
		Filter:   req.Filter,
	}))
	if err != nil {
		logext.Warnf(ctx, "[%s] lexical search failed,tenant_id:%s,err:%+v", where, req.TenantID, err.Error())
		// Continue with semantic results only.
		lexicalResults = nil
	}

	// Merge and score results.
	hits := s.mergeResults(semanticHits, lexicalResults, req.SemanticWeight, req.KeywordWeight, req.Limit)

	// If no semantic results, mark as fallback.
	usedFallback := len(semanticHits) == 0
	fallbackReason := ""
	if usedFallback {
		fallbackReason = fallbackReasonNoSemanticMatches
		for _, hit := range hits {
			hit.RankingSignals = appendUniqueString(hit.RankingSignals, "keyword_fallback")
		}
	}

	return ptrext.Of(SearchResponse{
		Hits:                hits,
		EmbeddingModel:      model,
		TotalWithEmbeddings: totalWithEmbeddings,
		UsedKeywordFallback: usedFallback,
		FallbackReason:      fallbackReason,
		RankingVersion:      RankingVersion,
		Coverage:            coverageOrDefault(coverage, totalWithEmbeddings, model),
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
	coverage *SearchCoverage,
) (*SearchResponse, error) {
	const where = "semanticsearch.Service.hybridSearchWithMetrics"

	resp, err := s.hybridSearch(ctx, req, embedding, model, totalWithEmbeddings, coverage)
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
	recordSearchMetrics(req.TenantID, searchType, elapsed, resp)

	return resp, nil
}

func recordSearchMetrics(tenantID, searchType string, elapsed time.Duration, resp *SearchResponse) {
	metrics.SearchQueriesTotal.WithLabelValues(tenantID, searchType).Inc()
	metrics.SearchQueryDuration.WithLabelValues(tenantID, searchType).Observe(elapsed.Seconds())
	if resp == nil {
		return
	}
	metrics.SearchResultsCount.WithLabelValues(tenantID).Observe(float64(len(resp.Hits)))
	recordSearchHealthMetrics(tenantID, resp)
}

func recordSearchHealthMetrics(tenantID string, resp *SearchResponse) {
	if resp.FallbackReason != "" {
		metrics.SearchFallbackReasonsTotal.WithLabelValues(tenantID, resp.FallbackReason).Inc()
	}
	coverage := resp.Coverage
	if coverage == nil || coverage.TotalLiveFeedback <= 0 {
		return
	}
	model := coverage.EmbeddingModel
	if model == "" {
		model = "none"
	}
	ratio := float64(coverage.TotalWithEmbeddings) / float64(coverage.TotalLiveFeedback)
	metrics.SearchEmbeddingCoverageRatio.WithLabelValues(tenantID, model).Set(clampRatio(ratio))
}

func clampRatio(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}

// mergeResults combines semantic and lexical results with reciprocal rank fusion.
func (s *service) mergeResults(
	semanticHits []feedback.SemanticSearchHit,
	lexicalResults []feedback.LexicalSearchHit,
	semanticWeight float64,
	keywordWeight float64,
	limit int,
) []*SearchHit {
	// Build a map of feedback ID -> combined hit.
	hitMap := make(map[int64]*SearchHit)
	seenSemantic := make(map[int64]struct{}, len(semanticHits))
	seenLexical := make(map[int64]struct{}, len(lexicalResults))

	// Add semantic results.
	for i, hit := range semanticHits {
		if hit.Feedback == nil {
			continue
		}
		if _, seen := seenSemantic[hit.Feedback.ID]; seen {
			continue
		}
		seenSemantic[hit.Feedback.ID] = struct{}{}
		rank := i + 1
		hitMap[hit.Feedback.ID] = ptrext.Of(SearchHit{
			Feedback:       hit.Feedback,
			Similarity:     hit.Similarity,
			KeywordScore:   0,
			MatchType:      "semantic",
			SemanticRank:   rank,
			FusedScore:     rrfScore(rank, semanticWeight),
			Evidence:       semanticEvidence(hit.Feedback),
			RankingSignals: []string{"semantic", "rrf"},
		})
	}

	// Add lexical results, merging with existing semantic hits.
	for i, lexicalHit := range lexicalResults {
		if lexicalHit.Feedback == nil {
			continue
		}
		if _, seen := seenLexical[lexicalHit.Feedback.ID]; seen {
			continue
		}
		seenLexical[lexicalHit.Feedback.ID] = struct{}{}
		rank := lexicalHit.Rank
		if rank <= 0 {
			rank = i + 1
		}
		keywordScore := lexicalHit.Score
		if keywordScore <= 0 {
			keywordScore = lexicalScoreForRank(rank)
		}

		if existing, ok := hitMap[lexicalHit.Feedback.ID]; ok {
			// This item was found by both searches.
			existing.KeywordScore = keywordScore
			existing.MatchType = "hybrid"
			existing.LexicalRank = rank
			existing.FusedScore += rrfScore(rank, keywordWeight)
			existing.Evidence = appendSearchEvidence(existing.Evidence, lexicalEvidence(lexicalHit)...)
			existing.RankingSignals = appendUniqueString(existing.RankingSignals, "lexical")
			existing.RankingSignals = appendUniqueString(existing.RankingSignals, "hybrid")
		} else {
			hitMap[lexicalHit.Feedback.ID] = ptrext.Of(SearchHit{
				Feedback:       lexicalHit.Feedback,
				Similarity:     0,
				KeywordScore:   keywordScore,
				MatchType:      "keyword",
				LexicalRank:    rank,
				FusedScore:     rrfScore(rank, keywordWeight),
				Evidence:       lexicalEvidence(lexicalHit),
				RankingSignals: []string{"lexical", "rrf"},
			})
		}
	}

	// Convert to slice and compute combined scores.
	hits := make([]*SearchHit, 0, len(hitMap))
	for _, hit := range hitMap {
		hits = append(hits, hit)
	}

	// Sort by fused score, then by source scores for deterministic ties.
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].FusedScore != hits[j].FusedScore {
			return hits[i].FusedScore > hits[j].FusedScore
		}
		if hits[i].Similarity != hits[j].Similarity {
			return hits[i].Similarity > hits[j].Similarity
		}
		if hits[i].KeywordScore != hits[j].KeywordScore {
			return hits[i].KeywordScore > hits[j].KeywordScore
		}
		return hits[i].Feedback.ID > hits[j].Feedback.ID
	})

	// Trim to limit.
	if len(hits) > limit {
		hits = hits[:limit]
	}

	return hits
}

func rrfScore(rank int, weight float64) float64 {
	if rank <= 0 || weight <= 0 {
		return 0
	}
	return weight / float64(DefaultRRFK+rank)
}

func lexicalScoreForRank(rank int) float64 {
	score := 1.0 - float64(rank-1)*0.05
	if score < 0.1 {
		return 0.1
	}
	return score
}

func semanticEvidence(fb *feedback.SearchFeedback) []SearchEvidence {
	if fb == nil {
		return nil
	}
	snippet := fb.EnrichedDisplayTitle
	field := "title"
	if snippet == "" {
		snippet = fb.EnrichedTitle
	}
	if snippet == "" {
		snippet = fb.Content
		field = "content"
	}
	if snippet == "" {
		return nil
	}
	return []SearchEvidence{{
		Field:   field,
		Snippet: truncateEvidence(snippet, 160),
		Reason:  "vector_similarity",
	}}
}

func lexicalEvidence(hit feedback.LexicalSearchHit) []SearchEvidence {
	evidence := make([]SearchEvidence, 0, len(hit.Snippets))
	for _, snippet := range hit.Snippets {
		if snippet.Snippet == "" {
			continue
		}
		evidence = append(evidence, SearchEvidence{
			Field:   snippet.Field,
			Snippet: truncateEvidence(snippet.Snippet, 160),
			Reason:  "lexical_match",
		})
	}
	if len(evidence) == 0 {
		for _, field := range hit.Fields {
			if field == "" {
				continue
			}
			evidence = append(evidence, SearchEvidence{
				Field:  field,
				Reason: "lexical_match",
			})
			break
		}
	}
	return evidence
}

func appendSearchEvidence(existing []SearchEvidence, next ...SearchEvidence) []SearchEvidence {
	if len(next) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing)+len(next))
	for _, item := range existing {
		seen[item.Field+"|"+item.Reason+"|"+item.Snippet] = struct{}{}
	}
	for _, item := range next {
		key := item.Field + "|" + item.Reason + "|" + item.Snippet
		if _, ok := seen[key]; ok {
			continue
		}
		existing = append(existing, item)
		seen[key] = struct{}{}
		if len(existing) == 3 {
			return existing
		}
	}
	return existing
}

func appendUniqueString(values []string, next string) []string {
	if next == "" {
		return values
	}
	for _, value := range values {
		if value == next {
			return values
		}
	}
	return append(values, next)
}

func truncateEvidence(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func candidateLimit(limit int) int {
	if limit <= 0 {
		limit = DefaultLimit
	}
	candidates := limit * CandidateMultiplier
	if candidates > MaxLimit {
		return MaxLimit
	}
	return candidates
}

func coverageFromStats(stats *feedback.EmbeddingStats) *SearchCoverage {
	if stats == nil {
		return nil
	}
	return ptrext.Of(SearchCoverage{
		TotalLiveFeedback:   int(stats.TotalFeedback),
		TotalWithEmbeddings: int(stats.EmbeddedCount),
		EmbeddingModel:      stats.EmbeddingModel,
	})
}

func coverageOrDefault(coverage *SearchCoverage, totalWithEmbeddings int, model string) *SearchCoverage {
	if coverage != nil {
		return coverage
	}
	return ptrext.Of(SearchCoverage{
		TotalWithEmbeddings: totalWithEmbeddings,
		EmbeddingModel:      model,
	})
}

// getOrGenerateEmbedding gets a cached embedding or generates a new one.
func (s *service) getOrGenerateEmbedding(ctx context.Context, tenantID, text string) ([]float32, string, error) {
	const where = "semanticsearch.Service.getOrGenerateEmbedding"

	// Compute query hash for caching.
	queryHash := hashQuery(tenantID, text)

	// Try cache first.
	if s.cache != nil {
		emb, model, found, err := s.cache.Get(ctx, tenantID, queryHash)
		if err != nil {
			logext.Warnf(ctx, "[%s] cache get error,tenant_id:%s,err:%+v", where, tenantID, err.Error())
			// Continue without cache.
		} else if found && len(emb) == EmbeddingDims && model != "" {
			metrics.EmbeddingCacheHits.WithLabelValues(tenantID, "hit").Inc()
			return emb, model, nil
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
		if err := s.cache.Set(ctx, tenantID, queryHash, embedding, model, QueryCacheTTL); err != nil {
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
