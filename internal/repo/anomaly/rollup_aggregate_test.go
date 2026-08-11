// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// fakeRows implements pgx.Rows over a fixed (date, v1, v2, count, samples)
// row set — the shape every aggregate family query returns.
type fakeRows struct {
	rows [][]any
	idx  int
	err  error
}

func (f *fakeRows) Close()                                       {}
func (f *fakeRows) Err() error                                   { return f.err }
func (f *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeRows) Next() bool {
	f.idx++
	return f.idx <= len(f.rows)
}

func (f *fakeRows) Scan(dest ...any) error {
	row := f.rows[f.idx-1]
	// (date *time.Time, v1 **string, v2 **string, count *int64, samples *[]int64)
	ptrext.Indirect(dest[0].(*time.Time)) // touch to keep shape honest
	*dest[0].(*time.Time) = row[0].(time.Time)
	assignOptString(dest[1], row[1])
	assignOptString(dest[2], row[2])
	*dest[3].(*int64) = row[3].(int64)
	*dest[4].(*[]int64) = row[4].([]int64)
	return nil
}

func assignOptString(dest, val any) {
	p := dest.(**string)
	if val == nil {
		*p = nil
		return
	}
	s := val.(string)
	*p = ptrext.Of(s)
}

func (f *fakeRows) Values() ([]any, error) { return nil, nil }
func (f *fakeRows) RawValues() [][]byte    { return nil }
func (f *fakeRows) Conn() *pgx.Conn        { return nil }

// routeEntry pairs a SQL fragment with its canned rows; entries are matched
// in order so more specific fragments win over generic ones.
type routeEntry struct {
	fragment string
	rows     [][]any
}

// fakeTx implements the pgx.Tx query surface, routing each aggregate query
// (recognized by an ordered SQL-fragment list) to a fresh row iterator.
type fakeTx struct {
	routes  []routeEntry
	queries []string
}

func (f *fakeTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	f.queries = append(f.queries, sql)
	for _, entry := range f.routes {
		if strings.Contains(sql, entry.fragment) {
			return ptrext.Of(fakeRows{rows: entry.rows}), nil
		}
	}
	return ptrext.Of(fakeRows{}), nil
}

