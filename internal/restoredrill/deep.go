// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// perIndexTimeout bounds a single amcheck call so one pathological index cannot
// consume the whole drill budget. An index that exceeds it is left UNVERIFIED
// (it cannot then reach a PASS) — never silently counted as checked.
const perIndexTimeout = 30 * time.Second

type deepDetail struct {
	IndexesChecked int `json:"indexes_checked"`
	IndexesTotal   int `json:"indexes_total"`
}

// checkDeep is the opt-in structural-integrity tier (research: pgbackrest_auto's
// logical validation). It asserts no index is invalid after restore, then runs
// amcheck's bt_index_parent_check(heapallindexed => true) over EVERY B-Tree
// index — catching corruption that schema/row-count checks cannot. A PASS
// requires that every index was actually verified; partial coverage (an index
// that timed out) is a WARN, and a drill that was cancelled mid-scan is a FAIL
// (incomplete), never a pass. Skips cleanly if amcheck is not installed.
func checkDeep(ctx context.Context, pool *pgxpool.Pool) CheckResult {
	r := CheckResult{Name: "deep"}

	var invalid int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_index WHERE NOT indisvalid`).Scan(&invalid); err != nil {
		r.Status = StatusFail
		r.Message = "cannot read index validity on the restored database"
		return r
	}
	if invalid > 0 {
		r.Status = StatusFail
		r.Message = fmt.Sprintf("%d invalid index(es) on the restored database", invalid)
		return r
	}

	var hasAmcheck bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'amcheck')`).Scan(&hasAmcheck); err != nil {
		r.Status = StatusFail
		r.Message = "cannot probe for amcheck on the restored database"
		return r
	}
	if !hasAmcheck {
		r.Status = StatusSkip
		r.Message = "indexes valid; amcheck not installed (CREATE EXTENSION amcheck for structural verification)"
		return r
	}

	checked, total, corruption, qErr := amcheckBTrees(ctx, pool)
	if qErr != nil {
		r.Status = StatusFail
		r.Message = fmt.Sprintf("amcheck did not complete (%v) — index integrity unverified", qErr)
		return r
	}
	r.Detail = deepDetail{IndexesChecked: checked, IndexesTotal: total}
	if corruption != nil {
		r.Status = StatusFail
		r.Message = fmt.Sprintf("amcheck found B-Tree corruption: %v", corruption)
		return r
	}
	if checked < total {
		r.Status = StatusWarn
		r.Message = fmt.Sprintf("amcheck verified %d/%d B-Tree index(es); %d timed out and could not be checked — coverage incomplete",
			checked, total, total-checked)
		return r
	}
	r.Status = StatusPass
	r.Message = fmt.Sprintf("indexes valid; amcheck verified all %d B-Tree index(es) (heapallindexed)", total)
	return r
}

// btreeIndexFilter selects the B-Tree indexes amcheck can actually target:
// regular indexes (relkind 'i') only. PARTITIONED indexes (relkind 'I') are
// btree-AM but have no heap, so bt_index_parent_check rejects them with "only
// B-Tree indexes are supported" — including them would misreport a healthy
// partitioned table as corruption.
const btreeIndexFilter = `
	FROM pg_index i
	JOIN pg_class c ON c.oid = i.indexrelid
	JOIN pg_am am ON am.oid = c.relam
	JOIN pg_namespace n ON n.oid = c.relnamespace
	WHERE am.amname = 'btree' AND c.relkind = 'i' AND i.indisready AND i.indisvalid
	  AND n.nspname NOT IN ('pg_catalog', 'information_schema')`

// amcheckBTrees verifies every B-Tree index. corruption is the first genuine
// bt_index_parent_check failure (a real defect); queryErr is an enumeration
// error or a drill-wide cancellation (the scan could not complete). An index
// that times out is left UNVERIFIED so checked < total, which prevents a PASS
// without being misreported as corruption.
func amcheckBTrees(ctx context.Context, pool *pgxpool.Pool) (checked, total int, corruption, queryErr error) {
	if err := pool.QueryRow(ctx, `SELECT count(*) `+btreeIndexFilter).Scan(&total); err != nil {
		return 0, 0, nil, err
	}
	names, err := btreeIndexNames(ctx, pool)
	if err != nil {
		return 0, total, nil, err
	}
	for _, name := range names {
		if ctx.Err() != nil {
			return checked, total, corruption, ctx.Err() // drill cancelled → incomplete
		}
		ictx, cancel := context.WithTimeout(ctx, perIndexTimeout)
		_, err := pool.Exec(ictx, `SELECT bt_index_parent_check($1::regclass, true)`, name)
		perIndexTimedOut := ictx.Err() != nil && ctx.Err() == nil
		cancel()
		switch {
		case err == nil:
			checked++
		case ctx.Err() != nil:
			return checked, total, corruption, ctx.Err() // cancelled mid-check
		case perIndexTimedOut:
			// could not verify this index in time — leave uncounted, NOT corruption
		case corruption == nil:
			corruption = fmt.Errorf("%s: %w", name, err)
		}
	}
	return checked, total, corruption, nil
}

func btreeIndexNames(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT c.oid::regclass::text `+btreeIndexFilter+` ORDER BY c.oid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}
