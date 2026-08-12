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

// eventVals builds the 19-column value list scanEvent expects.
func eventVals(id uuid.UUID, day time.Time) []any {
	return []any{
		id, "t1", SliceTotal, "total", "All feedback", "spike",
		day, day, int64(31),
		12.0, 6.0, 21.0, 3.8,
		"open", nil, `{"sample_ids":[1]}`,
		day, day, nil,
	}
}

func newTxRepo(tx *fakeTx) (*Repo, *fakePool) {
	pool := ptrext.Of(fakePool{tx: tx})
	return ptrext.Of(Repo{pool: pool}), pool
}

func TestUpsertHitOngoingViaFakeTx(t *testing.T) {
	id := uuid.New()
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	tx := ptrext.Of(fakeTx{rowRoutes: []txRowRoute{
		{"UPDATE anomaly_events SET", scanFromVals(eventVals(id, day))},
	}})
	r, _ := newTxRepo(tx)

	ev, isNew, err := r.UpsertHit(context.Background(), HitInput{
		TenantID: "t1", SliceType: SliceTotal, SliceKey: "total",
		Direction: "spike", BucketDate: day, Observed: 31,
	})
	if err != nil || isNew {
		t.Fatalf("ongoing hit: err=%v isNew=%v", err, isNew)
	}
	if ev.ID != id || !tx.committed {
		t.Fatalf("ongoing hit must return the updated row and commit: %+v", ev)
	}
}

func TestUpsertHitInsertViaFakeTx(t *testing.T) {
	id := uuid.New()
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	// No UPDATE route: falls to ErrNoRows, then the INSERT route answers.
	tx := ptrext.Of(fakeTx{rowRoutes: []txRowRoute{
		{"INSERT INTO anomaly_events", scanFromVals(eventVals(id, day))},
	}})
	r, _ := newTxRepo(tx)

	ev, isNew, err := r.UpsertHit(context.Background(), HitInput{
		TenantID: "t1", SliceType: SliceTotal, SliceKey: "total",
		Direction: "spike", BucketDate: day, Observed: 31,
	})
	if err != nil || !isNew {
		t.Fatalf("fresh hit: err=%v isNew=%v", err, isNew)
	}
	if ev.ID != id || !tx.committed {
		t.Fatalf("fresh hit must return the inserted row and commit: %+v", ev)
	}
}

func TestUpsertHitUpdateScanFailure(t *testing.T) {
	tx := ptrext.Of(fakeTx{rowRoutes: []txRowRoute{
		{"UPDATE anomaly_events SET", func(...any) error { return errFakeTx }},
	}})
	r, _ := newTxRepo(tx)
	_, _, err := r.UpsertHit(context.Background(), HitInput{TenantID: "t1"})
	if err == nil || !tx.rolledBack {
		t.Fatalf("non-ErrNoRows update failure must error and roll back, err=%v", err)
	}
}

func TestUpsertHitInsertScanFailure(t *testing.T) {
	tx := ptrext.Of(fakeTx{rowRoutes: []txRowRoute{
		{"INSERT INTO anomaly_events", func(...any) error { return errFakeTx }},
	}})
	r, _ := newTxRepo(tx)
	_, _, err := r.UpsertHit(context.Background(), HitInput{TenantID: "t1"})
	if err == nil {
		t.Fatal("insert failure must surface")
	}
}

func recomputeOptsForTx(day time.Time) RecomputeOpts {
	return RecomputeOpts{
		TenantID: "t1", Location: time.UTC,
		FromDate: day, ToDate: day, ConfigVersion: 1, MinCount: 10,
	}
}

func nowRoute(day time.Time) txRowRoute {
	return txRowRoute{"SELECT NOW()", scanFromVals([]any{day})}
}

func TestRecomputeWindowViaFakeTx(t *testing.T) {
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	tx := ptrext.Of(fakeTx{
		rowRoutes: []txRowRoute{nowRoute(day)},
		routes: []routeEntry{
			// Unsorted on purpose: b-key first, a-key second.
			{"GROUP BY d, f.source", [][]any{
				{day, "web", nil, int64(9), []int64{2}},
				{day, "api", nil, int64(7), []int64{1}},
			}},
		},
	})
	r, _ := newTxRepo(tx)

	if err := r.RecomputeWindow(context.Background(), recomputeOptsForTx(day)); err != nil {
		t.Fatalf("RecomputeWindow: %v", err)
	}
	if !tx.committed {
		t.Fatal("recompute must commit")
	}
	// Upserts (sorted by key: api before web) then the zeroing DELETE.
	var upserts []string
	var sawDelete bool
	for _, e := range tx.execs {
		switch {
		case strings.Contains(e, "INSERT INTO feedback_volume_buckets"):
			upserts = append(upserts, e)
		case strings.Contains(e, "DELETE FROM feedback_volume_buckets"):
			sawDelete = true
		}
	}
	if len(upserts) != 2 || !sawDelete {
		t.Fatalf("want 2 upserts + zeroing delete, got %d upserts sawDelete=%v", len(upserts), sawDelete)
	}
}

