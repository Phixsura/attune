// SPDX-License-Identifier: Apache-2.0

// feedback_search.go — semantic and lexical search for user_feedback (#30).
// Uses pgvector for vector similarity and PostgreSQL full-text search for lexical fallback.
package feedback

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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

// SearchSnippet identifies a short matched excerpt in a search result.
type SearchSnippet struct {
	Field   string
	Snippet string
}

// LexicalSearchParams configures PostgreSQL full-text lexical search.
type LexicalSearchParams struct {
	TenantID string
	Query    string
	Limit    int
	Filter   *FeedbackFilter
}

// LexicalSearchHit is one ranked lexical search result.
type LexicalSearchHit struct {
	Feedback *SearchFeedback
	Rank     int
	Score    float64
	Fields   []string
	Snippets []SearchSnippet
}

// SearchFeedback is the feedback data returned from search operations.
// A subset of the full feedback row optimized for search result display.
type SearchFeedback struct {
	ID                               int64
	Content                          string
	Source                           string
	Type                             string
	UserID                           string
	Language                         string
	PageURL                          string
	EnrichedTitle                    string
	EnrichedDisplayTitle             string
	EnrichedDisplayLocale            string
	EnrichedAttrs                    []byte
	EnrichedRationale                string
	IsUrgent                         bool
	ClassificationConfidence         *float64
	EnrichmentStatus                 string
	CreatedAt                        time.Time
	WorkflowStateID                  *string
	ClusterID                        *string
	ClusterLabel                     *string
	EnrichmentAttempts               int
	EnrichmentNextRetryAt            *time.Time
	TerminalFailureReasonClass       string
	TerminalFailureModel             string
	TerminalFailureChannelID         string
	TerminalFailureChannelName       string
	TerminalFailureConfigFingerprint string
	TerminalFailurePromptVersion     string
}

// normalizeSearchParams returns normalized limit, minSim, efSearch.
func normalizeSearchParams(params *SemanticSearchParams) (limit int, minSim float64, efSearch int) {
	limit = params.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}
	minSim = params.MinSimilarity
	if minSim <= 0 {
		minSim = DefaultMinSimilarity
	}
	efSearch = params.EfSearch
	if efSearch <= 0 {
		efSearch = DefaultEfSearch
	}
	return limit, minSim, efSearch
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

	limit, minSim, efSearch := normalizeSearchParams(params)

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
			SELECT id, content, source, type, user_id, COALESCE(language, ''), page_url,
				COALESCE(enriched_title, ''),
				COALESCE(enriched_display_title, ''),
				COALESCE(enriched_display_locale, ''),
				COALESCE(enriched_attrs, '{}'::jsonb),
				COALESCE(enriched_rationale, ''),
				is_urgent, classification_confidence, enrichment_status, created_at,
				workflow_state_id, cluster_id, cluster_label,
				enrichment_attempts, enrichment_next_retry_at,
				COALESCE(enrichment_failure_reason_class, ''),
				COALESCE(enrichment_failure_model, ''),
				COALESCE(enrichment_failure_channel_id, ''),
				COALESCE(enrichment_failure_channel_name, ''),
				COALESCE(enrichment_failure_config_fingerprint, ''),
				COALESCE(enrichment_failure_prompt_version, ''),
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
		fb, sim, err := scanSemanticSearchRow(rows)
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
// It is retained as a compatibility wrapper over LexicalSearch.
func (r *FeedbackRepo) KeywordSearch(
	ctx context.Context,
	params *KeywordSearchParams,
) ([]*SearchFeedback, error) {
	const where = "repo.FeedbackRepo.KeywordSearch"

	if params == nil || params.Query == "" {
		return nil, fmt.Errorf("%s: query required", where)
	}

	hits, err := r.LexicalSearch(ctx, ptrext.Of(LexicalSearchParams{
		TenantID: params.TenantID,
		Query:    params.Query,
		Limit:    params.Limit,
		Filter:   params.Filter,
	}))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", where, err)
	}

	results := make([]*SearchFeedback, 0, len(hits))
	for _, hit := range hits {
		if hit.Feedback == nil {
			continue
		}
		results = append(results, hit.Feedback)
	}
	return results, nil
}

