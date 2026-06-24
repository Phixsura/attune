// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// perIndexTimeout bounds a single amcheck call so one pathological index cannot
// consume the whole drill budget.
const perIndexTimeout = 30 * time.Second

// deepIndexLimit bounds how many B-Tree indexes amcheck scans per drill. The
// deep tier is opt-in and slow (a full heap + index structural read); the bound
// keeps a drill on a large database finite. Coverage is reported, never silently
// truncated.
const deepIndexLimit = 100

type deepDetail struct {
	IndexesChecked int `json:"indexes_checked"`
	IndexesTotal   int `json:"indexes_total"`
}

// checkDeep is the opt-in structural-integrity tier (research: pgbackrest_auto's
// logical validation). It asserts no index is invalid after restore, then runs
// amcheck's bt_index_parent_check(heapallindexed => true) over B-Tree indexes —
// catching corruption that schema/row-count checks cannot. Skips cleanly if
// amcheck is not installed.
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

	checked, total, firstErr, qErr := amcheckBTrees(ctx, pool)
	if qErr != nil {
		r.Status = StatusFail
		r.Message = "cannot enumerate B-Tree indexes for amcheck"
		return r
	}
	r.Detail = deepDetail{IndexesChecked: checked, IndexesTotal: total}
	if firstErr != nil {
		r.Status = StatusFail
		r.Message = fmt.Sprintf("amcheck found B-Tree corruption: %v", firstErr)
		return r
	}
	r.Status = StatusPass
	r.Message = fmt.Sprintf("indexes valid; amcheck verified %d/%d B-Tree index(es) (heapallindexed)", checked, total)
	return r
}

const btreeIndexFilter = `
	FROM pg_index i
	JOIN pg_class c ON c.oid = i.indexrelid
	JOIN pg_am am ON am.oid = c.relam
	JOIN pg_namespace n ON n.oid = c.relnamespace
	WHERE am.amname = 'btree' AND i.indisready AND i.indisvalid
	  AND n.nspname NOT IN ('pg_catalog', 'information_schema')`

func amcheckBTrees(ctx context.Context, pool *pgxpool.Pool) (checked, total int, firstErr, queryErr error) {
	if err := pool.QueryRow(ctx, `SELECT count(*) `+btreeIndexFilter).Scan(&total); err != nil {
		return 0, 0, nil, err
	}
	names, err := btreeIndexNames(ctx, pool)
	if err != nil {
		return 0, total, nil, err
	}
	for _, name := range names {
		ictx, cancel := context.WithTimeout(ctx, perIndexTimeout)
		_, err := pool.Exec(ictx, `SELECT bt_index_parent_check($1::regclass, true)`, name)
		cancel()
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", name, err)
			}
			continue
		}
		checked++
	}
	return checked, total, firstErr, nil
}

func btreeIndexNames(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT c.oid::regclass::text `+btreeIndexFilter+` ORDER BY c.oid LIMIT $1`, deepIndexLimit)
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
