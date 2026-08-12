//go:build integration

package anomaly_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	anomalyrepo "github.com/Phixsura/attune/internal/repo/anomaly"
	"github.com/Phixsura/attune/internal/testdb"
)

// shanghai is the tenant timezone used across rollup tests; created_at
// values are chosen to straddle UTC midnights so civil-date bucketing is
// actually exercised.
func shanghai(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	return loc
}

// insertFeedback inserts one user_feedback row with full control over the
// columns the rollup reads. enrichedAttrs "" means stay pending.
func insertFeedback(t *testing.T, pool *pgxpool.Pool, tenantID string,
	createdAt time.Time, source, subjectKey, enrichedAttrs string,
	clusterID *uuid.UUID, clusterLabel string,
) int64 {
	t.Helper()
	status := "pending"
	attrs := "{}"
	if enrichedAttrs != "" {
		status = "enriched"
		attrs = enrichedAttrs
	}
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO user_feedback
		  (tenant_id, user_id, source, content, subject_key,
		   enrichment_status, enriched_attrs, cluster_id, cluster_label, created_at)
		VALUES ($1,'u1',$2,'content',$3,$4,$5::jsonb,$6,$7,$8)
		RETURNING id`,
		tenantID, source, subjectKey, status, attrs, clusterID, nullIfEmpty(clusterLabel), createdAt).
		Scan(&id)
	require.NoError(t, err)
	return id
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// dims returns a DimensionSet with a single-valued 'severity' dimension and
// a multi-valued 'labels' dimension, both with taxonomy.
func dims() domain.DimensionSet {
	return domain.DimensionSet{
		{Name: "severity", Kind: domain.DimSingle, Taxonomy: []domain.Taxonomy{
			{Value: "critical"}, {Value: "low"},
		}},
		{Name: "labels", Kind: domain.DimMulti, Taxonomy: []domain.Taxonomy{
			{Value: "a"}, {Value: "b"},
		}},
	}
}

// bucketCount reads feedback_count for one bucket (0, false when absent).
func bucketCount(t *testing.T, pool *pgxpool.Pool, tenantID string, date, sliceType, sliceKey string) (int64, bool) {
	t.Helper()
	var n int64
	err := pool.QueryRow(context.Background(), `
		SELECT feedback_count FROM feedback_volume_buckets
		WHERE tenant_id=$1 AND bucket_date=$2 AND slice_type=$3 AND slice_key=$4`,
		tenantID, date, sliceType, sliceKey).Scan(&n)
	if err != nil {
		return 0, false
	}
	return n, true
}

// freshTenant creates a uniquely-slugged tenant so parallel test runs never
// share bucket rows.
func freshTenant(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var tenantID string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO tenants (slug, name) VALUES ($1,'Rollup T') RETURNING id`,
		fmt.Sprintf("anomaly-rollup-%s", uuid.NewString()[:8])).Scan(&tenantID)
	require.NoError(t, err)
	return tenantID
}

func recomputeOpts(tenantID string, loc *time.Location, from, to time.Time) anomalyrepo.RecomputeOpts {
	return anomalyrepo.RecomputeOpts{
		TenantID:      tenantID,
		Location:      loc,
		FromDate:      from,
		ToDate:        to,
		ConfigVersion: 1,
		MinCount:      10,
		Dimensions:    dims(),
	}
}

