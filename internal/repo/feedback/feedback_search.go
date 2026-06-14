// SPDX-License-Identifier: Apache-2.0

// feedback_search.go — semantic and keyword search for user_feedback (#30).
// Uses pgvector for vector similarity and PostgreSQL ILIKE for keyword fallback.
package feedback

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Search defaults and limits.
const (
	DefaultSearchLimit   = 20
	MaxSearchLimit       = 100
	DefaultMinSimilarity = 0.5
	DefaultEfSearch      = 200
	EmbeddingDims        = 256
)

// SemanticSearchParams configures a vector similarity search.
type SemanticSearchParams struct {
	TenantID       string
	Embedding      []float32       // 256-dim query embedding
	EmbeddingModel string          // must match feedback embedding model
	Limit          int             // max results (default 20, max 100)
	MinSimilarity  float64         // min cosine similarity (default 0.5)
	Filter         *FeedbackFilter // additional filters
	EfSearch       int             // HNSW ef_search parameter (default 200)
}

// SemanticSearchHit is one result from a semantic search.
type SemanticSearchHit struct {
	Feedback   *SearchFeedback
	Similarity float64
}

// SearchFeedback is the feedback data returned from search operations.
// A subset of the full feedback row optimized for search result display.
type SearchFeedback struct {
	ID                   int64
	Content              string
	Source               string
	EnrichedTitle        string
	EnrichedDisplayTitle string
	EnrichedRationale    string
	IsUrgent             bool
	EnrichmentStatus     string
	CreatedAt            time.Time
	WorkflowStateID      *string
	ClusterID            *string
	ClusterLabel         *string
}

