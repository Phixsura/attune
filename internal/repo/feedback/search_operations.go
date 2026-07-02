// SPDX-License-Identifier: Apache-2.0

package feedback

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	SearchQualityBucketHour = "hour"
	SearchQualityBucketDay  = "day"

	searchQualityDefaultLimit = 10
	searchQualityMaxLimit     = 50
)

var ErrSearchRunNotFound = errors.New("search run not found")

type SearchRunInsert struct {
	TenantID            string
	RunID               string
	QueryHash           string
	QueryPreview        string
	FilterHash          string
	RankingVersion      string
	EmbeddingModel      string
	ResultCount         int
	UsedKeywordFallback bool
	FallbackReason      string
	LatencyMS           int
	TotalLiveFeedback   int
	TotalWithEmbeddings int
	CoverageRatio       float64
	ActorUserID         string
}

type SearchResultEventInsert struct {
	TenantID    string
	RunID       string
	FeedbackID  int64
	Action      string
	Rank        int
	MatchType   string
	ActorUserID string
}

type SearchQualityQueryOpts struct {
	TenantID    string
	From        time.Time
	To          time.Time
	BucketWidth string
	Limit       int
}

type SearchQualityDashboard struct {
	Summary           SearchQualitySummary
	Series            []SearchQualitySeriesBucket
	Queries           []SearchQualityQueryAggregate
	ZeroResultQueries []SearchQualityQueryAggregate
	FallbackBreakdown []SearchFallbackAggregate
	IndexHealth       SearchIndexHealth
	RankingVersions   []SearchRankingVersion
}

type SearchQualitySummary struct {
	QueryCount         int64
	ZeroResultCount    int64
	FallbackCount      int64
	ClickCount         int64
	ClickedRunCount    int64
	AverageResultCount float64
	P95LatencyMS       int64
}

type SearchQualitySeriesBucket struct {
	Bucket          time.Time
	QueryCount      int64
	ZeroResultCount int64
	FallbackCount   int64
	ClickCount      int64
	ClickedRunCount int64
	P95LatencyMS    int64
}

type SearchQualityQueryAggregate struct {
	QueryHash          string
	QueryPreview       string
	QueryCount         int64
	ZeroResultCount    int64
	FallbackCount      int64
	ClickCount         int64
	ClickedRunCount    int64
	AverageResultCount float64
	P95LatencyMS       int64
	LastSeenAt         time.Time
}

type SearchFallbackAggregate struct {
	Reason string
	Count  int64
	Share  float64
}

type SearchIndexHealth struct {
	TotalLiveFeedback       int64
	TotalWithEmbeddings     int64
	EmbeddingModel          string
	OldestMissingFeedbackAt *time.Time
	MissingFeedbackCount    int64
}

type SearchRankingVersion struct {
	RankingVersion string
	Status         string
	TrafficPercent int
	Notes          string
	UpdatedAt      time.Time
}

func (r *FeedbackRepo) RecordSearchRun(ctx context.Context, row SearchRunInsert) error {
	const where = "repo.FeedbackRepo.RecordSearchRun"

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", where, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO feedback_search_runs
		 (tenant_id, run_id, query_hash, query_preview, filter_hash, ranking_version,
		  embedding_model, result_count, used_keyword_fallback, fallback_reason,
		  latency_ms, total_live_feedback, total_with_embeddings, coverage_ratio,
		  actor_user_id)
		VALUES
		 ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
		  $11, $12, $13, $14, $15)
		ON CONFLICT (tenant_id, run_id) DO NOTHING`,
		row.TenantID, row.RunID, row.QueryHash, row.QueryPreview, row.FilterHash,
		row.RankingVersion, row.EmbeddingModel, row.ResultCount,
		row.UsedKeywordFallback, row.FallbackReason, row.LatencyMS,
		row.TotalLiveFeedback, row.TotalWithEmbeddings, clampSearchRatio(row.CoverageRatio),
		row.ActorUserID,
	); err != nil {
		return fmt.Errorf("%s: insert run: %w", where, err)
	}

	if row.RankingVersion != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO feedback_search_ranking_versions
			 (tenant_id, ranking_version, status, traffic_percent, notes, activated_at)
			VALUES ($1, $2, 'active', 100, 'Current production ranker', NOW())
			ON CONFLICT (tenant_id, ranking_version) DO NOTHING`,
			row.TenantID, row.RankingVersion,
		); err != nil {
			return fmt.Errorf("%s: upsert ranking version: %w", where, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%s: commit: %w", where, err)
	}
	return nil
}