func TestPG_RecomputeTotalSourceCivilDates(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	loc := shanghai(t)
	tenantID := freshTenant(t, pool)

	// 2026-08-09 23:30 Shanghai = 15:30 UTC same day → buckets on Aug 9.
	// 2026-08-10 01:00 Shanghai = Aug 9 17:00 UTC → bucket on Aug 10 (civil).
	insertFeedback(t, pool, tenantID, time.Date(2026, 8, 9, 23, 30, 0, 0, loc), "api", "", "", nil, "")
	insertFeedback(t, pool, tenantID, time.Date(2026, 8, 10, 1, 0, 0, 0, loc), "api", "", "", nil, "")
	insertFeedback(t, pool, tenantID, time.Date(2026, 8, 10, 9, 0, 0, 0, loc), "zendesk", "", "", nil, "")

	from := time.Date(2026, 8, 9, 0, 0, 0, 0, loc)
	to := time.Date(2026, 8, 10, 0, 0, 0, 0, loc)
	require.NoError(t, repo.RecomputeWindow(context.Background(), recomputeOpts(tenantID, loc, from, to)))

	n, ok := bucketCount(t, pool, tenantID, "2026-08-09", "total", "total")
	require.True(t, ok)
	require.EqualValues(t, 1, n)
	n, ok = bucketCount(t, pool, tenantID, "2026-08-10", "total", "total")
	require.True(t, ok)
	require.EqualValues(t, 2, n)
	n, ok = bucketCount(t, pool, tenantID, "2026-08-10", "source", "source:zendesk")
	require.True(t, ok)
	require.EqualValues(t, 1, n)
	n, ok = bucketCount(t, pool, tenantID, "2026-08-10", "source", "source:api")
	require.True(t, ok)
	require.EqualValues(t, 1, n)
}

func TestPG_RecomputeDimensionSingleMultiAndPendingExclusion(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	loc := shanghai(t)
	tenantID := freshTenant(t, pool)
	day := time.Date(2026, 8, 10, 12, 0, 0, 0, loc)

	insertFeedback(t, pool, tenantID, day, "api", "", `{"severity":"critical","labels":["a","b"]}`, nil, "")
	insertFeedback(t, pool, tenantID, day, "api", "", `{"severity":"critical"}`, nil, "")
	// Pending row: counts in total, not in dimensions.
	insertFeedback(t, pool, tenantID, day, "api", "", "", nil, "")

	from := time.Date(2026, 8, 10, 0, 0, 0, 0, loc)
	require.NoError(t, repo.RecomputeWindow(context.Background(), recomputeOpts(tenantID, loc, from, from)))

	n, ok := bucketCount(t, pool, tenantID, "2026-08-10", "total", "total")
	require.True(t, ok)
	require.EqualValues(t, 3, n)

	sevKey := anomalyrepo.DimensionSliceKey("severity", "critical")
	n, ok = bucketCount(t, pool, tenantID, "2026-08-10", "dimension", sevKey)
	require.True(t, ok)
	require.EqualValues(t, 2, n, "pending row must not count toward dimensions")

	// Multi expansion: one row contributes to both label buckets.
	n, ok = bucketCount(t, pool, tenantID, "2026-08-10", "dimension", anomalyrepo.DimensionSliceKey("labels", "a"))
	require.True(t, ok)
	require.EqualValues(t, 1, n)
	n, ok = bucketCount(t, pool, tenantID, "2026-08-10", "dimension", anomalyrepo.DimensionSliceKey("labels", "b"))
	require.True(t, ok)
	require.EqualValues(t, 1, n)
}

func TestPG_RecomputeClusterMinCount(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	loc := shanghai(t)
	tenantID := freshTenant(t, pool)
	day := time.Date(2026, 8, 10, 12, 0, 0, 0, loc)

	big := uuid.New()
	small := uuid.New()
	for i := 0; i < 12; i++ {
		insertFeedback(t, pool, tenantID, day.Add(time.Duration(i)*time.Minute), "api", "", "", &big, "Big cluster")
	}
	for i := 0; i < 3; i++ {
		insertFeedback(t, pool, tenantID, day.Add(time.Duration(i)*time.Minute), "api", "", "", &small, "Small cluster")
	}

	from := time.Date(2026, 8, 10, 0, 0, 0, 0, loc)
	require.NoError(t, repo.RecomputeWindow(context.Background(), recomputeOpts(tenantID, loc, from, from)))

	n, ok := bucketCount(t, pool, tenantID, "2026-08-10", "cluster", "cluster:"+big.String())
	require.True(t, ok)
	require.EqualValues(t, 12, n)
	_, ok = bucketCount(t, pool, tenantID, "2026-08-10", "cluster", "cluster:"+small.String())
	require.False(t, ok, "cluster below MinCount must not get a bucket")
}