// SemanticSearch performs vector similarity search using pgvector's HNSW index.
// Requires embeddings to be present and model to match the query embedding model.
func (r *FeedbackRepo) SemanticSearch(
	ctx context.Context,
	params *SemanticSearchParams,
) ([]SemanticSearchHit, error) {
	const where = "repo.FeedbackRepo.SemanticSearch"

	if params == nil {
		return nil, fmt.Errorf("%s: params required", where)
	}
	if len(params.Embedding) != EmbeddingDims {
		return nil, fmt.Errorf("%s: embedding must be %d dimensions, got %d",
			where, EmbeddingDims, len(params.Embedding))
	}
	if params.EmbeddingModel == "" {
		return nil, fmt.Errorf("%s: embedding_model required", where)
	}

	// Normalize params.
	limit := params.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}
	minSim := params.MinSimilarity
	if minSim <= 0 {
		minSim = DefaultMinSimilarity
	}
	efSearch := params.EfSearch
	if efSearch <= 0 {
		efSearch = DefaultEfSearch
	}

	// Build query with filters.
	qb := newQueryBuilder(params.TenantID)
	qb.and("embedding IS NOT NULL")
	qb.and("embedding_model = " + qb.addArg(params.EmbeddingModel))

	if err := r.applyBatchFilters(qb, params.Filter); err != nil {
		return nil, fmt.Errorf("%s: %w", where, err)
	}

	// Convert embedding to pgvector string format.
	embStr := vecToString(params.Embedding)
	embArg := qb.addArg(embStr)

	// Use transaction to set ef_search.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: begin tx: %w", where, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", efSearch))
	if err != nil {
		logext.Warnf(ctx, "[%s] set ef_search failed,err:%+v", where, err.Error())
		// Continue without ef_search optimization.
	}

	// Build the search query.
	// Cosine similarity = 1 - cosine_distance, so we filter where 1 - dist > minSim.
	query := fmt.Sprintf(`
		SELECT id, content, source,
			COALESCE(enriched_title, ''),
			COALESCE(enriched_display_title, ''),
			COALESCE(enriched_rationale, ''),
			is_urgent, enrichment_status, created_at,
			workflow_state_id, cluster_id, cluster_label,
			1 - (embedding <=> %s::vector) AS similarity
		FROM user_feedback
		%s
		AND 1 - (embedding <=> %s::vector) > %s
		ORDER BY embedding <=> %s::vector
		LIMIT %s`,
		embArg, qb.where, embArg, qb.addArg(minSim), embArg, qb.addArg(limit))

	rows, err := tx.Query(ctx, query, qb.args...)
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v", where, params.TenantID, err.Error())
		return nil, fmt.Errorf("%s: %w", where, err)
	}
	defer rows.Close()

	var hits []SemanticSearchHit
	for rows.Next() {
		var (
			fb  SearchFeedback
			sim float64
		)
		err := rows.Scan(
			&fb.ID, &fb.Content, &fb.Source,
			&fb.EnrichedTitle, &fb.EnrichedDisplayTitle, &fb.EnrichedRationale, // ptrext:allow scan-target
			&fb.IsUrgent, &fb.EnrichmentStatus, &fb.CreatedAt, // ptrext:allow scan-target
			&fb.WorkflowStateID, &fb.ClusterID, &fb.ClusterLabel, // ptrext:allow scan-target
			&sim,
		)
		if err != nil {
			return nil, fmt.Errorf("%s: scan: %w", where, err)
		}
		hits = append(hits, SemanticSearchHit{
			Feedback:   ptrext.Of(fb),
			Similarity: sim,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: rows: %w", where, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("%s: commit: %w", where, err)
	}

	return hits, nil
}

// KeywordSearchParams configures a text-based search.
type KeywordSearchParams struct {
	TenantID string
	Query    string          // search text (ILIKE on content + titles)
	Limit    int             // max results (default 20, max 100)
	Filter   *FeedbackFilter // additional filters
}

// KeywordSearch performs text search as fallback when embeddings unavailable.
// Uses ILIKE on content, enriched_title, and enriched_display_title.
func (r *FeedbackRepo) KeywordSearch(
	ctx context.Context,
	params *KeywordSearchParams,
) ([]*SearchFeedback, error) {
	const where = "repo.FeedbackRepo.KeywordSearch"

	if params == nil || params.Query == "" {
		return nil, fmt.Errorf("%s: query required", where)
	}

	limit := params.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}

	qb := newQueryBuilder(params.TenantID)

	if err := r.applyBatchFilters(qb, params.Filter); err != nil {
		return nil, fmt.Errorf("%s: %w", where, err)
	}

	// Add text search condition.
	p := qb.addArg("%" + params.Query + "%")
	qb.and("(content ILIKE " + p +
		" OR enriched_title ILIKE " + p +
		" OR enriched_display_title ILIKE " + p + ")")

	query := `
		SELECT id, content, source,
			COALESCE(enriched_title, ''),
			COALESCE(enriched_display_title, ''),
			COALESCE(enriched_rationale, ''),
			is_urgent, enrichment_status, created_at,
			workflow_state_id, cluster_id, cluster_label
		FROM user_feedback
		` + qb.where + `
		ORDER BY
			CASE WHEN enriched_title ILIKE ` + p + ` THEN 0 ELSE 1 END,
			created_at DESC
		LIMIT ` + qb.addArg(limit)

	rows, err := r.pool.Query(ctx, query, qb.args...)
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v", where, params.TenantID, err.Error())
		return nil, fmt.Errorf("%s: %w", where, err)
	}
	defer rows.Close()

	var results []*SearchFeedback
	for rows.Next() {
		var fb SearchFeedback
		err := rows.Scan(
			&fb.ID, &fb.Content, &fb.Source,
			&fb.EnrichedTitle, &fb.EnrichedDisplayTitle, &fb.EnrichedRationale, // ptrext:allow scan-target
			&fb.IsUrgent, &fb.EnrichmentStatus, &fb.CreatedAt, // ptrext:allow scan-target
			&fb.WorkflowStateID, &fb.ClusterID, &fb.ClusterLabel, // ptrext:allow scan-target
		)
		if err != nil {
			return nil, fmt.Errorf("%s: scan: %w", where, err)
		}
		results = append(results, ptrext.Of(fb))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: rows: %w", where, err)
	}

	return results, nil
}

// GetFeedbackEmbedding retrieves the embedding for a single feedback item.
// Returns nil if the feedback has no embedding.
func (r *FeedbackRepo) GetFeedbackEmbedding(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
) ([]float32, string, error) {
	const where = "repo.FeedbackRepo.GetFeedbackEmbedding"

	var embStr *string
	var model string
	err := r.pool.QueryRow(
		ctx,
		`SELECT embedding::text, embedding_model
		 FROM user_feedback
		 WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`,
		feedbackID, tenantID,
	).Scan(&embStr, &model) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrFeedbackNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", where, err)
	}

	embStrVal := ptrext.Indirect(embStr)
	if embStrVal == "" {
		return nil, "", nil
	}

	emb, err := parseVecString(embStrVal)
	if err != nil {
		return nil, "", fmt.Errorf("%s: parse embedding: %w", where, err)
	}

	return emb, model, nil
}

