// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// RecomputeOpts parameterizes one recompute pass over a tenant's window of
// civil dates (inclusive). The window predicate on user_feedback is the
// UTC half-open interval [FromDate 00:00 loc, ToDate+1d 00:00 loc).
type RecomputeOpts struct {
	TenantID      string
	Location      *time.Location
	FromDate      time.Time
	ToDate        time.Time
	ConfigVersion int
	// MinCount is the HAVING floor for cluster buckets (natural top-N).
	MinCount int64
	// Dimensions supplies the tenant's dimension set; only dims with a
	// non-empty taxonomy are sliced (bounded value domain).
	Dimensions domain.DimensionSet
	// CustomSlices are pre-validated custom definitions to materialize.
	CustomSlices []CustomSlice
}

// perDimensionValueCap bounds how many distinct values of one dimension
// get buckets in a single recompute window (top-N by count).
const perDimensionValueCap = 50

// sampleIDsCap bounds sample_feedback_ids per bucket.
const sampleIDsCap = 5

// upsertBucketSQL is shared by every slice family; the SELECT feeding it
// differs per family. Rows carry (bucket_date, slice_type, slice_key,
// slice_display, count, samples).
const upsertBucketSQL = `
	INSERT INTO feedback_volume_buckets
	  (tenant_id, bucket_date, slice_type, slice_key, slice_display,
	   config_version, feedback_count, sample_feedback_ids, computed_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
	ON CONFLICT (tenant_id, bucket_date, slice_type, slice_key)
	DO UPDATE SET
	  feedback_count      = EXCLUDED.feedback_count,
	  sample_feedback_ids = EXCLUDED.sample_feedback_ids,
	  slice_display       = EXCLUDED.slice_display,
	  config_version      = EXCLUDED.config_version,
	  computed_at         = NOW()`

// bucketRow is one aggregate result destined for the upsert.
type bucketRow struct {
	date    string
	stype   string
	key     string
	display string
	count   int64
	samples []int64
}

