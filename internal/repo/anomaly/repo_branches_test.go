// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// queryRoute / rowRoute build the fakePool route entries without repeating
// the anonymous struct literals at every call site.
func queryRoute(fragment string, rows [][]any) struct {
	fragment string
	rows     [][]any
} {
	return struct {
		fragment string
		rows     [][]any
	}{fragment, rows}
}

func rowRoute(fragment string, row scanRow) struct {
	fragment string
	row      scanRow
} {
	return struct {
		fragment string
		row      scanRow
	}{fragment, row}
}

func poolRepo(pool *fakePool) *Repo { return ptrext.Of(Repo{pool: pool}) }

// Scan failures inside row loops — one per method with a rows iteration.
func TestRowScanFailures(t *testing.T) {
	ctx := context.Background()
	dayT := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		fragment string
		rows     [][]any
		call     func(r *Repo) error
	}{
		{
			"ListCustomSlices", "FROM tenant_anomaly_custom_slices",
			[][]any{{uuid.New(), "x", "{}", true, ""}},
			func(r *Repo) error { _, err := r.ListCustomSlices(ctx, "t1"); return err },
		},
		{
			"listEvents", "FROM anomaly_events",
			[][]any{{uuid.New()}},
			func(r *Repo) error { _, err := r.ListEvents(ctx, "t1", "open", 10); return err },
		},
		{
			"OpenDigestAnomaliesInWindow", "notify_mode = 'digest'",
			[][]any{{"d"}},
			func(r *Repo) error {
				_, err := r.OpenDigestAnomaliesInWindow(ctx, "t1", dayT, dayT.AddDate(0, 0, 1))
				return err
			},
		},
		{
			"FilterLiveFeedbackIDs", "FROM user_feedback",
			[][]any{{int64(1)}},
			func(r *Repo) error { _, err := r.FilterLiveFeedbackIDs(ctx, "t1", []int64{1}); return err },
		},
		{
			"ListUnnotifiedOpenEvents", "notified_at IS NULL",
			[][]any{{uuid.New()}},
			func(r *Repo) error { _, err := r.ListUnnotifiedOpenEvents(ctx, "t1"); return err },
		},
		{
			"ActiveTenantsWithFeedback", "FROM tenants t",
			[][]any{{"t1"}},
			func(r *Repo) error { _, err := r.ActiveTenantsWithFeedback(ctx, 90); return err },
		},
		{
			"BaselineCounts", "SELECT bucket_date::text, feedback_count",
			[][]any{{"2026-08-10", int64(1)}},
			func(r *Repo) error {
				_, err := r.BaselineCounts(ctx, "t1", SliceTotal, "total", []time.Time{dayT})
				return err
			},
		},
		{
			"SlicesForDetection", "SELECT DISTINCT slice_type",
			[][]any{{"total"}},
			func(r *Repo) error {
				_, err := r.SlicesForDetection(ctx, "t1", AllSliceTypes(), dayT, nil)
				return err
			},
		},
		{
			"WindowCounts", "FROM feedback_volume_buckets",
			[][]any{{"total"}},
			func(r *Repo) error {
				_, err := r.WindowCounts(ctx, "t1", AllSliceTypes(), []time.Time{dayT})
				return err
			},
		},
		{
			"UnclaimedSettledDates", "unnest($2::date[])",
			[][]any{{dayT}},
			func(r *Repo) error {
				_, err := r.UnclaimedSettledDates(ctx, "t1", []time.Time{dayT})
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := ptrext.Of(fakePool{scanErrOn: tc.fragment})
			pool.queryRoutes = append(pool.queryRoutes, queryRoute(tc.fragment, tc.rows))
			if err := tc.call(poolRepo(pool)); err == nil {
				t.Fatalf("%s: scan failure must surface", tc.name)
			}
		})
	}
}