// FindSimilarFeedback finds feedback items similar to a given feedback item.
// Uses the item's embedding if available.
func (r *FeedbackRepo) FindSimilarFeedback(
	ctx context.Context,
	tenantID string,
	feedbackID int64,
	limit int,
	minSimilarity float64,
) ([]SemanticSearchHit, error) {
	const where = "repo.FeedbackRepo.FindSimilarFeedback"

	// Get the source feedback's embedding.
	emb, model, err := r.GetFeedbackEmbedding(ctx, tenantID, feedbackID)
	if err != nil {
		return nil, err
	}
	if emb == nil {
		return nil, fmt.Errorf("%s: feedback %d has no embedding", where, feedbackID)
	}

	// Search excluding the source feedback.
	params := ptrext.Of(SemanticSearchParams{
		TenantID:       tenantID,
		Embedding:      emb,
		EmbeddingModel: model,
		Limit:          limit + 1, // +1 to account for potential self-match.
		MinSimilarity:  minSimilarity,
	})

	hits, err := r.SemanticSearch(ctx, params)
	if err != nil {
		return nil, err
	}

	// Filter out the source feedback.
	var filtered []SemanticSearchHit
	for _, h := range hits {
		if h.Feedback.ID != feedbackID {
			filtered = append(filtered, h)
		}
	}

	// Trim to requested limit.
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered, nil
}

// HasEmbedding checks if a tenant has any embedded feedback.
// Useful to determine if semantic search is available.
func (r *FeedbackRepo) HasEmbedding(ctx context.Context, tenantID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(
		ctx,
		`SELECT EXISTS(
			SELECT 1 FROM user_feedback
			WHERE tenant_id = $1 AND embedding IS NOT NULL
			LIMIT 1
		)`, tenantID,
	).Scan(&exists) // ptrext:allow scan-target
	if err != nil {
		return false, fmt.Errorf("check embedding existence: %w", err)
	}
	return exists, nil
}

// EmbeddingStats returns statistics about embeddings for a tenant.
type EmbeddingStats struct {
	TotalFeedback    int64
	EmbeddedCount    int64
	EmbeddingModel   string // most common model
	EmbeddingPercent float64
}

// GetEmbeddingStats returns embedding statistics for a tenant.
func (r *FeedbackRepo) GetEmbeddingStats(ctx context.Context, tenantID string) (*EmbeddingStats, error) {
	const where = "repo.FeedbackRepo.GetEmbeddingStats"

	var stats EmbeddingStats
	var model *string

	err := r.pool.QueryRow(
		ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE embedding IS NOT NULL),
			(SELECT embedding_model FROM user_feedback
			 WHERE tenant_id = $1 AND embedding IS NOT NULL
			 GROUP BY embedding_model ORDER BY COUNT(*) DESC LIMIT 1)
		FROM user_feedback
		WHERE tenant_id = $1 AND deleted_at IS NULL`,
		tenantID,
	).Scan(&stats.TotalFeedback, &stats.EmbeddedCount, &model) // ptrext:allow scan-target
	if err != nil {
		return nil, fmt.Errorf("%s: %w", where, err)
	}

	if model != nil {
		stats.EmbeddingModel = ptrext.Indirect(model)
	}
	if stats.TotalFeedback > 0 {
		stats.EmbeddingPercent = float64(stats.EmbeddedCount) / float64(stats.TotalFeedback) * 100
	}

	return ptrext.Of(stats), nil
}

// vecToString converts a []float32 to pgvector's text format: "[1.0,2.0,3.0]".
// Local copy to avoid import cycle with embedding package.
func vecToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// parseVecString parses pgvector's text format "[1.0,2.0,3.0]" to []float32.
func parseVecString(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return nil, nil
	}
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("invalid vector format: %s", s)
	}
	s = s[1 : len(s)-1] // Remove brackets.
	if s == "" {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	result := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("invalid float at index %d: %w", i, err)
		}
		result[i] = float32(f)
	}
	return result, nil
}