// RecomputeWindow rebuilds all buckets for the window inside one
// transaction: aggregate each slice family from user_feedback, upsert, and
// finally delete window buckets this pass did not touch (data vanished —
// GDPR deletes, recluster, cohort churn).
func (r *Repo) RecomputeWindow(ctx context.Context, opts RecomputeOpts) error {
	fromUTC := civilDate(opts.FromDate, opts.Location).UTC()
	toUTC := civilDate(opts.ToDate, opts.Location).AddDate(0, 0, 1).UTC()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("anomaly rollup begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var txStart time.Time
	if err := tx.QueryRow(ctx, `SELECT NOW()`).Scan(&txStart); err != nil {
		return fmt.Errorf("anomaly rollup clock: %w", err)
	}

	rows, err := r.aggregateWindow(ctx, tx, opts, fromUTC, toUTC)
	if err != nil {
		return err
	}
	// Deterministic upsert order: two worker replicas recompute the same
	// tenant concurrently (run claims guard detection, not rollup); rows
	// upserted in differing orders across overlapping key sets would
	// deadlock. Sorted order makes lock acquisition monotonic.
	sort.Slice(rows, func(i, j int) bool {
		a, b := &rows[i], &rows[j]
		if a.date != b.date {
			return a.date < b.date
		}
		if a.stype != b.stype {
			return a.stype < b.stype
		}
		return a.key < b.key
	})
	for i := range rows {
		b := &rows[i]
		if _, err := tx.Exec(ctx, upsertBucketSQL,
			opts.TenantID, b.date, b.stype, b.key, b.display,
			opts.ConfigVersion, b.count, b.samples); err != nil {
			return fmt.Errorf("anomaly rollup upsert %s/%s: %w", b.stype, b.key, err)
		}
	}

	// Zeroing pass: window buckets untouched by this recompute no longer
	// have backing rows — remove them so drops and GDPR erasure are
	// reflected.
	if _, err := tx.Exec(ctx, `
		DELETE FROM feedback_volume_buckets
		WHERE tenant_id = $1 AND bucket_date BETWEEN $2 AND $3 AND computed_at < $4`,
		opts.TenantID, dateStr(civilDate(opts.FromDate, opts.Location)),
		dateStr(civilDate(opts.ToDate, opts.Location)), txStart); err != nil {
		return fmt.Errorf("anomaly rollup zeroing: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("anomaly rollup commit: %w", err)
	}
	return nil
}

// aggregateWindow collects bucket rows from every slice family.
func (r *Repo) aggregateWindow(
	ctx context.Context, tx pgx.Tx, opts RecomputeOpts, fromUTC, toUTC time.Time,
) ([]bucketRow, error) {
	var out []bucketRow
	collect := func(rows pgx.Rows, stype string, keyFn func(vals []string) (key, display string)) error {
		defer rows.Close()
		for rows.Next() {
			var (
				date    time.Time
				vals    []string
				count   int64
				samples []int64
			)
			var v1, v2 *string
			if err := rows.Scan(&date, &v1, &v2, &count, &samples); err != nil {
				return err
			}
			for _, v := range []*string{v1, v2} {
				if v != nil {
					vals = append(vals, ptrext.Indirect(v))
				}
			}
			key, display := keyFn(vals)
			if len(samples) > sampleIDsCap {
				samples = samples[:sampleIDsCap]
			}
			out = append(out, bucketRow{
				date: dateStr(date), stype: stype, key: key,
				display: display, count: count, samples: samples,
			})
		}
		return rows.Err()
	}

	if err := r.aggregateCore(ctx, tx, opts, fromUTC, toUTC, collect); err != nil {
		return nil, err
	}
	if err := r.aggregateDimensions(ctx, tx, opts, fromUTC, toUTC, collect); err != nil {
		return nil, err
	}
	if err := r.aggregateCustom(ctx, tx, opts, fromUTC, toUTC, collect); err != nil {
		return nil, err
	}
	return out, nil
}

type collectFn func(rows pgx.Rows, stype string, keyFn func(vals []string) (key, display string)) error

// aggregateCore covers total, source, cluster, and cohort families.
func (r *Repo) aggregateCore(
	ctx context.Context, tx pgx.Tx, opts RecomputeOpts, fromUTC, toUTC time.Time, collect collectFn,
) error {
	tz := opts.Location.String()

	rows, err := tx.Query(ctx, `
		SELECT (f.created_at AT TIME ZONE $4)::date AS d, NULL::text, NULL::text,
		       COUNT(*), (array_agg(f.id ORDER BY f.id DESC))[1:5]
		FROM user_feedback f
		WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3
		GROUP BY d`, opts.TenantID, fromUTC, toUTC, tz)
	if err != nil {
		return fmt.Errorf("anomaly rollup total: %w", err)
	}
	if err := collect(rows, SliceTotal, func([]string) (string, string) {
		return SliceTotal, "All feedback"
	}); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `
		SELECT (f.created_at AT TIME ZONE $4)::date AS d, f.source, NULL::text,
		       COUNT(*), (array_agg(f.id ORDER BY f.id DESC))[1:5]
		FROM user_feedback f
		WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3
		GROUP BY d, f.source`, opts.TenantID, fromUTC, toUTC, tz)
	if err != nil {
		return fmt.Errorf("anomaly rollup source: %w", err)
	}
	if err := collect(rows, SliceSource, func(vals []string) (string, string) {
		return SourceSliceKey(vals[0]), vals[0]
	}); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `
		SELECT (f.created_at AT TIME ZONE $4)::date AS d, f.cluster_id::text,
		       COALESCE(f.cluster_label,''),
		       COUNT(*), (array_agg(f.id ORDER BY f.id DESC))[1:5]
		FROM user_feedback f
		WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3
		  AND f.cluster_id IS NOT NULL
		GROUP BY d, f.cluster_id, f.cluster_label
		HAVING COUNT(*) >= $5`, opts.TenantID, fromUTC, toUTC, tz, opts.MinCount)
	if err != nil {
		return fmt.Errorf("anomaly rollup cluster: %w", err)
	}
	if err := collect(rows, SliceCluster, func(vals []string) (string, string) {
		display := vals[0]
		if len(vals) > 1 && vals[1] != "" {
			display = vals[1]
		}
		return "cluster:" + vals[0], display
	}); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `
		SELECT (f.created_at AT TIME ZONE $4)::date AS d, cm.cohort_id::text, co.name,
		       COUNT(*), (array_agg(f.id ORDER BY f.id DESC))[1:5]
		FROM user_feedback f
		JOIN cohort_memberships cm ON cm.tenant_id = f.tenant_id
		     AND cm.external_user_id = f.subject_key AND cm.left_at IS NULL
		JOIN cohorts co ON co.id = cm.cohort_id
		WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3
		  AND f.subject_key <> ''
		GROUP BY d, cm.cohort_id, co.name`, opts.TenantID, fromUTC, toUTC, tz)
	if err != nil {
		return fmt.Errorf("anomaly rollup cohort: %w", err)
	}
	return collect(rows, SliceCohort, func(vals []string) (string, string) {
		display := vals[0]
		if len(vals) > 1 && vals[1] != "" {
			display = vals[1]
		}
		return "cohort:" + vals[0], display
	})
}