func TestPG_RecomputeCohortJoin(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	loc := shanghai(t)
	tenantID := freshTenant(t, pool)
	ctx := context.Background()
	day := time.Date(2026, 8, 10, 12, 0, 0, 0, loc)

	var sourceID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO cohort_sources (tenant_id, provider, name) VALUES ($1,'amplitude','src')
		RETURNING id`, tenantID).Scan(&sourceID))
	var cohortID uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO cohorts (tenant_id, cohort_source_id, external_cohort_id, name)
		VALUES ($1,$2,'ext-1','Enterprise') RETURNING id`, tenantID, sourceID).Scan(&cohortID))
	_, err := pool.Exec(ctx, `
		INSERT INTO cohort_memberships (tenant_id, cohort_id, external_user_id)
		VALUES ($1,$2,'member-1')`, tenantID, cohortID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO cohort_memberships (tenant_id, cohort_id, external_user_id, left_at)
		VALUES ($1,$2,'member-gone',NOW())`, tenantID, cohortID)
	require.NoError(t, err)

	insertFeedback(t, pool, tenantID, day, "api", "member-1", "", nil, "")
	insertFeedback(t, pool, tenantID, day, "api", "member-gone", "", nil, "")
	insertFeedback(t, pool, tenantID, day, "api", "", "", nil, "")

	from := time.Date(2026, 8, 10, 0, 0, 0, 0, loc)
	require.NoError(t, repo.RecomputeWindow(context.Background(), recomputeOpts(tenantID, loc, from, from)))

	n, ok := bucketCount(t, pool, tenantID, "2026-08-10", "cohort", "cohort:"+cohortID.String())
	require.True(t, ok)
	require.EqualValues(t, 1, n, "only active membership counts")
}

func TestPG_RecomputeCustomConjunction(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	loc := shanghai(t)
	tenantID := freshTenant(t, pool)
	day := time.Date(2026, 8, 10, 12, 0, 0, 0, loc)

	insertFeedback(t, pool, tenantID, day, "api", "", `{"severity":"critical"}`, nil, "")
	insertFeedback(t, pool, tenantID, day, "zendesk", "", `{"severity":"critical"}`, nil, "")
	insertFeedback(t, pool, tenantID, day, "api", "", `{"severity":"low"}`, nil, "")

	sliceID := uuid.New()
	opts := recomputeOpts(tenantID, loc, time.Date(2026, 8, 10, 0, 0, 0, 0, loc), time.Date(2026, 8, 10, 0, 0, 0, 0, loc))
	opts.CustomSlices = []anomalyrepo.CustomSlice{{
		ID:      sliceID,
		Display: "api criticals",
		Conditions: []anomalyrepo.CustomCondition{
			{Field: "source", Values: []string{"api"}},
			{Field: "dimension", Name: "severity", Values: []string{"critical"}},
		},
	}}
	require.NoError(t, repo.RecomputeWindow(context.Background(), opts))

	n, ok := bucketCount(t, pool, tenantID, "2026-08-10", "custom", "custom:"+sliceID.String())
	require.True(t, ok)
	require.EqualValues(t, 1, n, "only the row matching BOTH conditions")
}

func TestPG_RecomputeIdempotentAndZeroing(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	loc := shanghai(t)
	tenantID := freshTenant(t, pool)
	ctx := context.Background()
	day := time.Date(2026, 8, 10, 12, 0, 0, 0, loc)

	id := insertFeedback(t, pool, tenantID, day, "api", "", "", nil, "")
	from := time.Date(2026, 8, 10, 0, 0, 0, 0, loc)
	opts := recomputeOpts(tenantID, loc, from, from)

	require.NoError(t, repo.RecomputeWindow(ctx, opts))
	require.NoError(t, repo.RecomputeWindow(ctx, opts)) // idempotent
	n, ok := bucketCount(t, pool, tenantID, "2026-08-10", "total", "total")
	require.True(t, ok)
	require.EqualValues(t, 1, n)

	// GDPR-style delete → recompute zeroes (removes) the bucket.
	_, err := pool.Exec(ctx, `DELETE FROM user_feedback WHERE id=$1`, id)
	require.NoError(t, err)
	require.NoError(t, repo.RecomputeWindow(ctx, opts))
	_, ok = bucketCount(t, pool, tenantID, "2026-08-10", "total", "total")
	require.False(t, ok, "vanished data must remove the bucket")
}

func TestPG_RecomputeSampleIDsCapped(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	loc := shanghai(t)
	tenantID := freshTenant(t, pool)
	day := time.Date(2026, 8, 10, 12, 0, 0, 0, loc)

	var last int64
	for i := 0; i < 7; i++ {
		last = insertFeedback(t, pool, tenantID, day.Add(time.Duration(i)*time.Minute), "api", "", "", nil, "")
	}
	from := time.Date(2026, 8, 10, 0, 0, 0, 0, loc)
	require.NoError(t, repo.RecomputeWindow(context.Background(), recomputeOpts(tenantID, loc, from, from)))

	var samples []int64
	require.NoError(t, pool.QueryRow(context.Background(), `
		SELECT sample_feedback_ids FROM feedback_volume_buckets
		WHERE tenant_id=$1 AND bucket_date='2026-08-10' AND slice_type='total'`, tenantID).
		Scan(&samples))
	require.Len(t, samples, 5)
	require.Equal(t, last, samples[0], "newest first")
}

func TestPG_BaselineCountsFillsZeros(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	loc := shanghai(t)
	tenantID := freshTenant(t, pool)
	ctx := context.Background()

	// Buckets on 3 of 8 same-weekday dates.
	for _, d := range []string{"2026-06-15", "2026-06-29", "2026-07-27"} {
		_, err := pool.Exec(ctx, `
			INSERT INTO feedback_volume_buckets (tenant_id, bucket_date, slice_type, slice_key, feedback_count)
			VALUES ($1,$2,'total','total',7)`, tenantID, d)
		require.NoError(t, err)
	}
	var dates []time.Time
	for week := 8; week >= 1; week-- {
		dates = append(dates, time.Date(2026, 8, 10, 0, 0, 0, 0, loc).AddDate(0, 0, -7*week))
	}
	counts, err := repo.BaselineCounts(ctx, tenantID, "total", "total", dates)
	require.NoError(t, err)
	require.Len(t, counts, 8)
	// 2026-06-15 is 8 weeks before 2026-08-10; 06-29 is 6; 07-27 is 2.
	require.EqualValues(t, 7, counts[0])
	require.EqualValues(t, 0, counts[1])
	require.EqualValues(t, 7, counts[2])
	require.EqualValues(t, 7, counts[6])
	require.EqualValues(t, 0, counts[7])
}

func TestPG_SlicesForDetectionIncludesVanished(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	loc := shanghai(t)
	tenantID := freshTenant(t, pool)
	ctx := context.Background()

	// Slice existed in the baseline weeks but not on detection day.
	_, err := pool.Exec(ctx, `
		INSERT INTO feedback_volume_buckets (tenant_id, bucket_date, slice_type, slice_key, slice_display, feedback_count)
		VALUES ($1,'2026-08-03','source','source:zendesk','Zendesk',9)`, tenantID)
	require.NoError(t, err)

	detect := time.Date(2026, 8, 10, 0, 0, 0, 0, loc)
	baseline := []time.Time{time.Date(2026, 8, 3, 0, 0, 0, 0, loc)}
	slices, err := repo.SlicesForDetection(ctx, tenantID, []string{"total", "source"}, detect, baseline)
	require.NoError(t, err)
	found := false
	for _, s := range slices {
		if s.Type == "source" && s.Key == "source:zendesk" {
			found = true
		}
	}
	require.True(t, found, "vanished slice must remain a drop candidate")
}

func TestPG_CleanupRetention(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	tenantID := freshTenant(t, pool)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO feedback_volume_buckets (tenant_id, bucket_date, slice_type, slice_key, feedback_count)
		VALUES ($1, CURRENT_DATE - 500, 'total','total',1),
		       ($1, CURRENT_DATE - 10, 'total','total',1)`, tenantID)
	require.NoError(t, err)

	require.NoError(t, repo.CleanupRetention(ctx, 400, 90))

	var n int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM feedback_volume_buckets WHERE tenant_id=$1`, tenantID).Scan(&n))
	require.Equal(t, 1, n, "only the recent bucket survives")
}

// TestPG_RecomputeDimensionKeepsAllDaysForTopValues is the regression test
// for the value-cap semantics: the per-dimension cap must bound DISTINCT
// VALUES, never (date, value) rows. A row-level LIMIT would keep only the
// highest-count day rows and punch zero-holes into lower-volume values'
// baselines across a multi-day window.
func TestPG_RecomputeDimensionKeepsAllDaysForTopValues(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	loc := shanghai(t)
	tenantID := freshTenant(t, pool)

	// 60 days × 2 severity values. "critical" dominates (3/day), "low" is
	// steady (2/day). 120 (date,value) pairs — far beyond a row LIMIT of 50.
	start := time.Date(2026, 6, 12, 12, 0, 0, 0, loc)
	for d := 0; d < 60; d++ {
		day := start.AddDate(0, 0, d)
		for i := 0; i < 3; i++ {
			insertFeedback(t, pool, tenantID, day.Add(time.Duration(i)*time.Minute), "api", "", `{"severity":"critical"}`, nil, "")
		}
		for i := 0; i < 2; i++ {
			insertFeedback(t, pool, tenantID, day.Add(time.Duration(i)*time.Minute), "api", "", `{"severity":"low"}`, nil, "")
		}
	}

	opts := recomputeOpts(tenantID, loc, start, start.AddDate(0, 0, 59))
	require.NoError(t, repo.RecomputeWindow(context.Background(), opts))

	// EVERY day must have a bucket for BOTH values.
	for _, value := range []string{"critical", "low"} {
		key := anomalyrepo.DimensionSliceKey("severity", value)
		var got int
		require.NoError(t, pool.QueryRow(context.Background(), `
			SELECT COUNT(*) FROM feedback_volume_buckets
			WHERE tenant_id=$1 AND slice_type='dimension' AND slice_key=$2`,
			tenantID, key).Scan(&got))
		require.Equal(t, 60, got,
			"value %q must keep a bucket for all 60 days (cap is on values, not rows)", value)
	}
}

// TestPG_ContributionScopedToDimensionSlice: attribution for a dimension
// slice must count only that slice's feedback, not the whole tenant.
func TestPG_ContributionScopedToDimensionSlice(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := anomalyrepo.New(pool)
	loc := shanghai(t)
	tenantID := freshTenant(t, pool)
	ctx := context.Background()
	day := time.Date(2026, 8, 10, 12, 0, 0, 0, loc)

	// critical spike comes 100% from zendesk; a big pile of api "low"
	// feedback would dominate an unscoped source breakdown.
	for i := 0; i < 10; i++ {
		insertFeedback(t, pool, tenantID, day.Add(time.Duration(i)*time.Minute), "zendesk", "", `{"severity":"critical"}`, nil, "")
	}
	for i := 0; i < 40; i++ {
		insertFeedback(t, pool, tenantID, day.Add(time.Duration(i)*time.Minute), "api", "", `{"severity":"low"}`, nil, "")
	}

	rows, err := repo.GroupCountsByAxis(ctx, tenantID, loc,
		[]anomalyrepo.CustomCondition{{Field: "dimension", Name: "severity", Values: []string{"critical"}}},
		anomalyrepo.GroupByAxis{Field: "source"}, day, nil)
	require.NoError(t, err)

	bySource := map[string]int64{}
	for _, row := range rows {
		bySource[row.Value] = row.Observed
	}
	require.EqualValues(t, 10, bySource["zendesk"], "scoped count must see only the slice")
	require.Zero(t, bySource["api"], "api feedback is outside the severity=critical slice")
}
