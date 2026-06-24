// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// evaluateEncoding fails when the restored database is not UTF8 — attune assumes
// UTF8, and a restore into a non-UTF8 database silently corrupts multibyte text.
func evaluateEncoding(encoding string) CheckResult {
	r := CheckResult{Name: "encoding"}
	if !strings.EqualFold(encoding, "UTF8") {
		r.Status = StatusFail
		r.Message = fmt.Sprintf("restored database encoding is %q, expected UTF8 — multibyte text may be corrupted", encoding)
		return r
	}
	r.Status = StatusPass
	r.Message = "database encoding is UTF8"
	return r
}

func gatherEncoding(ctx context.Context, pool *pgxpool.Pool) CheckResult {
	var enc string
	if err := pool.QueryRow(ctx,
		`SELECT pg_encoding_to_char(encoding) FROM pg_database WHERE datname = current_database()`).Scan(&enc); err != nil {
		return CheckResult{Name: "encoding", Status: StatusFail, Message: "cannot read database encoding"}
	}
	return evaluateEncoding(enc)
}

// evaluateMatviews fails when any materialized view is unpopulated — a restore
// that recreated the views but never REFRESHed them, so they return no rows.
func evaluateMatviews(unpopulated []string) CheckResult {
	r := CheckResult{Name: "materialized_views"}
	if len(unpopulated) > 0 {
		sort.Strings(unpopulated)
		r.Status = StatusFail
		r.Message = fmt.Sprintf("%d unpopulated materialized view(s) after restore (need REFRESH): %s",
			len(unpopulated), strings.Join(unpopulated, ", "))
		return r
	}
	r.Status = StatusPass
	r.Message = "all materialized views are populated"
	return r
}

// evaluateExtensions fails when the restored database is missing an extension
// present in the live baseline — a restore that dropped an extension a feature
// depends on. Baseline-relative; skipped without a baseline.
func evaluateExtensions(restored, baseline []string) CheckResult {
	r := CheckResult{Name: "extensions"}
	if baseline == nil {
		r.Status = StatusSkip
		r.Message = "no baseline supplied; extension set not compared"
		return r
	}
	have := make(map[string]bool, len(restored))
	for _, e := range restored {
		have[e] = true
	}
	var missing []string
	for _, e := range baseline {
		if !have[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		r.Status = StatusFail
		r.Message = fmt.Sprintf("%d extension(s) present in live but missing after restore: %s",
			len(missing), strings.Join(missing, ", "))
		return r
	}
	r.Status = StatusPass
	r.Message = fmt.Sprintf("all %d live extension(s) present", len(baseline))
	return r
}

func gatherExtensions(ctx context.Context, pool *pgxpool.Pool, baseline []string) CheckResult {
	if baseline == nil {
		return evaluateExtensions(nil, nil)
	}
	restored, err := extensionNames(ctx, pool)
	if err != nil {
		return CheckResult{Name: "extensions", Status: StatusFail, Message: "cannot read installed extensions"}
	}
	return evaluateExtensions(restored, baseline)
}

func extensionNames(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT extname FROM pg_extension ORDER BY extname`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// BaselineExtensions returns the live database's installed extension names, for
// the extensions check to compare a restored DB against.
func BaselineExtensions(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	return extensionNames(ctx, pool)
}

func gatherMatviews(ctx context.Context, pool *pgxpool.Pool) CheckResult {
	rows, err := pool.Query(ctx,
		`SELECT schemaname || '.' || matviewname FROM pg_matviews WHERE NOT ispopulated`)
	if err != nil {
		return CheckResult{Name: "materialized_views", Status: StatusFail, Message: "cannot read materialized view state"}
	}
	defer rows.Close()
	var unpopulated []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return CheckResult{Name: "materialized_views", Status: StatusFail, Message: "cannot scan materialized view row"}
		}
		unpopulated = append(unpopulated, name)
	}
	if rows.Err() != nil {
		return CheckResult{Name: "materialized_views", Status: StatusFail, Message: "error reading materialized view state"}
	}
	return evaluateMatviews(unpopulated)
}