// aggregateDimensions covers taxonomy-bounded dimensions: single dims read
// the scalar value, multi dims expand the JSONB array. Values are capped at
// perDimensionValueCap per dimension per window (top-N by count).
func (r *Repo) aggregateDimensions(
	ctx context.Context, tx pgx.Tx, opts RecomputeOpts, fromUTC, toUTC time.Time, collect collectFn,
) error {
	tz := opts.Location.String()
	for _, dim := range opts.Dimensions {
		if len(dim.Taxonomy) == 0 {
			continue // unbounded value domain: not sliced
		}
		// The value cap must bound DISTINCT VALUES, not (date, value) rows:
		// capping rows would silently drop whole days from lower-volume
		// values and punch zero-holes into their baselines. The CTE picks
		// the window's top-N values by total count; the outer query then
		// keeps EVERY day for those values.
		var query string
		if dim.Kind == domain.DimMulti {
			query = `
				WITH top_vals AS (
				  SELECT v.val
				  FROM user_feedback f
				  CROSS JOIN LATERAL jsonb_array_elements_text(
				    COALESCE(f.enriched_attrs -> $5, '[]'::jsonb)) AS v(val)
				  WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3
				    AND f.enrichment_status='enriched'
				  GROUP BY v.val ORDER BY COUNT(*) DESC LIMIT $6
				)
				SELECT (f.created_at AT TIME ZONE $4)::date AS d, v.val, NULL::text,
				       COUNT(*), (array_agg(f.id ORDER BY f.id DESC))[1:5]
				FROM user_feedback f
				CROSS JOIN LATERAL jsonb_array_elements_text(
				  COALESCE(f.enriched_attrs -> $5, '[]'::jsonb)) AS v(val)
				WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3
				  AND f.enrichment_status='enriched'
				  AND v.val IN (SELECT val FROM top_vals)
				GROUP BY d, v.val`
		} else {
			query = `
				WITH top_vals AS (
				  SELECT f.enriched_attrs ->> $5 AS val
				  FROM user_feedback f
				  WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3
				    AND f.enrichment_status='enriched'
				    AND f.enriched_attrs ->> $5 IS NOT NULL
				  GROUP BY val ORDER BY COUNT(*) DESC LIMIT $6
				)
				SELECT (f.created_at AT TIME ZONE $4)::date AS d,
				       f.enriched_attrs ->> $5 AS val, NULL::text,
				       COUNT(*), (array_agg(f.id ORDER BY f.id DESC))[1:5]
				FROM user_feedback f
				WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3
				  AND f.enrichment_status='enriched'
				  AND f.enriched_attrs ->> $5 IN (SELECT val FROM top_vals)
				GROUP BY d, val`
		}
		rows, err := tx.Query(ctx, query,
			opts.TenantID, fromUTC, toUTC, tz, dim.Name, perDimensionValueCap)
		if err != nil {
			return fmt.Errorf("anomaly rollup dim %s: %w", dim.Name, err)
		}
		name := dim.Name
		if err := collect(rows, SliceDimension, func(vals []string) (string, string) {
			return DimensionSliceKey(name, vals[0]), name + "=" + vals[0]
		}); err != nil {
			return err
		}
	}
	return nil
}

// aggregateCustom materializes each custom slice via compiled conjunctive
// predicates (parameterized — never string-concatenated values).
func (r *Repo) aggregateCustom(
	ctx context.Context, tx pgx.Tx, opts RecomputeOpts, fromUTC, toUTC time.Time, collect collectFn,
) error {
	tz := opts.Location.String()
	for _, cs := range opts.CustomSlices {
		where, args := compileCustomConditions(cs.Conditions, 5)
		query := `
			SELECT (f.created_at AT TIME ZONE $4)::date AS d, NULL::text, NULL::text,
			       COUNT(*), (array_agg(f.id ORDER BY f.id DESC))[1:5]
			FROM user_feedback f
			WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3` + where + `
			GROUP BY d`
		full := append([]any{opts.TenantID, fromUTC, toUTC, tz}, args...)
		rows, err := tx.Query(ctx, query, full...)
		if err != nil {
			return fmt.Errorf("anomaly rollup custom %s: %w", cs.ID, err)
		}
		key, display := CustomSliceKey(cs.ID), cs.Display
		if err := collect(rows, SliceCustom, func([]string) (string, string) {
			return key, display
		}); err != nil {
			return err
		}
	}
	return nil
}