// Unused pgx.Tx surface (aggregateWindow only calls Query).
func (f *fakeTx) Begin(context.Context) (pgx.Tx, error) { return nil, nil }
func (f *fakeTx) Commit(context.Context) error          { return nil }
func (f *fakeTx) Rollback(context.Context) error        { return nil }
func (f *fakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (f *fakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (f *fakeTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }

func (f *fakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (f *fakeTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (f *fakeTx) QueryRow(context.Context, string, ...any) pgx.Row { return nil }
func (f *fakeTx) Conn() *pgx.Conn                                  { return nil }

func TestAggregateWindowCollectsAllFamilies(t *testing.T) {
	day := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	clusterID := uuid.NewString()
	cohortID := uuid.NewString()

	tx := ptrext.Of(fakeTx{routes: []routeEntry{
		// Ordered: specific fragments before the generic total shape (the
		// custom-slice query shares total's NULL,NULL select but carries the
		// compiled condition).
		{"f.source = ANY(", [][]any{
			{day, nil, nil, int64(1), []int64{10}},
		}},
		{"GROUP BY d, f.source", [][]any{
			{day, "zendesk", nil, int64(7), []int64{9}},
		}},
		{"f.cluster_id IS NOT NULL", [][]any{
			{day, clusterID, "Login cluster", int64(11), []int64{4}},
			{day, clusterID + "x", "", int64(10), []int64{5}}, // empty label → id display
		}},
		{"cohort_memberships", [][]any{
			{day, cohortID, "Enterprise", int64(3), []int64{6}},
		}},
		{"jsonb_array_elements_text", [][]any{ // multi dimension
			{day, "bug", nil, int64(2), []int64{8}},
		}},
		{"->> $5", [][]any{ // single dimension
			{day, "critical", nil, int64(4), []int64{7}},
		}},
		{"NULL::text, NULL::text", [][]any{ // total
			{day, nil, nil, int64(12), []int64{1, 2, 3, 4, 5, 6, 7}}, // >5 samples: capped
		}},
	}})

	repo := ptrext.Of(Repo{}) // aggregateWindow never touches the pool
	opts := RecomputeOpts{
		TenantID: "t1", Location: time.UTC,
		FromDate: day, ToDate: day, ConfigVersion: 1, MinCount: 10,
		Dimensions: domain.DimensionSet{
			{Name: "severity", Kind: domain.DimSingle, Taxonomy: []domain.Taxonomy{{Value: "critical"}}},
			{Name: "labels", Kind: domain.DimMulti, Taxonomy: []domain.Taxonomy{{Value: "bug"}}},
			{Name: "freeform", Kind: domain.DimMulti}, // no taxonomy: skipped
		},
		CustomSlices: []CustomSlice{{
			ID: uuid.New(), Display: "api criticals",
			Conditions: []CustomCondition{{Field: "source", Values: []string{"api"}}},
		}},
	}

	rows, err := repo.aggregateWindow(context.Background(),
		tx, opts, day, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("aggregateWindow: %v", err)
	}

	byKey := map[string]bucketRow{}
	for _, r := range rows {
		byKey[r.stype+"/"+r.key] = r
	}
	assertCoreBuckets(t, byKey, clusterID, cohortID)
	assertDimensionAndCustomBuckets(t, byKey)
	assertTaxonomyGate(t, tx.queries)
}

// assertCoreBuckets checks total/source/cluster/cohort collection.
func assertCoreBuckets(t *testing.T, byKey map[string]bucketRow, clusterID, cohortID string) {
	t.Helper()
	total, ok := byKey["total/total"]
	if !ok || total.count != 12 {
		t.Fatalf("total bucket wrong: %+v", byKey)
	}
	if len(total.samples) != 5 {
		t.Fatalf("samples must cap at 5, got %d", len(total.samples))
	}
	if src := byKey["source/source:zendesk"]; src.display != "zendesk" || src.count != 7 {
		t.Fatalf("source bucket wrong: %+v", src)
	}
	if cl := byKey["cluster/cluster:"+clusterID]; cl.display != "Login cluster" {
		t.Fatalf("cluster display must use label: %+v", cl)
	}
	if cl2 := byKey["cluster/cluster:"+clusterID+"x"]; cl2.display != clusterID+"x" {
		t.Fatalf("empty label must fall back to id: %+v", cl2)
	}
	if co := byKey["cohort/cohort:"+cohortID]; co.display != "Enterprise" || co.count != 3 {
		t.Fatalf("cohort bucket wrong: %+v", co)
	}
}

// assertDimensionAndCustomBuckets checks dimension hashing and custom slices.
func assertDimensionAndCustomBuckets(t *testing.T, byKey map[string]bucketRow) {
	t.Helper()
	sevKey := "dimension/" + DimensionSliceKey("severity", "critical")
	if dim := byKey[sevKey]; dim.display != "severity=critical" || dim.count != 4 {
		t.Fatalf("severity bucket wrong: %+v (keys=%v)", dim, keysOf(byKey))
	}
	labKey := "dimension/" + DimensionSliceKey("labels", "bug")
	if lab := byKey[labKey]; lab.count != 2 {
		t.Fatalf("labels bucket wrong: %+v", lab)
	}
	foundCustom := false
	for k, r := range byKey {
		if strings.HasPrefix(k, "custom/custom:") {
			foundCustom = true
			if r.display != "api criticals" || r.count != 1 {
				t.Fatalf("custom bucket wrong: %+v", r)
			}
		}
	}
	if !foundCustom {
		t.Fatalf("custom bucket missing: %v", keysOf(byKey))
	}
}

// assertTaxonomyGate: the freeform (no-taxonomy) dimension must not query —
// exactly two dimension queries (severity single + labels multi).
func assertTaxonomyGate(t *testing.T, queries []string) {
	t.Helper()
	dimQueries := 0
	for _, q := range queries {
		if strings.Contains(q, "enriched_attrs ->> $5") || strings.Contains(q, "jsonb_array_elements_text") {
			dimQueries++
		}
	}
	if dimQueries != 2 {
		t.Fatalf("want 2 dimension queries (taxonomy-bounded only), got %d", dimQueries)
	}
}

func keysOf(m map[string]bucketRow) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
