// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// evaluateConstraints grades unvalidated foreign-key / check constraints
// RELATIVE to a live baseline: some NOT VALID constraints are intentional and
// ship with the schema, so a healthy restore preserves live's count. A restore
// that INTRODUCED new unvalidated constraints (e.g. a partial restore that added
// FKs NOT VALID and never validated them) is the bug. Without a baseline the
// absolute count is not actionable, so the check is skipped.
func evaluateConstraints(restored int, baseline *int) CheckResult {
	r := CheckResult{Name: "constraints"}
	if baseline == nil {
		r.Status = StatusSkip
		r.Message = "no baseline supplied; unvalidated-constraint count is not actionable without comparison"
		return r
	}
	base := ptrext.Indirect(baseline)
	if restored > base {
		r.Status = StatusFail
		r.Message = fmt.Sprintf("restore introduced %d new unvalidated constraint(s) vs live (%d → %d) — data integrity is unverified",
			restored-base, base, restored)
		return r
	}
	r.Status = StatusPass
	r.Message = fmt.Sprintf("constraint validation state matches live (%d unvalidated)", restored)
	return r
}

// evaluateSequences fails when any identity/serial sequence is behind its
// owning column's maximum value — the next insert would reuse an existing id.
// This is a classic restore bug (data loaded, sequences not reset).
func evaluateSequences(behind []string) CheckResult {
	r := CheckResult{Name: "sequences"}
	if len(behind) > 0 {
		sort.Strings(behind)
		r.Status = StatusFail
		r.Message = fmt.Sprintf("%d sequence(s) behind their column max (next insert would collide): %s",
			len(behind), strings.Join(behind, ", "))
		return r
	}
	r.Status = StatusPass
	r.Message = "all identity/serial sequences are ahead of their column max"
	return r
}

func gatherConstraints(ctx context.Context, pool *pgxpool.Pool, baseline *int) CheckResult {
	n, err := unvalidatedConstraintCount(ctx, pool)
	if err != nil {
		return CheckResult{
			Name:    "constraints",
			Status:  StatusFail,
			Message: "cannot read constraint validity on the restored database",
		}
	}
	return evaluateConstraints(n, baseline)
}

func unvalidatedConstraintCount(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var n int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_constraint WHERE contype IN ('f', 'c') AND NOT convalidated`).Scan(&n)
	return n, err
}

// BaselineUnvalidatedCount returns the live database's unvalidated-constraint
// count, for the constraints check to compare a restored DB against.
func BaselineUnvalidatedCount(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	return unvalidatedConstraintCount(ctx, pool)
}

type seqRef struct {
	schema, seq, tbl, col string
}

func gatherSequences(ctx context.Context, pool *pgxpool.Pool) CheckResult {
	refs, err := sequenceOwners(ctx, pool)
	if err != nil {
		return CheckResult{
			Name:    "sequences",
			Status:  StatusFail,
			Message: "cannot enumerate sequences on the restored database",
		}
	}
	var behind []string
	for _, sr := range refs {
		ok, isBehind := sequenceBehind(ctx, pool, sr)
		if !ok {
			continue
		}
		if isBehind {
			behind = append(behind, sr.schema+"."+sr.seq)
		}
	}
	return evaluateSequences(behind)
}

func sequenceOwners(ctx context.Context, pool *pgxpool.Pool) ([]seqRef, error) {
	rows, err := pool.Query(ctx, `
		SELECT n.nspname, s.relname, t.relname, a.attname
		FROM pg_class s
		JOIN pg_depend d ON d.objid = s.oid
			AND d.classid = 'pg_class'::regclass
			AND d.refclassid = 'pg_class'::regclass
			AND d.deptype IN ('a', 'i')
		JOIN pg_class t ON t.oid = d.refobjid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = d.refobjsubid
		JOIN pg_namespace n ON n.oid = s.relnamespace
		WHERE s.relkind = 'S' AND n.nspname NOT IN ('pg_catalog', 'information_schema')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []seqRef
	for rows.Next() {
		var sr seqRef
		if err := rows.Scan(&sr.schema, &sr.seq, &sr.tbl, &sr.col); err != nil {
			return nil, err
		}
		refs = append(refs, sr)
	}
	return refs, rows.Err()
}

// sequenceBehind reports whether the sequence's next value would be <= the
// owning column's max (a collision). ok=false means the comparison could not be
// made (e.g. permission), and the sequence is skipped rather than failed.
func sequenceBehind(ctx context.Context, pool *pgxpool.Pool, sr seqRef) (ok, behind bool) {
	var lastVal int64
	var isCalled bool
	seqIdent := pgx.Identifier{sr.schema, sr.seq}.Sanitize()
	if err := pool.QueryRow(ctx, "SELECT last_value, is_called FROM "+seqIdent).Scan(&lastVal, &isCalled); err != nil {
		return false, false
	}
	var maxVal int64
	q := "SELECT COALESCE(max(" + pgx.Identifier{sr.col}.Sanitize() + "), 0) FROM " + pgx.Identifier{sr.schema, sr.tbl}.Sanitize()
	if err := pool.QueryRow(ctx, q).Scan(&maxVal); err != nil {
		return false, false
	}
	if maxVal <= 0 {
		return true, false
	}
	next := lastVal
	if isCalled {
		next = lastVal + 1
	}
	return true, next <= maxVal
}