func (r *FeedbackRepo) RecordSearchResultEvent(ctx context.Context, row SearchResultEventInsert) error {
	const where = "repo.FeedbackRepo.RecordSearchResultEvent"

	tag, err := r.pool.Exec(ctx, `
		INSERT INTO feedback_search_result_events
		 (tenant_id, run_id, feedback_id, action, rank, match_type, actor_user_id)
		SELECT $1, $2::uuid, $3, $4, $5, $6, $7
		 WHERE EXISTS (
		   SELECT 1
		     FROM feedback_search_runs
		    WHERE tenant_id = $1
		      AND run_id = $2::uuid
		 )`,
		row.TenantID, row.RunID, row.FeedbackID, row.Action, row.Rank,
		row.MatchType, row.ActorUserID,
	)
	if err != nil {
		return fmt.Errorf("%s: insert event: %w", where, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrSearchRunNotFound
	}
	return nil
}

func (r *FeedbackRepo) SearchQualityDashboard(
	ctx context.Context,
	opts SearchQualityQueryOpts,
) (*SearchQualityDashboard, error) {
	opts = normalizeSearchQualityQueryOpts(opts)

	summary, err := r.SearchQualitySummary(ctx, opts)
	if err != nil {
		return nil, err
	}
	series, err := r.SearchQualitySeries(ctx, opts)
	if err != nil {
		return nil, err
	}
	queries, err := r.SearchQualityQueries(ctx, opts, false)
	if err != nil {
		return nil, err
	}
	zeroResultQueries, err := r.SearchQualityQueries(ctx, opts, true)
	if err != nil {
		return nil, err
	}
	fallbackBreakdown, err := r.SearchFallbackBreakdown(ctx, opts)
	if err != nil {
		return nil, err
	}
	indexHealth, err := r.SearchIndexHealth(ctx, opts.TenantID)
	if err != nil {
		return nil, err
	}
	rankingVersions, err := r.SearchRankingVersions(ctx, opts.TenantID)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(SearchQualityDashboard{
		Summary:           summary,
		Series:            series,
		Queries:           queries,
		ZeroResultQueries: zeroResultQueries,
		FallbackBreakdown: fallbackBreakdown,
		IndexHealth:       indexHealth,
		RankingVersions:   rankingVersions,
	}), nil
}

func normalizeSearchQualityQueryOpts(opts SearchQualityQueryOpts) SearchQualityQueryOpts {
	opts.From = opts.From.UTC()
	opts.To = opts.To.UTC()
	if opts.BucketWidth != SearchQualityBucketHour {
		opts.BucketWidth = SearchQualityBucketDay
	}
	if opts.Limit <= 0 {
		opts.Limit = searchQualityDefaultLimit
	}
	if opts.Limit > searchQualityMaxLimit {
		opts.Limit = searchQualityMaxLimit
	}
	return opts
}

func (r *FeedbackRepo) SearchQualitySummary(
	ctx context.Context,
	opts SearchQualityQueryOpts,
) (SearchQualitySummary, error) {
	opts = normalizeSearchQualityQueryOpts(opts)
	var out SearchQualitySummary
	err := r.pool.QueryRow(ctx, `
		WITH runs AS (
			SELECT *
			  FROM feedback_search_runs
			 WHERE tenant_id = $1
			   AND created_at >= $2
			   AND created_at < $3
		),
		clicks AS (
			SELECT tenant_id, run_id, COUNT(*) FILTER (WHERE action = 'open') AS open_events
			  FROM feedback_search_result_events
			 WHERE tenant_id = $1
			   AND created_at >= $2
			   AND created_at < $3
			 GROUP BY tenant_id, run_id
		)
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE runs.result_count = 0),
		       COUNT(*) FILTER (WHERE runs.used_keyword_fallback),
		       COALESCE(SUM(COALESCE(clicks.open_events, 0)), 0),
		       COUNT(*) FILTER (WHERE COALESCE(clicks.open_events, 0) > 0),
		       COALESCE(AVG(runs.result_count), 0)::float8,
		       COALESCE((percentile_disc(0.95) WITHIN GROUP (ORDER BY runs.latency_ms))::bigint, 0)
		  FROM runs
		  LEFT JOIN clicks
		    ON clicks.tenant_id = runs.tenant_id
		   AND clicks.run_id = runs.run_id`,
		opts.TenantID, opts.From, opts.To,
	).Scan(
		&out.QueryCount, &out.ZeroResultCount, &out.FallbackCount,
		&out.ClickCount, &out.ClickedRunCount, &out.AverageResultCount,
		&out.P95LatencyMS,
	)
	if err != nil {
		return SearchQualitySummary{}, fmt.Errorf("search quality summary: %w", err)
	}
	return out, nil
}

func (r *FeedbackRepo) SearchQualitySeries(
	ctx context.Context,
	opts SearchQualityQueryOpts,
) ([]SearchQualitySeriesBucket, error) {
	opts = normalizeSearchQualityQueryOpts(opts)
	rows, err := r.pool.Query(ctx, `
		WITH runs AS (
			SELECT *
			  FROM feedback_search_runs
			 WHERE tenant_id = $1
			   AND created_at >= $3
			   AND created_at < $4
		),
		clicks AS (
			SELECT tenant_id, run_id, COUNT(*) FILTER (WHERE action = 'open') AS open_events
			  FROM feedback_search_result_events
			 WHERE tenant_id = $1
			   AND created_at >= $3
			   AND created_at < $4
			 GROUP BY tenant_id, run_id
		)
		SELECT date_trunc($2, runs.created_at) AS bucket,
		       COUNT(*),
		       COUNT(*) FILTER (WHERE runs.result_count = 0),
		       COUNT(*) FILTER (WHERE runs.used_keyword_fallback),
		       COALESCE(SUM(COALESCE(clicks.open_events, 0)), 0),
		       COUNT(*) FILTER (WHERE COALESCE(clicks.open_events, 0) > 0),
		       COALESCE((percentile_disc(0.95) WITHIN GROUP (ORDER BY runs.latency_ms))::bigint, 0)
		  FROM runs
		  LEFT JOIN clicks
		    ON clicks.tenant_id = runs.tenant_id
		   AND clicks.run_id = runs.run_id
		 GROUP BY bucket
		 ORDER BY bucket ASC`,
		opts.TenantID, opts.BucketWidth, opts.From, opts.To,
	)
	if err != nil {
		return nil, fmt.Errorf("search quality series: %w", err)
	}
	defer rows.Close()
	var out []SearchQualitySeriesBucket
	for rows.Next() {
		var row SearchQualitySeriesBucket
		if err := rows.Scan(
			&row.Bucket, &row.QueryCount, &row.ZeroResultCount,
			&row.FallbackCount, &row.ClickCount, &row.ClickedRunCount,
			&row.P95LatencyMS,
		); err != nil {
			return nil, fmt.Errorf("scan search quality series: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *FeedbackRepo) SearchQualityQueries(
	ctx context.Context,
	opts SearchQualityQueryOpts,
	zeroOnly bool,
) ([]SearchQualityQueryAggregate, error) {
	opts = normalizeSearchQualityQueryOpts(opts)
	rows, err := r.pool.Query(ctx, `
		WITH runs AS (
			SELECT *
			  FROM feedback_search_runs
			 WHERE tenant_id = $1
			   AND created_at >= $2
			   AND created_at < $3
		),
		clicks AS (
			SELECT tenant_id, run_id, COUNT(*) FILTER (WHERE action = 'open') AS open_events
			  FROM feedback_search_result_events
			 WHERE tenant_id = $1
			   AND created_at >= $2
			   AND created_at < $3
			 GROUP BY tenant_id, run_id
		),
		query_rows AS (
			SELECT runs.query_hash,
			       (array_agg(runs.query_preview ORDER BY runs.created_at DESC, runs.id DESC))[1] AS query_preview,
			       COUNT(*) AS query_count,
			       COUNT(*) FILTER (WHERE runs.result_count = 0) AS zero_result_count,
			       COUNT(*) FILTER (WHERE runs.used_keyword_fallback) AS fallback_count,
			       COALESCE(SUM(COALESCE(clicks.open_events, 0)), 0) AS click_count,
			       COUNT(*) FILTER (WHERE COALESCE(clicks.open_events, 0) > 0) AS clicked_run_count,
			       COALESCE(AVG(runs.result_count), 0)::float8 AS average_result_count,
			       COALESCE((percentile_disc(0.95) WITHIN GROUP (ORDER BY runs.latency_ms))::bigint, 0) AS p95_latency_ms,
			       MAX(runs.created_at) AS last_seen_at
			  FROM runs
			  LEFT JOIN clicks
			    ON clicks.tenant_id = runs.tenant_id
			   AND clicks.run_id = runs.run_id
			 GROUP BY runs.query_hash
		)
		SELECT query_hash, query_preview, query_count, zero_result_count, fallback_count,
		       click_count, clicked_run_count, average_result_count, p95_latency_ms, last_seen_at
		  FROM query_rows
		 WHERE ($4 = FALSE OR zero_result_count > 0)
		 ORDER BY
		       CASE WHEN $4 THEN zero_result_count ELSE query_count END DESC,
		       query_count DESC,
		       last_seen_at DESC
		 LIMIT $5`,
		opts.TenantID, opts.From, opts.To, zeroOnly, opts.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search quality queries: %w", err)
	}
	defer rows.Close()
	var out []SearchQualityQueryAggregate
	for rows.Next() {
		var row SearchQualityQueryAggregate
		if err := rows.Scan(
			&row.QueryHash, &row.QueryPreview, &row.QueryCount, &row.ZeroResultCount,
			&row.FallbackCount, &row.ClickCount, &row.ClickedRunCount,
			&row.AverageResultCount, &row.P95LatencyMS, &row.LastSeenAt,
		); err != nil {
			return nil, fmt.Errorf("scan search quality query: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *FeedbackRepo) SearchFallbackBreakdown(
	ctx context.Context,
	opts SearchQualityQueryOpts,
) ([]SearchFallbackAggregate, error) {
	opts = normalizeSearchQualityQueryOpts(opts)
	rows, err := r.pool.Query(ctx, `
		WITH fallbacks AS (
			SELECT COALESCE(NULLIF(fallback_reason, ''), 'unknown') AS reason
			  FROM feedback_search_runs
			 WHERE tenant_id = $1
			   AND created_at >= $2
			   AND created_at < $3
			   AND used_keyword_fallback
		),
		total AS (
			SELECT COUNT(*)::float8 AS n FROM fallbacks
		)
		SELECT fallbacks.reason,
		       COUNT(*) AS count,
		       CASE WHEN total.n = 0 THEN 0 ELSE COUNT(*)::float8 / total.n END AS share
		  FROM fallbacks
		  CROSS JOIN total
		 GROUP BY fallbacks.reason, total.n
		 ORDER BY count DESC, fallbacks.reason ASC
		 LIMIT $4`,
		opts.TenantID, opts.From, opts.To, opts.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search fallback breakdown: %w", err)
	}
	defer rows.Close()
	var out []SearchFallbackAggregate
	for rows.Next() {
		var row SearchFallbackAggregate
		if err := rows.Scan(&row.Reason, &row.Count, &row.Share); err != nil {
			return nil, fmt.Errorf("scan search fallback breakdown: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *FeedbackRepo) SearchIndexHealth(ctx context.Context, tenantID string) (SearchIndexHealth, error) {
	var out SearchIndexHealth
	var model *string
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE embedding IS NOT NULL),
		       (SELECT embedding_model
		          FROM user_feedback
		         WHERE tenant_id = $1
		           AND embedding IS NOT NULL
		           AND deleted_at IS NULL
		         GROUP BY embedding_model
		         ORDER BY COUNT(*) DESC
		         LIMIT 1),
		       COUNT(*) FILTER (WHERE embedding IS NULL),
		       MIN(created_at) FILTER (WHERE embedding IS NULL)
		  FROM user_feedback
		 WHERE tenant_id = $1
		   AND deleted_at IS NULL`,
		tenantID,
	).Scan(
		&out.TotalLiveFeedback, &out.TotalWithEmbeddings, &model,
		&out.MissingFeedbackCount, &out.OldestMissingFeedbackAt,
	)
	if err != nil {
		return SearchIndexHealth{}, fmt.Errorf("search index health: %w", err)
	}
	out.EmbeddingModel = ptrext.Indirect(model)
	return out, nil
}

func (r *FeedbackRepo) SearchRankingVersions(ctx context.Context, tenantID string) ([]SearchRankingVersion, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ranking_version, status, traffic_percent, notes, updated_at
		  FROM feedback_search_ranking_versions
		 WHERE tenant_id = $1
		 ORDER BY
		       CASE status
		         WHEN 'active' THEN 0
		         WHEN 'canary' THEN 1
		         WHEN 'shadow' THEN 2
		         WHEN 'draft' THEN 3
		         ELSE 4
		       END,
		       updated_at DESC,
		       ranking_version ASC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("search ranking versions: %w", err)
	}
	defer rows.Close()
	var out []SearchRankingVersion
	for rows.Next() {
		var row SearchRankingVersion
		if err := rows.Scan(
			&row.RankingVersion, &row.Status, &row.TrafficPercent,
			&row.Notes, &row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan search ranking version: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func clampSearchRatio(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 1:
		return 1
	default:
		return value
	}
}
