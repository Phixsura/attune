// Package repo — console analytics/aggregate queries over user_feedback.
// Split from feedback_console.go so neither file becomes a grab-bag.
// These power the dashboard widgets + weekly digest (usage bars,
// top-values-per-dim, urgent ratio); the list/detail read path stays
// in feedback_console.go.
package feedback

import (
	"context"
	"fmt"
	"time"
)

// UsageBucket is one day's ingest count, returned by UsageByDay for the
// /usage endpoint.
type UsageBucket struct {
	Bucket time.Time
	Value  int64
}

// UsageByDay returns daily ingest counts for tenant in [from, to). Zero-row
// days are NOT in the result — the SPA fills gaps as empty bars.
//
// Timezone: bucket boundaries are UTC days.
func (r *FeedbackRepo) UsageByDay(
	ctx context.Context, tenantID string, from, to time.Time,
) ([]UsageBucket, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT date_trunc('day', created_at) AS bucket, COUNT(*)
		  FROM user_feedback
		 WHERE tenant_id = $1
		   AND created_at >= $2
		   AND created_at < $3
		 GROUP BY bucket
		 ORDER BY bucket ASC`,
		tenantID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("usage by day: %w", err)
	}
	defer rows.Close()
	var out []UsageBucket
	for rows.Next() {
		var b UsageBucket
		if err := rows.Scan(&b.Bucket, &b.Value); err != nil {
			return nil, fmt.Errorf("scan usage bucket: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ValueCount is one row of TopValuesByDim.
type ValueCount struct {
	Value string
	Count int64
}

// TopValuesByDim returns the top-N stable Value strings emitted under
// `dim` within [from, to), DESC by occurrence count. Powers the weekly
// digest "top values" line and the feedback dashboard distribution
// charts. Works uniformly for single-kind (`-> 'dim'`) and multi-kind
// (`jsonb_path_query`) dims: callers indicate which via `multi`.
func (r *FeedbackRepo) TopValuesByDim(
	ctx context.Context, tenantID, dim string, multi bool, from, to time.Time, n int,
) ([]ValueCount, error) {
	if n <= 0 {
		n = 5
	}
	var sql string
	if multi {
		sql = `
		SELECT v::text AS val, COUNT(*) AS c
		  FROM user_feedback,
		       jsonb_array_elements_text(COALESCE(enriched_attrs -> $4, '[]'::jsonb)) AS v
		 WHERE tenant_id = $1
		   AND created_at >= $2
		   AND created_at <  $3
		 GROUP BY val
		 ORDER BY c DESC
		 LIMIT $5`
	} else {
		sql = `
		SELECT (enriched_attrs ->> $4) AS val, COUNT(*) AS c
		  FROM user_feedback
		 WHERE tenant_id = $1
		   AND created_at >= $2
		   AND created_at <  $3
		   AND enriched_attrs ? $4
		 GROUP BY val
		 ORDER BY c DESC
		 LIMIT $5`
	}
	rows, err := r.pool.Query(ctx, sql, tenantID, from, to, dim, n)
	if err != nil {
		return nil, fmt.Errorf("top values for dim %q: %w", dim, err)
	}
	defer rows.Close()
	var out []ValueCount
	for rows.Next() {
		var vc ValueCount
		if err := rows.Scan(&vc.Value, &vc.Count); err != nil {
			return nil, fmt.Errorf("scan top value: %w", err)
		}
		out = append(out, vc)
	}
	return out, rows.Err()
}

// UrgentCount returns the count of rows with is_urgent = true within
// [from, to). Powers the weekly digest "urgent count" headline and
// the feedback dashboard urgent ratio.
func (r *FeedbackRepo) UrgentCount(
	ctx context.Context, tenantID string, from, to time.Time,
) (int64, error) {
	var n int64
	err := r.pool.QueryRow(
		ctx, `
		SELECT COUNT(*)
		  FROM user_feedback
		 WHERE tenant_id = $1
		   AND created_at >= $2
		   AND created_at < $3
		   AND is_urgent = TRUE`,
		tenantID, from, to,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("urgent count: %w", err)
	}
	return n, nil
}
