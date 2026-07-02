// SPDX-License-Identifier: Apache-2.0

// Package semanticsearch provides semantic search capabilities for feedback,
// combining vector similarity search with keyword fallback (#30).
package semanticsearch

import (
	"context"
	"time"

	"github.com/Phixsura/attune/internal/repo/feedback"
)

// Search defaults and limits.
const (
	DefaultLimit          = 20
	DefaultMinSimilarity  = 0.5
	DefaultSemanticWeight = 0.7
	DefaultKeywordWeight  = 0.3
	DefaultRRFK           = 60
	CandidateMultiplier   = 4
	MaxLimit              = 100
	MaxQueryRunes         = 512
	EmbeddingDims         = 256
	QueryCacheTTL         = time.Hour
	RankingVersion        = "rrf.pgfts.v1.k60"
)

// Rate limit settings.
const (
	SearchRateLimit  = 60          // requests per tenant per window
	SearchRateWindow = time.Minute // sliding window size
	RateLimitKey     = "search:%s" // format with tenantID
)

// SearchRequest represents a validated search request.
type SearchRequest struct {
	TenantID       string
	Query          string
	Limit          int
	MinSimilarity  float64
	Filter         *feedback.FeedbackFilter
	SemanticWeight float64
	KeywordWeight  float64
}

// SearchResponse represents search results.
type SearchResponse struct {
	Hits                []*SearchHit
	EmbeddingModel      string
	TotalWithEmbeddings int
	UsedKeywordFallback bool
	FallbackReason      string
	RankingVersion      string
	Coverage            *SearchCoverage
}

// SearchHit is a single search result with ranking metadata.
type SearchHit struct {
	Feedback       *feedback.SearchFeedback
	Similarity     float64 // semantic similarity score (0.0-1.0)
	KeywordScore   float64 // lexical relevance score (0.0-1.0)
	MatchType      string  // "semantic", "keyword", or "hybrid"
	SemanticRank   int
	LexicalRank    int
	FusedScore     float64
	Evidence       []SearchEvidence
	RankingSignals []string
}

// SearchEvidence explains why a result matched.
type SearchEvidence struct {
	Field   string
	Snippet string
	Reason  string
}

// SearchCoverage describes corpus coverage for the search response.
type SearchCoverage struct {
	TotalLiveFeedback   int
	TotalWithEmbeddings int
	EmbeddingModel      string
}

// Service provides semantic search capabilities.
type Service interface {
	// Search performs semantic search with keyword fallback.
	Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error)

	// GetEmbedding generates an embedding for text (for external use).
	GetEmbedding(ctx context.Context, tenantID, text string) ([]float32, string, error)
}

// EmbeddingCache provides caching for query embeddings.
type EmbeddingCache interface {
	// Get retrieves a cached embedding by query hash, returning the embedding, model, and found flag.
	Get(ctx context.Context, tenantID, queryHash string) (embedding []float32, model string, found bool, err error)
	// Set stores an embedding with the model it was generated from, with TTL.
	Set(ctx context.Context, tenantID, queryHash string, embedding []float32, model string, ttl time.Duration) error
}