// compileCustomConditions renders AND-ed predicates for a custom slice.
// startIdx is the first free positional parameter number minus one (args
// appended after the shared window params).
func compileCustomConditions(conds []CustomCondition, startIdx int) (string, []any) {
	var sb strings.Builder
	var args []any
	n := startIdx
	for _, c := range conds {
		switch c.Field {
		case "source":
			fmt.Fprintf(&sb, " AND f.source = ANY($%d)", n)
			args = append(args, c.Values)
			n++
		case "dimension":
			if c.Multi {
				fmt.Fprintf(&sb,
					" AND f.enrichment_status='enriched' AND f.enriched_attrs -> $%d ?| $%d",
					n, n+1)
			} else {
				// The scalar form ORs an array-containment check so callers
				// that don't know the dimension kind (the worker's slice →
				// condition re-expression) still match multi-value dims;
				// jsonb_typeof guards ?| against misfiring on strings.
				fmt.Fprintf(&sb,
					" AND f.enrichment_status='enriched' AND (f.enriched_attrs ->> $%d = ANY($%d)"+
						" OR (jsonb_typeof(f.enriched_attrs -> $%d) = 'array' AND f.enriched_attrs -> $%d ?| $%d))",
					n, n+1, n, n, n+1)
			}
			args = append(args, c.Name, c.Values)
			n += 2
		case "cohort":
			fmt.Fprintf(&sb,
				" AND EXISTS (SELECT 1 FROM cohort_memberships cm WHERE cm.tenant_id = f.tenant_id"+
					" AND cm.cohort_id = ANY($%d::uuid[]) AND cm.external_user_id = f.subject_key"+
					" AND cm.left_at IS NULL)", n)
			args = append(args, c.Values)
			n++
		case "cluster":
			// Internal-only field (contribution scoping); not part of the
			// operator-facing custom-slice whitelist.
			fmt.Fprintf(&sb, " AND f.cluster_id = ANY($%d::uuid[])", n)
			args = append(args, c.Values)
			n++
		}
	}
	return sb.String(), args
}

// BaselineCounts returns feedback_count for one slice on each requested
// date, in input order, with 0 for missing buckets (count semantics:
// absence means zero).
func (r *Repo) BaselineCounts(
	ctx context.Context, tenantID, sliceType, sliceKey string, dates []time.Time,
) ([]int64, error) {
	strs := make([]string, len(dates))
	for i, d := range dates {
		strs[i] = dateStr(d)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT bucket_date::text, feedback_count FROM feedback_volume_buckets
		WHERE tenant_id=$1 AND slice_type=$2 AND slice_key=$3 AND bucket_date = ANY($4::date[])`,
		tenantID, sliceType, sliceKey, strs)
	if err != nil {
		return nil, fmt.Errorf("anomaly baseline counts: %w", err)
	}
	defer rows.Close()
	byDate := make(map[string]int64, len(dates))
	for rows.Next() {
		var d string
		var c int64
		if err := rows.Scan(&d, &c); err != nil {
			return nil, err
		}
		byDate[d] = c
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]int64, len(dates))
	for i, s := range strs {
		out[i] = byDate[s]
	}
	return out, nil
}

// SlicesForDetection returns the union of slices present on the detection
// date or any baseline date — the union keeps vanished slices visible as
// drop candidates.
func (r *Repo) SlicesForDetection(
	ctx context.Context, tenantID string, enabled []string,
	detectDate time.Time, baselineDates []time.Time,
) ([]SliceRef, error) {
	all := make([]string, 0, len(baselineDates)+1)
	all = append(all, dateStr(detectDate))
	for _, d := range baselineDates {
		all = append(all, dateStr(d))
	}
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT slice_type, slice_key, slice_display
		FROM feedback_volume_buckets
		WHERE tenant_id=$1 AND slice_type = ANY($2) AND bucket_date = ANY($3::date[])`,
		tenantID, enabled, all)
	if err != nil {
		return nil, fmt.Errorf("anomaly slices for detection: %w", err)
	}
	defer rows.Close()
	var out []SliceRef
	for rows.Next() {
		var s SliceRef
		if err := rows.Scan(&s.Type, &s.Key, &s.Display); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// CountOn returns feedback_count (0 when absent) and sample ids for one
// slice on one date.
func (r *Repo) CountOn(
	ctx context.Context, tenantID, sliceType, sliceKey string, date time.Time,
) (int64, []int64, error) {
	var count int64
	var samples []int64
	err := r.pool.QueryRow(ctx, `
		SELECT feedback_count, sample_feedback_ids FROM feedback_volume_buckets
		WHERE tenant_id=$1 AND bucket_date=$2 AND slice_type=$3 AND slice_key=$4`,
		tenantID, dateStr(date), sliceType, sliceKey).Scan(&count, &samples)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil, nil
		}
		return 0, nil, fmt.Errorf("anomaly count on: %w", err)
	}
	return count, samples, nil
}