func TestRecomputeWindowClockFailure(t *testing.T) {
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	tx := ptrext.Of(fakeTx{rowRoutes: []txRowRoute{
		{"SELECT NOW()", func(...any) error { return errFakeTx }},
	}})
	r, _ := newTxRepo(tx)
	if err := r.RecomputeWindow(context.Background(), recomputeOptsForTx(day)); err == nil {
		t.Fatal("clock failure must surface")
	}
	if !tx.rolledBack {
		t.Fatal("failed recompute must roll back")
	}
}

func TestRecomputeWindowUpsertFailure(t *testing.T) {
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	tx := ptrext.Of(fakeTx{
		rowRoutes: []txRowRoute{nowRoute(day)},
		routes: []routeEntry{
			{"GROUP BY d, f.source", [][]any{{day, "api", nil, int64(7), []int64{1}}}},
		},
		execErrOn: "INSERT INTO feedback_volume_buckets",
	})
	r, _ := newTxRepo(tx)
	if err := r.RecomputeWindow(context.Background(), recomputeOptsForTx(day)); err == nil {
		t.Fatal("upsert failure must surface")
	}
}

func TestRecomputeWindowZeroingFailure(t *testing.T) {
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	tx := ptrext.Of(fakeTx{
		rowRoutes: []txRowRoute{nowRoute(day)},
		execErrOn: "DELETE FROM feedback_volume_buckets",
	})
	r, _ := newTxRepo(tx)
	if err := r.RecomputeWindow(context.Background(), recomputeOptsForTx(day)); err == nil {
		t.Fatal("zeroing failure must surface")
	}
}

func TestRecomputeWindowCommitFailure(t *testing.T) {
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	tx := ptrext.Of(fakeTx{
		rowRoutes: []txRowRoute{nowRoute(day)},
		commitErr: errFakeTx,
	})
	r, _ := newTxRepo(tx)
	if err := r.RecomputeWindow(context.Background(), recomputeOptsForTx(day)); err == nil {
		t.Fatal("commit failure must surface")
	}
}

func TestRecomputeWindowAggregateFailure(t *testing.T) {
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	tx := ptrext.Of(fakeTx{
		rowRoutes:  []txRowRoute{nowRoute(day)},
		queryErrOn: "GROUP BY d, f.source",
	})
	r, _ := newTxRepo(tx)
	if err := r.RecomputeWindow(context.Background(), recomputeOptsForTx(day)); err == nil {
		t.Fatal("aggregate failure must surface")
	}
}

func TestReplaceCustomSlicesViaFakeTx(t *testing.T) {
	tx := ptrext.Of(fakeTx{})
	r, _ := newTxRepo(tx)
	err := r.ReplaceCustomSlices(context.Background(), "t1", []StoredCustomSlice{
		{ID: uuid.New(), Name: "x", DefinitionJSON: "{}", Enabled: true},
	})
	if err != nil || !tx.committed {
		t.Fatalf("replace must delete+insert+commit: err=%v", err)
	}
	if len(tx.execs) != 2 {
		t.Fatalf("want DELETE then INSERT, got %v", tx.execs)
	}
}

func TestReplaceCustomSlicesDeleteFailure(t *testing.T) {
	tx := ptrext.Of(fakeTx{execErrOn: "DELETE FROM tenant_anomaly_custom_slices"})
	r, _ := newTxRepo(tx)
	if err := r.ReplaceCustomSlices(context.Background(), "t1", nil); err == nil {
		t.Fatal("delete failure must surface")
	}
	if !tx.rolledBack {
		t.Fatal("failed replace must roll back")
	}
}

func TestReplaceCustomSlicesInsertFailure(t *testing.T) {
	tx := ptrext.Of(fakeTx{execErrOn: "INSERT INTO tenant_anomaly_custom_slices"})
	r, _ := newTxRepo(tx)
	err := r.ReplaceCustomSlices(context.Background(), "t1", []StoredCustomSlice{
		{ID: uuid.New(), Name: "x", DefinitionJSON: "{}"},
	})
	if err == nil {
		t.Fatal("insert failure must surface")
	}
}