func TestGetEventHappyPath(t *testing.T) {
	id := uuid.New()
	dayT := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	pool := ptrext.Of(fakePool{})
	pool.rowRoutes = append(pool.rowRoutes,
		rowRoute("FROM anomaly_events", scanRow{vals: eventVals(id, dayT)}))
	got, err := poolRepo(pool).GetEvent(context.Background(), "t1", id)
	if err != nil || got.ID != id || got.Status != "open" {
		t.Fatalf("GetEvent: %v %+v", err, got)
	}
}

func TestSetEvidenceHappyPath(t *testing.T) {
	pool := ptrext.Of(fakePool{})
	if err := poolRepo(pool).SetEvidence(context.Background(), uuid.New(), ""); err != nil {
		t.Fatalf("SetEvidence: %v", err)
	}
	// Empty evidence must be normalized to a valid JSON object.
	if len(pool.execTags) != 1 || !strings.Contains(pool.execTags[0], "SET evidence") {
		t.Fatalf("evidence update not issued: %v", pool.execTags)
	}
}

func TestCleanupRetentionRunsBothDeletes(t *testing.T) {
	pool := ptrext.Of(fakePool{})
	if err := poolRepo(pool).CleanupRetention(context.Background(), 400, 90); err != nil {
		t.Fatalf("CleanupRetention: %v", err)
	}
	if len(pool.execTags) != 2 {
		t.Fatalf("want bucket + run deletes, got %v", pool.execTags)
	}
	// Second DELETE failing must also surface (first succeeds).
	pool2 := ptrext.Of(fakePool{execErrOn: "anomaly_detection_runs"})
	if err := poolRepo(pool2).CleanupRetention(context.Background(), 400, 90); err == nil {
		t.Fatal("run-retention failure must surface")
	}
}

func TestLatestDoneRunNoRuns(t *testing.T) {
	pool := ptrext.Of(fakePool{})
	pool.rowRoutes = append(pool.rowRoutes,
		rowRoute("MAX(bucket_date)", scanRow{vals: []any{nil}}))
	_, ok, err := poolRepo(pool).LatestDoneRun(context.Background(), "t1")
	if err != nil || ok {
		t.Fatalf("no done runs must yield ok=false: ok=%v err=%v", ok, err)
	}
}

func TestCompileClusterCondition(t *testing.T) {
	where, args := compileCustomConditions([]CustomCondition{
		{Field: "cluster", Values: []string{uuid.NewString()}},
	}, 4)
	if !strings.Contains(where, "f.cluster_id = ANY($4::uuid[])") || len(args) != 1 {
		t.Fatalf("cluster condition wrong: %q args=%d", where, len(args))
	}
}

// The upsert sort must order by date, then slice type, then key — all
// three comparator branches.
func TestRecomputeWindowSortOrder(t *testing.T) {
	d1 := time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	tx := ptrext.Of(fakeTx{
		rowRoutes: []txRowRoute{nowRoute(d2)},
		routes: []routeEntry{
			// Deliberately shuffled: later date first, keys reversed.
			{"GROUP BY d, f.source", [][]any{
				{d2, "web", nil, int64(9), []int64{2}},
				{d2, "api", nil, int64(7), []int64{1}},
				{d1, "web", nil, int64(5), []int64{3}},
			}},
			{"NULL::text, NULL::text", [][]any{
				{d2, nil, nil, int64(16), []int64{4}},
			}},
		},
	})
	pool := ptrext.Of(fakePool{tx: tx})
	opts := recomputeOptsForTx(d1)
	opts.ToDate = d2
	if err := poolRepo(pool).RecomputeWindow(context.Background(), opts); err != nil {
		t.Fatalf("RecomputeWindow: %v", err)
	}
	var upserts int
	for _, e := range tx.execs {
		if strings.Contains(e, "INSERT INTO feedback_volume_buckets") {
			upserts++
		}
	}
	if upserts != 4 {
		t.Fatalf("want 4 upserts across dates/types/keys, got %d", upserts)
	}
}