// CleanupRetention deletes buckets older than bucketDays and detection runs
// older than runDays. Safe to call every tick.
func (r *Repo) CleanupRetention(ctx context.Context, bucketDays, runDays int) error {
	if _, err := r.pool.Exec(ctx, `
		DELETE FROM feedback_volume_buckets
		WHERE bucket_date < CURRENT_DATE - $1::int`, bucketDays); err != nil {
		return fmt.Errorf("anomaly bucket retention: %w", err)
	}
	if _, err := r.pool.Exec(ctx, `
		DELETE FROM anomaly_detection_runs
		WHERE bucket_date < CURRENT_DATE - $1::int`, runDays); err != nil {
		return fmt.Errorf("anomaly runs retention: %w", err)
	}
	return nil
}

// GroupByAxis describes one contribution grouping axis over the anomalous
// slice's feedback: by source, or by one single-valued dimension.
type GroupByAxis struct {
	Field string // "source" | "dimension"
	Name  string // dimension machine key when Field=="dimension"
}

// GroupCountRow is one grouping value's observed day-count plus its
// per-baseline-date counts for median computation service-side.
type GroupCountRow struct {
	Value    string
	Observed int64
	// BaselineCounts holds this value's count on each requested baseline
	// date, zero-filled, in input order.
	BaselineCounts []int64
}

// GroupCountsByAxis returns, for the feedback matching sliceWhere on date,
// counts grouped by the axis, plus the same grouping over each baseline
// date. slice filtering reuses the custom-condition compiler so any slice
// family can be re-expressed as conditions by the caller.
func (r *Repo) GroupCountsByAxis(
	ctx context.Context, tenantID string, loc *time.Location,
	sliceConds []CustomCondition, axis GroupByAxis,
	date time.Time, baselineDates []time.Time,
) ([]GroupCountRow, error) {
	dates := append([]time.Time{date}, baselineDates...)
	byValue := make(map[string]*GroupCountRow)
	for idx, d := range dates {
		counts, err := r.groupCountsOneDay(ctx, tenantID, loc, sliceConds, axis, d)
		if err != nil {
			return nil, err
		}
		for value, c := range counts {
			row, ok := byValue[value]
			if !ok {
				row = ptrext.Of(GroupCountRow{
					Value:          value,
					BaselineCounts: make([]int64, len(baselineDates)),
				})
				byValue[value] = row
			}
			if idx == 0 {
				row.Observed = c
			} else {
				row.BaselineCounts[idx-1] = c
			}
		}
	}
	out := make([]GroupCountRow, 0, len(byValue))
	for _, row := range byValue {
		out = append(out, ptrext.Indirect(row))
	}
	return out, nil
}

// groupCountsOneDay aggregates one civil day of the filtered feedback by
// the grouping axis.
func (r *Repo) groupCountsOneDay(
	ctx context.Context, tenantID string, loc *time.Location,
	sliceConds []CustomCondition, axis GroupByAxis, day time.Time,
) (map[string]int64, error) {
	fromUTC := civilDate(day, loc).UTC()
	toUTC := civilDate(day, loc).AddDate(0, 0, 1).UTC()

	groupExpr := "f.source"
	args := []any{tenantID, fromUTC, toUTC}
	n := 4
	if axis.Field == "dimension" {
		groupExpr = fmt.Sprintf("f.enriched_attrs ->> $%d", n)
		args = append(args, axis.Name)
		n++
	}
	where, condArgs := compileCustomConditions(sliceConds, n)
	args = append(args, condArgs...)

	query := `
		SELECT ` + groupExpr + ` AS v, COUNT(*)
		FROM user_feedback f
		WHERE f.tenant_id=$1 AND f.created_at>=$2 AND f.created_at<$3` + where + `
		GROUP BY v HAVING ` + groupExpr + ` IS NOT NULL`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("anomaly group counts: %w", err)
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var v string
		var c int64
		if err := rows.Scan(&v, &c); err != nil {
			return nil, err
		}
		out[v] = c
	}
	return out, rows.Err()
}