func TestGroupCountsByAxisViaFakePool(t *testing.T) {
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	base := day.AddDate(0, 0, -7)
	pool := ptrext.Of(fakePool{queryRoutes: []struct {
		fragment string
		rows     [][]any
	}{
		// Same route answers observed date and baseline date alike; the
		// per-value split is what we assert.
		{"GROUP BY v HAVING", [][]any{
			{"api", int64(9)},
			{"web", int64(4)},
		}},
	}})
	r := ptrext.Of(Repo{pool: pool})

	out, err := r.GroupCountsByAxis(context.Background(), "t1", time.UTC,
		nil, GroupByAxis{Field: "source"}, day, []time.Time{base})
	if err != nil {
		t.Fatalf("GroupCountsByAxis: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 grouped values, got %d", len(out))
	}
	for _, row := range out {
		if row.Observed == 0 || len(row.BaselineCounts) != 1 || row.BaselineCounts[0] == 0 {
			t.Fatalf("observed and baseline must both fill: %+v", row)
		}
	}
}

func TestGroupCountsByAxisDimensionExpr(t *testing.T) {
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	pool := ptrext.Of(fakePool{})
	r := ptrext.Of(Repo{pool: pool})
	out, err := r.GroupCountsByAxis(context.Background(), "t1", time.UTC,
		[]CustomCondition{{Field: "source", Values: []string{"api"}}},
		GroupByAxis{Field: "dimension", Name: "severity"}, day, nil)
	if err != nil || len(out) != 0 {
		t.Fatalf("empty grouping: err=%v out=%v", err, out)
	}
}

func TestGroupCountsScanFailure(t *testing.T) {
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	pool := ptrext.Of(fakePool{
		queryRoutes: []struct {
			fragment string
			rows     [][]any
		}{{"GROUP BY v HAVING", [][]any{{"api", int64(1)}}}},
		scanErrOn: "GROUP BY v HAVING",
	})
	r := ptrext.Of(Repo{pool: pool})
	if _, err := r.GroupCountsByAxis(context.Background(), "t1", time.UTC,
		nil, GroupByAxis{Field: "source"}, day, nil); err == nil {
		t.Fatal("scan failure must surface")
	}
}

// aggregateWindow row-level failures: Err() after iteration and Scan().
func TestAggregateWindowRowsErr(t *testing.T) {
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	tx := ptrext.Of(fakeTx{
		routes:    []routeEntry{{"NULL::text, NULL::text", nil}},
		rowsErrOn: "NULL::text, NULL::text",
	})
	repo := ptrext.Of(Repo{})
	if _, err := repo.aggregateWindow(context.Background(), tx,
		recomputeOptsForTx(day), day, day.AddDate(0, 0, 1)); err == nil {
		t.Fatal("rows.Err must surface")
	}
}

func TestAggregateWindowScanErr(t *testing.T) {
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	tx := ptrext.Of(fakeTx{
		routes:    []routeEntry{{"NULL::text, NULL::text", [][]any{{day, nil, nil, int64(1), []int64{1}}}}},
		scanErrOn: "NULL::text, NULL::text",
	})
	repo := ptrext.Of(Repo{})
	if _, err := repo.aggregateWindow(context.Background(), tx,
		recomputeOptsForTx(day), day, day.AddDate(0, 0, 1)); err == nil {
		t.Fatal("scan failure must surface")
	}
}

// Per-family query failures walk every early-return in the aggregate path.
func TestAggregateFamilyQueryFailures(t *testing.T) {
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	opts := recomputeOptsForTx(day)
	opts.Dimensions = domain.DimensionSet{
		{Name: "severity", Kind: domain.DimSingle, Taxonomy: []domain.Taxonomy{{Value: "critical"}}},
		{Name: "labels", Kind: domain.DimMulti, Taxonomy: []domain.Taxonomy{{Value: "a"}}},
	}
	opts.CustomSlices = []CustomSlice{{
		ID: uuid.New(), Display: "cs",
		Conditions: []CustomCondition{{Field: "source", Values: []string{"api"}}},
	}}
	repo := ptrext.Of(Repo{})
	for _, fragment := range []string{
		"GROUP BY d, f.source",
		"f.cluster_id IS NOT NULL",
		"cohort_memberships",
		"->> $5",
		"jsonb_array_elements_text",
		"f.source = ANY(",
	} {
		tx := ptrext.Of(fakeTx{queryErrOn: fragment})
		if _, err := repo.aggregateWindow(context.Background(), tx,
			opts, day, day.AddDate(0, 0, 1)); err == nil {
			t.Fatalf("query failure on %q must surface", fragment)
		}
	}
}
