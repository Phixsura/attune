// Package repo — console analytics/aggregate queries over user_feedback.
// Split from feedback_console.go to honor the listen ≤300-line file rule
// (CLAUDE.md 律 2). These power the dashboard widgets + weekly digest
// (usage bars, kind donut, top-modules line); the list/detail read path
// stays in feedback_console.go.
package repo

import (
	"context"
	"fmt"
	"time"
)

// UsageBucket is one day's ingest count, returned by UsageByDay for the
// /usage endpoint. Wave 3+ billing pivots these into invoice line items.
type UsageBucket struct {
	Bucket time.Time
	Value  int64
}

// UsageByDay returns daily ingest counts for tenant in [from, to). Zero-row
// days are NOT in the result — the SPA fills gaps as empty bars.
//
// Timezone: bucket boundaries are UTC days. Asia/Shanghai tenants may see
// up to 8h of "first-day" rows attributed to the prior day. Acceptable
// until billing-grade accuracy lands at Wave 3.
func (r *FeedbackRepo) UsageByDay(
	ctx context.Context, tenantID string, from, to time.Time,
) ([]UsageBucket, error) {
	rows, err := r.pool.Query(ctx, `
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

// KindCounts returns the count of feedback rows per enriched_kind within
// the [from, to) window, scoped to tenant. Unenriched rows
// (enriched_kind IS NULL) bucket under the key "unknown" so the donut
// never has a silent gap.
func (r *FeedbackRepo) KindCounts(
	ctx context.Context, tenantID string, from, to time.Time,
) (map[string]int64, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT COALESCE(enriched_kind, 'unknown') AS kind, COUNT(*)
		  FROM user_feedback
		 WHERE tenant_id = $1
		   AND created_at >= $2
		   AND created_at < $3
		 GROUP BY kind`,
		tenantID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("kind counts: %w", err)
	}
	defer rows.Close()
	out := make(map[string]int64, 6)
	for rows.Next() {
		var k string
		var n int64
		if err := rows.Scan(&k, &n); err != nil {
			return nil, fmt.Errorf("scan kind count: %w", err)
		}
		out[k] = n
	}
	return out, rows.Err()
}

// TopModulesByTenant returns the top-N module strings (flattened from
// enriched_modules JSONB array) by occurrence count, within [from, to).
// Powers the weekly digest "top modules" line.
//
// Uses jsonb_array_elements_text to unnest the JSONB array — cheap on
// Y1 data sizes; switch to a materialized view at Wave 3 if it becomes
// a hot path.
func (r *FeedbackRepo) TopModulesByTenant(
	ctx context.Context, tenantID string, from, to time.Time, n int,
) ([]string, error) {
	if n <= 0 {
		n = 3
	}
	rows, err := r.pool.Query(ctx, `
		SELECT module, COUNT(*) AS c
		  FROM user_feedback,
		       LATERAL jsonb_array_elements_text(COALESCE(enriched_modules, '[]'::jsonb)) AS module
		 WHERE tenant_id = $1
		   AND created_at >= $2
		   AND created_at <  $3
		 GROUP BY module
		 ORDER BY c DESC
		 LIMIT $4`,
		tenantID, from, to, n,
	)
	if err != nil {
		return nil, fmt.Errorf("top modules: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		var c int64
		if err := rows.Scan(&m, &c); err != nil {
			return nil, fmt.Errorf("scan top module: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