// LexicalSearch performs full-text search with an ILIKE partial-match fallback.
func (r *FeedbackRepo) LexicalSearch(
	ctx context.Context,
	params *LexicalSearchParams,
) ([]LexicalSearchHit, error) {
	const where = "repo.FeedbackRepo.LexicalSearch"

	if params == nil || strings.TrimSpace(params.Query) == "" {
		return nil, fmt.Errorf("%s: query required", where)
	}

	limit := normalizeLexicalLimit(params.Limit)
	qb := newQueryBuilder(params.TenantID)

	if err := r.applyBatchFilters(qb, params.Filter); err != nil {
		return nil, fmt.Errorf("%s: %w", where, err)
	}

	queryText := strings.TrimSpace(params.Query)
	queryArg := qb.addArg(queryText)
	patternArg := qb.addArg(likeContainsPattern(queryText))
	documentSQL := feedbackSearchDocumentSQL()
	tsQuerySQL := "plainto_tsquery('simple'::regconfig, " + queryArg + ")"
	partialMatchSQL := feedbackPartialMatchSQL(patternArg)
	scoreSQL := fmt.Sprintf(
		`GREATEST(
			ts_rank_cd(%s, %s),
			CASE
				WHEN enriched_title ILIKE %s ESCAPE '\' OR enriched_display_title ILIKE %s ESCAPE '\' THEN 0.95
				WHEN content ILIKE %s ESCAPE '\' THEN 0.80
				WHEN enriched_rationale ILIKE %s ESCAPE '\' THEN 0.65
				ELSE 0.25
			END
		)`,
		documentSQL,
		tsQuerySQL,
		patternArg,
		patternArg,
		patternArg,
		patternArg,
	)
	qb.and("(" + documentSQL + " @@ " + tsQuerySQL + " OR " + partialMatchSQL + ")")

	query := `
			SELECT id, content, source, type, user_id, COALESCE(language, ''), page_url,
				COALESCE(enriched_title, ''),
				COALESCE(enriched_display_title, ''),
				COALESCE(enriched_display_locale, ''),
				COALESCE(enriched_attrs, '{}'::jsonb),
				COALESCE(enriched_rationale, ''),
				is_urgent, classification_confidence, enrichment_status, created_at,
				workflow_state_id, cluster_id, cluster_label,
				enrichment_attempts, enrichment_next_retry_at,
				COALESCE(enrichment_failure_reason_class, ''),
				COALESCE(enrichment_failure_model, ''),
				COALESCE(enrichment_failure_channel_id, ''),
				COALESCE(enrichment_failure_channel_name, ''),
				COALESCE(enrichment_failure_config_fingerprint, ''),
				COALESCE(enrichment_failure_prompt_version, ''),
				` + scoreSQL + ` AS lexical_score
			FROM user_feedback
			` + qb.where + `
			ORDER BY
				CASE WHEN id::text = ` + queryArg + ` THEN 0 ELSE 1 END,
				CASE WHEN enriched_title ILIKE ` + patternArg + ` ESCAPE '\' OR enriched_display_title ILIKE ` + patternArg + ` ESCAPE '\' THEN 0 ELSE 1 END,
				lexical_score DESC,
				created_at DESC,
				id DESC
		LIMIT ` + qb.addArg(limit)

	rows, err := r.pool.Query(ctx, query, qb.args...)
	if err != nil {
		logext.Errorf(ctx, "[%s] query failed,tenant_id:%s,err:%+v", where, params.TenantID, err.Error())
		return nil, fmt.Errorf("%s: %w", where, err)
	}
	defer rows.Close()

	var results []LexicalSearchHit
	for rows.Next() {
		fb, score, err := scanLexicalSearchRow(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: scan: %w", where, err)
		}
		rank := len(results) + 1
		results = append(results, LexicalSearchHit{
			Feedback: ptrext.Of(fb),
			Rank:     rank,
			Score:    normalizeLexicalScore(score),
			Fields:   lexicalFields(queryText, fb),
			Snippets: lexicalSnippets(queryText, fb),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: rows: %w", where, err)
	}

	return results, nil
}

func scanSemanticSearchRow(rows pgx.Rows) (SearchFeedback, float64, error) {
	var similarity float64
	fb, err := scanSearchFeedbackRow(rows, &similarity) // ptrext:allow scan-target
	return fb, similarity, err
}

func scanLexicalSearchRow(rows pgx.Rows) (SearchFeedback, float64, error) {
	var score float64
	fb, err := scanSearchFeedbackRow(rows, &score) // ptrext:allow scan-target
	return fb, score, err
}

func scanSearchFeedbackRow(rows pgx.Rows, extraDest ...any) (SearchFeedback, error) {
	var (
		fb         SearchFeedback
		confidence sql.NullFloat64
		wsID       sql.NullString // ptrext:allow scan-target
		clusterID  sql.NullString // ptrext:allow scan-target
		clusterLbl sql.NullString // ptrext:allow scan-target
		nextRetry  sql.NullTime   // ptrext:allow scan-target
	)
	dest := []any{
		&fb.ID, &fb.Content, &fb.Source, &fb.Type, &fb.UserID, &fb.Language, &fb.PageURL, // ptrext:allow scan-target
		&fb.EnrichedTitle, &fb.EnrichedDisplayTitle, &fb.EnrichedDisplayLocale, // ptrext:allow scan-target
		&fb.EnrichedAttrs, &fb.EnrichedRationale, &fb.IsUrgent, &confidence, // ptrext:allow scan-target
		&fb.EnrichmentStatus, &fb.CreatedAt, // ptrext:allow scan-target
		&wsID, &clusterID, &clusterLbl, // ptrext:allow scan-target
		&fb.EnrichmentAttempts, &nextRetry, // ptrext:allow scan-target
		&fb.TerminalFailureReasonClass,       // ptrext:allow scan-target
		&fb.TerminalFailureModel,             // ptrext:allow scan-target
		&fb.TerminalFailureChannelID,         // ptrext:allow scan-target
		&fb.TerminalFailureChannelName,       // ptrext:allow scan-target
		&fb.TerminalFailureConfigFingerprint, // ptrext:allow scan-target
		&fb.TerminalFailurePromptVersion,     // ptrext:allow scan-target
	}
	dest = append(dest, extraDest...)
	if err := rows.Scan(dest...); err != nil {
		return SearchFeedback{}, err
	}
	return hydrateSearchFeedbackNulls(fb, confidence, wsID, clusterID, clusterLbl, nextRetry), nil
}

func hydrateSearchFeedbackNulls(
	fb SearchFeedback,
	confidence sql.NullFloat64,
	wsID sql.NullString,
	clusterID sql.NullString,
	clusterLbl sql.NullString,
	nextRetry sql.NullTime,
) SearchFeedback {
	fb.ClassificationConfidence = nullFloatPtr(confidence)
	if wsID.Valid {
		fb.WorkflowStateID = ptrext.Of(wsID.String)
	}
	if clusterID.Valid {
		fb.ClusterID = ptrext.Of(clusterID.String)
	}
	if clusterLbl.Valid {
		fb.ClusterLabel = ptrext.Of(clusterLbl.String)
	}
	if nextRetry.Valid {
		fb.EnrichmentNextRetryAt = ptrext.Of(nextRetry.Time)
	}
	return fb
}

func normalizeLexicalLimit(limit int) int {
	if limit <= 0 {
		return DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		return MaxSearchLimit
	}
	return limit
}

func feedbackSearchDocumentSQL() string {
	return `to_tsvector(
		'simple'::regconfig,
		COALESCE(content, '') || ' ' ||
		COALESCE(enriched_title, '') || ' ' ||
		COALESCE(enriched_display_title, '') || ' ' ||
		COALESCE(enriched_rationale, '') || ' ' ||
		COALESCE(source, '') || ' ' ||
		COALESCE(type, '') || ' ' ||
		COALESCE(user_id, '') || ' ' ||
		COALESCE(page_url, '')
	)`
}

func feedbackPartialMatchSQL(patternArg string) string {
	like := " ILIKE " + patternArg + " ESCAPE '\\'"
	return "(content" + like +
		" OR enriched_title" + like +
		" OR enriched_display_title" + like +
		" OR enriched_rationale" + like +
		" OR source" + like +
		" OR type" + like +
		" OR user_id" + like +
		" OR page_url" + like + ")"
}

func likeContainsPattern(query string) string {
	return "%" + escapeLikePattern(query) + "%"
}

func escapeLikePattern(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '\\', '%', '_':
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func normalizeLexicalScore(score float64) float64 {
	if score <= 0 {
		return 0.1
	}
	if score > 1 {
		return 1
	}
	return score
}

func lexicalFields(query string, fb SearchFeedback) []string {
	fields := make([]string, 0, 3)
	for _, candidate := range lexicalFieldCandidates(fb) {
		if candidate.value == "" || !textMatchesQuery(candidate.value, query) {
			continue
		}
		fields = append(fields, candidate.field)
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func lexicalSnippets(query string, fb SearchFeedback) []SearchSnippet {
	snippets := make([]SearchSnippet, 0, 2)
	for _, candidate := range lexicalFieldCandidates(fb) {
		if candidate.value == "" || !textMatchesQuery(candidate.value, query) {
			continue
		}
		snippets = append(snippets, SearchSnippet{
			Field:   candidate.field,
			Snippet: snippetAroundQuery(candidate.value, query, 160),
		})
		if len(snippets) == 2 {
			break
		}
	}
	if len(snippets) == 0 {
		snippets = append(snippets, SearchSnippet{
			Field:   "content",
			Snippet: truncateRunes(fb.Content, 160),
		})
	}
	return snippets
}

type lexicalFieldCandidate struct {
	field string
	value string
}

func lexicalFieldCandidates(fb SearchFeedback) []lexicalFieldCandidate {
	title := fb.EnrichedDisplayTitle
	if title == "" {
		title = fb.EnrichedTitle
	}
	return []lexicalFieldCandidate{
		{field: "title", value: title},
		{field: "content", value: fb.Content},
		{field: "rationale", value: fb.EnrichedRationale},
		{field: "source", value: fb.Source},
		{field: "type", value: fb.Type},
		{field: "user_id", value: fb.UserID},
		{field: "page_url", value: fb.PageURL},
	}
}

func textMatchesQuery(text, query string) bool {
	lowerText := strings.ToLower(text)
	for _, term := range searchTerms(query) {
		if strings.Contains(lowerText, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func searchTerms(query string) []string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return nil
	}
	terms := []string{trimmed}
	if strings.Contains(trimmed, " ") {
		for _, term := range strings.Fields(trimmed) {
			if len([]rune(term)) < 2 {
				continue
			}
			terms = append(terms, term)
		}
	}
	return terms
}

func snippetAroundQuery(text, query string, maxRunes int) string {
	terms := searchTerms(query)
	if len(terms) == 0 {
		return truncateRunes(text, maxRunes)
	}
	lowerText := strings.ToLower(text)
	for _, term := range terms {
		idx := strings.Index(lowerText, strings.ToLower(term))
		if idx < 0 {
			continue
		}
		matchStart := utf8.RuneCountInString(text[:idx])
		matchRunes := len([]rune(term))
		runes := []rune(text)
		contextRunes := (maxRunes - matchRunes) / 2
		if contextRunes < 12 {
			contextRunes = 12
		}
		start := matchStart - contextRunes
		if start < 0 {
			start = 0
		}
		end := matchStart + matchRunes + contextRunes
		if end > len(runes) {
			end = len(runes)
		}
		snippet := string(runes[start:end])
		if start > 0 {
			snippet = "..." + snippet
		}
		if end < len(runes) {
			snippet += "..."
		}
		return snippet
	}
	return truncateRunes(text, maxRunes)
}

func truncateRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
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
// Uses the item's embedding if available. Snapshots of the same source
// thread (inbound adapters create one row per conversation/ticket update
// under an updated_at-suffixed idempotency key) collapse to their best
// hit, so a long-running conversation never inflates the recurrence
// signal with copies of itself.
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

	// Over-fetch: self-match + same-thread snapshots are dropped below.
	params := ptrext.Of(SemanticSearchParams{
		TenantID:       tenantID,
		Embedding:      emb,
		EmbeddingModel: model,
		Limit:          limit*3 + 1,
		MinSimilarity:  minSimilarity,
	})

	hits, err := r.SemanticSearch(ctx, params)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(hits)+1)
	ids = append(ids, feedbackID)
	for _, h := range hits {
		ids = append(ids, h.Feedback.ID)
	}
	threads, err := r.feedbackThreadKeys(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}

	// Filter: drop the anchor row, anything from the anchor's own
	// thread, and later (lower-similarity) snapshots of an already-seen
	// thread. Hits arrive similarity-descending, so the first snapshot
	// per thread is the best one.
	seenThreads := map[string]bool{}
	if k := threads[feedbackID]; k != "" {
		seenThreads[k] = true
	}
	var filtered []SemanticSearchHit
	for _, h := range hits {
		if h.Feedback.ID == feedbackID {
			continue
		}
		if k := threads[h.Feedback.ID]; k != "" {
			if seenThreads[k] {
				continue
			}
			seenThreads[k] = true
		}
		filtered = append(filtered, h)
	}

	// Trim to requested limit.
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered, nil
}

// feedbackThreadKeys resolves each feedback row to its source-thread
// identity (channel-prefixed conversation/ticket ID) when the row came
// from an inbound support channel. Rows without a thread identity map to
// "" and are never deduped against each other.
func (r *FeedbackRepo) feedbackThreadKeys(ctx context.Context, tenantID string, feedbackIDs []int64) (map[int64]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, source, COALESCE(
			source_meta->>'intercom_conversation_id',
			source_meta->>'zendesk_ticket_id',
			'')
		FROM user_feedback
		WHERE tenant_id = $1 AND id = ANY($2)`,
		tenantID, feedbackIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("feedback thread keys: %w", err)
	}
	defer rows.Close()

	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var source, threadID string
		if err := rows.Scan(&id, &source, &threadID); err != nil { // ptrext:allow pgx-scan
			return nil, fmt.Errorf("feedback thread keys scan: %w", err)
		}
		if threadID != "" {
			out[id] = source + ":" + threadID
		}
	}
	return out, rows.Err()
}

// LinkedRequestRef identifies a customer request already tracking a
// feedback row (via customer_request_feedback_links).
type LinkedRequestRef struct {
	ID     string
	CrNo   int64
	Title  string
	Status string
}

// RequestsLinkedToFeedback resolves which active customer requests
// reference each of the given feedback rows — the dedup signal behind
// "link to the existing request instead of creating a duplicate".
func (r *FeedbackRepo) RequestsLinkedToFeedback(ctx context.Context, tenantID string, feedbackIDs []int64) (map[int64][]LinkedRequestRef, error) {
	if len(feedbackIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT fl.feedback_id, cr.id, cr.display_number, cr.title, cr.status
		FROM customer_request_feedback_links fl
		JOIN customer_requests cr
		  ON cr.tenant_id = fl.tenant_id AND cr.id = fl.request_id
		WHERE fl.tenant_id = $1
		  AND fl.feedback_id = ANY($2)
		  AND cr.archived_at IS NULL
		ORDER BY cr.updated_at DESC`,
		tenantID, feedbackIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("requests linked to feedback: %w", err)
	}
	defer rows.Close()

	out := map[int64][]LinkedRequestRef{}
	for rows.Next() {
		var feedbackID int64
		var ref LinkedRequestRef
		if err := rows.Scan(&feedbackID, &ref.ID, &ref.CrNo, &ref.Title, &ref.Status); err != nil { // ptrext:allow pgx-scan
			return nil, fmt.Errorf("requests linked to feedback scan: %w", err)
		}
		out[feedbackID] = append(out[feedbackID], ref)
	}
	return out, rows.Err()
}

// HasEmbedding checks if a tenant has any embedded feedback.
// Useful to determine if semantic search is available.
func (r *FeedbackRepo) HasEmbedding(ctx context.Context, tenantID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(
		ctx,
		`SELECT EXISTS(
			SELECT 1 FROM user_feedback
			WHERE tenant_id = $1 AND embedding IS NOT NULL AND deleted_at IS NULL
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