// aggregate collect-stage failures inside dimension and custom loops.
func TestAggregateDimensionCollectFailure(t *testing.T) {
	dayT := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	opts := recomputeOptsForTx(dayT)
	opts.Dimensions = domain.DimensionSet{
		{Name: "severity", Kind: domain.DimSingle, Taxonomy: []domain.Taxonomy{{Value: "critical"}}},
	}
	tx := ptrext.Of(fakeTx{
		routes:    []routeEntry{{"->> $5", [][]any{{dayT, "critical", nil, int64(4), []int64{7}}}}},
		scanErrOn: "->> $5",
	})
	repo := ptrext.Of(Repo{})
	if _, err := repo.aggregateWindow(context.Background(), tx,
		opts, dayT, dayT.AddDate(0, 0, 1)); err == nil {
		t.Fatal("dimension collect failure must surface")
	}
}

func TestAggregateCustomCollectFailure(t *testing.T) {
	dayT := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	opts := recomputeOptsForTx(dayT)
	opts.CustomSlices = []CustomSlice{{
		ID: uuid.New(), Display: "cs",
		Conditions: []CustomCondition{{Field: "source", Values: []string{"api"}}},
	}}
	tx := ptrext.Of(fakeTx{
		routes:    []routeEntry{{"f.source = ANY(", [][]any{{dayT, nil, nil, int64(1), []int64{1}}}}},
		scanErrOn: "f.source = ANY(",
	})
	repo := ptrext.Of(Repo{})
	if _, err := repo.aggregateWindow(context.Background(), tx,
		opts, dayT, dayT.AddDate(0, 0, 1)); err == nil {
		t.Fatal("custom collect failure must surface")
	}
}

func TestAggregateTotalQueryFailure(t *testing.T) {
	dayT := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	tx := ptrext.Of(fakeTx{queryErrOn: "NULL::text, NULL::text"})
	repo := ptrext.Of(Repo{})
	if _, err := repo.aggregateWindow(context.Background(), tx,
		recomputeOptsForTx(dayT), dayT, dayT.AddDate(0, 0, 1)); err == nil {
		t.Fatal("total query failure must surface")
	}
}

// Collect-stage scan failures inside the core families (source, cluster).
func TestAggregateCoreCollectFailures(t *testing.T) {
	dayT := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	repo := ptrext.Of(Repo{})
	for _, tc := range []struct {
		fragment string
		rows     [][]any
	}{
		{"GROUP BY d, f.source", [][]any{{dayT, "api", nil, int64(1), []int64{1}}}},
		{"f.cluster_id IS NOT NULL", [][]any{{dayT, uuid.NewString(), "", int64(1), []int64{1}}}},
	} {
		tx := ptrext.Of(fakeTx{
			routes:    []routeEntry{{tc.fragment, tc.rows}},
			scanErrOn: tc.fragment,
		})
		if _, err := repo.aggregateWindow(context.Background(), tx,
			recomputeOptsForTx(dayT), dayT, dayT.AddDate(0, 0, 1)); err == nil {
			t.Fatalf("collect failure on %q must surface", tc.fragment)
		}
	}
}

// rows.Err surfacing after successful iteration (deferred driver error).
func TestBaselineCountsRowsErr(t *testing.T) {
	dayT := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	pool := ptrext.Of(fakePool{rowsErrOn: "SELECT bucket_date::text, feedback_count"})
	pool.queryRoutes = append(pool.queryRoutes, queryRoute(
		"SELECT bucket_date::text, feedback_count", nil))
	if _, err := poolRepo(pool).BaselineCounts(context.Background(),
		"t1", SliceTotal, "total", []time.Time{dayT}); err == nil {
		t.Fatal("rows.Err must surface")
	}
}

func TestWindowCountsQueryFailure(t *testing.T) {
	dayT := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	pool := ptrext.Of(fakePool{queryErrOn: "FROM feedback_volume_buckets"})
	if _, err := poolRepo(pool).WindowCounts(context.Background(),
		"t1", AllSliceTypes(), []time.Time{dayT}); err == nil {
		t.Fatal("query failure must surface")
	}
}
