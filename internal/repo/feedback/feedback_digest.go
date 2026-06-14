// SPDX-License-Identifier: Apache-2.0

// feedback_digest.go — window aggregation reads for the daily digest (#27).
// Split from feedback_console_stats.go to honor the no-grab-bag-files guidance.
// All windows are [from, to) on created_at; the digest worker computes the
// bounds from the tenant's local "yesterday".
package feedback

import (
	"context"
	"fmt"
	"time"
)

// DigestWindowStats are the totals shown atop a digest: every ingested row in
// the window, how many are enriched, how many enriched rows are still
// unclustered (so a thin themes list is never silently misleading), and the
// urgent count.
type DigestWindowStats struct {
	Total       int
	Enriched    int
	Unclustered int
	Urgent      int
}

// DigestFeedbackRow is one enriched row for the themeless list (1–5 rows) and as
// input to the naive LLM theme-naming fallback (clustering-off tenants).
type DigestFeedbackRow struct {
	ID        int64
	Title     string
	Rationale string
}

// WindowStats returns the digest totals for [from, to) on created_at.
func (r *FeedbackRepo) WindowStats(
	ctx context.Context, tenantID string, from, to time.Time,
) (DigestWindowStats, error) {
	var s DigestWindowStats
	err := r.pool.QueryRow(ctx, `
		SELECT
		  COUNT(*),
		  COUNT(*) FILTER (WHERE enrichment_status = 'done'),
		  COUNT(*) FILTER (WHERE enrichment_status = 'done' AND cluster_id IS NULL),
		  COUNT(*) FILTER (WHERE is_urgent)
		FROM user_feedback
		WHERE tenant_id = $1 AND created_at >= $2 AND created_at < $3`,
		tenantID, from, to,
	).Scan(&s.Total, &s.Enriched, &s.Unclustered, &s.Urgent)
	if err != nil {
		return DigestWindowStats{}, fmt.Errorf("digest window stats: %w", err)
	}
	return s, nil
}

// EnrichedInWindow returns up to `limit` enriched rows (most recent first) in
// [from, to) — the themeless-tier list and the naive-fallback LLM input.
func (r *FeedbackRepo) EnrichedInWindow(
	ctx context.Context, tenantID string, from, to time.Time, limit int,
) ([]DigestFeedbackRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id,
		       COALESCE(NULLIF(enriched_title, ''), LEFT(content, 100)),
		       COALESCE(enriched_rationale, '')
		FROM user_feedback
		WHERE tenant_id = $1
		  AND enrichment_status = 'done'
		  AND created_at >= $2 AND created_at < $3
		ORDER BY created_at DESC
		LIMIT $4`, tenantID, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("enriched in window: %w", err)
	}
	defer rows.Close()

	var out []DigestFeedbackRow
	for rows.Next() {
		var f DigestFeedbackRow
		if err := rows.Scan(&f.ID, &f.Title, &f.Rationale); err != nil {
			return nil, fmt.Errorf("scan digest feedback row: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
