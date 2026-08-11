// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// scanRow implements pgx.Row over a fixed value list; assign copies each
// canned value into the scan destination via the same shapes the repo uses.
type scanRow struct {
	vals []any
	err  error
}

func (r scanRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		if i >= len(r.vals) {
			break
		}
		assignVal(d, r.vals[i])
	}
	return nil
}

func assignVal(dest, val any) {
	target := reflect.ValueOf(dest).Elem()
	if val == nil {
		target.Set(reflect.Zero(target.Type()))
		return
	}
	source := reflect.ValueOf(val)
	if source.Type().AssignableTo(target.Type()) {
		target.Set(source)
		return
	}
	if source.Type().ConvertibleTo(target.Type()) {
		target.Set(source.Convert(target.Type()))
		return
	}
	// Optional columns arrive as values but scan into pointers.
	if target.Kind() == reflect.Pointer && source.Type().AssignableTo(target.Type().Elem()) {
		box := reflect.New(target.Type().Elem())
		box.Elem().Set(source)
		target.Set(box)
	}
}

// listRows implements pgx.Rows over canned scanRow value lists.
type listRows struct {
	rows [][]any
	idx  int
}

func (l *listRows) Close()                                       {}
func (l *listRows) Err() error                                   { return nil }
func (l *listRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (l *listRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (l *listRows) Next() bool {
	l.idx++
	return l.idx <= len(l.rows)
}

func (l *listRows) Scan(dest ...any) error {
	return scanRow{vals: l.rows[l.idx-1]}.Scan(dest...)
}
func (l *listRows) Values() ([]any, error) { return nil, nil }
func (l *listRows) RawValues() [][]byte    { return nil }
func (l *listRows) Conn() *pgx.Conn        { return nil }

// fakePool routes Query/QueryRow/Exec by SQL fragment (ordered).
type fakePool struct {
	queryRoutes []struct {
		fragment string
		rows     [][]any
	}
	rowRoutes []struct {
		fragment string
		row      scanRow
	}
	execTags []string
}

func (f *fakePool) Begin(context.Context) (pgx.Tx, error) { return nil, errBoomPool }

func (f *fakePool) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	for _, r := range f.queryRoutes {
		if strings.Contains(sql, r.fragment) {
			return ptrext.Of(listRows{rows: r.rows}), nil
		}
	}
	return ptrext.Of(listRows{}), nil
}

func (f *fakePool) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	for _, r := range f.rowRoutes {
		if strings.Contains(sql, r.fragment) {
			return r.row
		}
	}
	return scanRow{err: pgx.ErrNoRows}
}

func (f *fakePool) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.execTags = append(f.execTags, sql)
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

var errBoomPool = pgx.ErrTxClosed

func day(s string) time.Time {
	d, _ := time.Parse("2006-01-02", s)
	return d
}

func TestBaselineCountsZeroFill(t *testing.T) {
	pool := ptrext.Of(fakePool{})
	pool.queryRoutes = append(pool.queryRoutes, struct {
		fragment string
		rows     [][]any
	}{"feedback_count FROM feedback_volume_buckets", [][]any{
		{"2026-08-03", int64(7)},
		{"2026-07-27", int64(9)},
	}})
	r := ptrext.Of(Repo{pool: pool})

	counts, err := r.BaselineCounts(context.Background(), "t1", SliceTotal, "total",
		[]time.Time{day("2026-07-27"), day("2026-08-03"), day("2026-08-10")})
	if err != nil {
		t.Fatalf("BaselineCounts: %v", err)
	}
	if counts[0] != 9 || counts[1] != 7 || counts[2] != 0 {
		t.Fatalf("zero-fill wrong: %v", counts)
	}
}

func TestSlicesForDetectionMapsRows(t *testing.T) {
	pool := ptrext.Of(fakePool{})
	pool.queryRoutes = append(pool.queryRoutes, struct {
		fragment string
		rows     [][]any
	}{"SELECT DISTINCT slice_type", [][]any{
		{"total", "total", "All feedback"},
		{"source", "source:zendesk", "Zendesk"},
	}})
	r := ptrext.Of(Repo{pool: pool})

	slices, err := r.SlicesForDetection(context.Background(), "t1",
		AllSliceTypes(), day("2026-08-10"), []time.Time{day("2026-08-03")})
	if err != nil || len(slices) != 2 {
		t.Fatalf("SlicesForDetection: %v %v", err, slices)
	}
	if slices[1].Key != "source:zendesk" || slices[1].Display != "Zendesk" {
		t.Fatalf("mapping wrong: %+v", slices[1])
	}
}

func TestCountOnNoRowsIsZero(t *testing.T) {
	r := ptrext.Of(Repo{pool: ptrext.Of(fakePool{})})
	count, samples, err := r.CountOn(context.Background(), "t1", SliceTotal, "total", day("2026-08-10"))
	if err != nil || count != 0 || samples != nil {
		t.Fatalf("no-rows must yield zero: %d %v %v", count, samples, err)
	}
}

func TestCountOnReadsRow(t *testing.T) {
	pool := ptrext.Of(fakePool{})
	pool.rowRoutes = append(pool.rowRoutes, struct {
		fragment string
		row      scanRow
	}{"SELECT feedback_count, sample_feedback_ids", scanRow{vals: []any{int64(31), []int64{7, 8}}}})
	r := ptrext.Of(Repo{pool: pool})
	count, samples, err := r.CountOn(context.Background(), "t1", SliceTotal, "total", day("2026-08-10"))
	if err != nil || count != 31 || len(samples) != 2 {
		t.Fatalf("CountOn: %d %v %v", count, samples, err)
	}
}

func TestGetConfigDefaultsOnNoRows(t *testing.T) {
	r := ptrext.Of(Repo{pool: ptrext.Of(fakePool{})})
	cfg, err := r.GetConfig(context.Background(), "t1")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if cfg.Sensitivity != "medium" || cfg.MinCount != 10 {
		t.Fatalf("defaults expected on no rows: %+v", cfg)
	}
}

func TestGetConfigReadsRow(t *testing.T) {
	pool := ptrext.Of(fakePool{})
	pool.rowRoutes = append(pool.rowRoutes, struct {
		fragment string
		row      scanRow
	}{"FROM tenant_anomaly_configs", scanRow{vals: []any{
		"low", 25, 6,
		[]string{"total"},
		[]string{"total"},
		"digest", false, 3, 3, nil,
	}}})
	r := ptrext.Of(Repo{pool: pool})
	cfg, err := r.GetConfig(context.Background(), "t1")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if cfg.Sensitivity != "low" || cfg.MinCount != 25 || cfg.NotifyMode != "digest" ||
		cfg.DetectionEnabled || cfg.ConfigVersion != 3 || cfg.BackfilledAt != nil {
		t.Fatalf("row mapping wrong: %+v", cfg)
	}
}

func TestUpsertConfigAndMarkBackfilledExec(t *testing.T) {
	pool := ptrext.Of(fakePool{})
	r := ptrext.Of(Repo{pool: pool})
	if err := r.UpsertConfig(context.Background(), DefaultConfig("t1"), "op"); err != nil {
		t.Fatalf("UpsertConfig: %v", err)
	}
	if err := r.MarkBackfilled(context.Background(), "t1", 2); err != nil {
		t.Fatalf("MarkBackfilled: %v", err)
	}
	if len(pool.execTags) != 2 {
		t.Fatalf("want 2 execs, got %d", len(pool.execTags))
	}
}

func TestEventMutationsExec(t *testing.T) {
	pool := ptrext.Of(fakePool{})
	r := ptrext.Of(Repo{pool: pool})
	ctx := context.Background()
	id := uuid.New()
	if err := r.SetQualityAction(ctx, id, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err := r.ResolveEvent(ctx, "t1", id); err != nil {
		t.Fatal(err)
	}
	if err := r.RetractEvent(ctx, "t1", id); err != nil {
		t.Fatal(err)
	}
	if err := r.DisableCustomSlice(ctx, "t1", id, "reason"); err != nil {
		t.Fatal(err)
	}
	if err := r.CleanupRetention(ctx, 400, 90); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkRunDone(ctx, "t1", day("2026-08-10"), "owner"); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkRunFailed(ctx, "t1", day("2026-08-10"), "owner", nil); err != nil {
		t.Fatal(err)
	}
	if got := len(pool.execTags); got != 8 {
		t.Fatalf("want 8 execs (cleanup runs two), got %d", got)
	}
}

func TestListEventsMapsRows(t *testing.T) {
	now := time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)
	id := uuid.New()
	pool := ptrext.Of(fakePool{})
	pool.queryRoutes = append(pool.queryRoutes, struct {
		fragment string
		rows     [][]any
	}{"FROM anomaly_events", [][]any{{
		id, "t1", "total", "total", "All feedback", "spike",
		day("2026-08-09"), day("2026-08-09"), int64(40),
		12.0, 6.0, 21.0, 3.8,
		"open", nil, `{"sample_ids":[1]}`,
		now, now, nil,
	}}})
	r := ptrext.Of(Repo{pool: pool})

	events, err := r.ListEvents(context.Background(), "t1", "open", 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("ListEvents: %v %v", err, events)
	}
	e := events[0]
	if e.ID != id || e.Direction != "spike" || e.Observed != 40 || e.QualityActionID != nil {
		t.Fatalf("mapping wrong: %+v", e)
	}

	open, err := r.ListOpenEvents(context.Background(), "t1")
	if err != nil || len(open) != 1 {
		t.Fatalf("ListOpenEvents: %v %v", err, open)
	}
}

func TestListCustomSlicesMapsRows(t *testing.T) {
	id := uuid.New()
	pool := ptrext.Of(fakePool{})
	pool.queryRoutes = append(pool.queryRoutes, struct {
		fragment string
		rows     [][]any
	}{"FROM tenant_anomaly_custom_slices", [][]any{
		{id, "api criticals", `{"conditions":[]}`, true, ""},
	}})
	r := ptrext.Of(Repo{pool: pool})
	slices, err := r.ListCustomSlices(context.Background(), "t1")
	if err != nil || len(slices) != 1 || slices[0].Name != "api criticals" {
		t.Fatalf("ListCustomSlices: %v %+v", err, slices)
	}
}

func TestOpenDigestAnomaliesMapsRows(t *testing.T) {
	pool := ptrext.Of(fakePool{})
	pool.queryRoutes = append(pool.queryRoutes, struct {
		fragment string
		rows     [][]any
	}{"FROM anomaly_events", [][]any{
		{"severity=critical", "spike", int64(31), 12.0, uuid.NewString()},
	}})
	r := ptrext.Of(Repo{pool: pool})
	out, err := r.OpenDigestAnomaliesInWindow(context.Background(), "t1",
		day("2026-08-09"), day("2026-08-10"))
	if err != nil || len(out) != 1 || out[0].SliceDisplay != "severity=critical" {
		t.Fatalf("OpenDigestAnomalies: %v %+v", err, out)
	}
}

func TestActiveTenantsMapsRows(t *testing.T) {
	pool := ptrext.Of(fakePool{})
	pool.queryRoutes = append(pool.queryRoutes, struct {
		fragment string
		rows     [][]any
	}{"FROM tenants t", [][]any{
		{"t1", "UTC"}, {"t2", "Asia/Shanghai"},
	}})
	r := ptrext.Of(Repo{pool: pool})
	tenants, err := r.ActiveTenantsWithFeedback(context.Background(), 90)
	if err != nil || len(tenants) != 2 || tenants[1].Timezone != "Asia/Shanghai" {
		t.Fatalf("ActiveTenants: %v %+v", err, tenants)
	}
}

func TestUnclaimedSettledDatesMapsRows(t *testing.T) {
	pool := ptrext.Of(fakePool{})
	pool.queryRoutes = append(pool.queryRoutes, struct {
		fragment string
		rows     [][]any
	}{"FROM unnest", [][]any{
		{day("2026-08-09")},
	}})
	r := ptrext.Of(Repo{pool: pool})
	free, err := r.UnclaimedSettledDates(context.Background(), "t1",
		[]time.Time{day("2026-08-08"), day("2026-08-09")})
	if err != nil || len(free) != 1 || free[0].Format("2006-01-02") != "2026-08-09" {
		t.Fatalf("UnclaimedSettledDates: %v %v", err, free)
	}
}

func TestClaimRunRowsAffected(t *testing.T) {
	pool := ptrext.Of(fakePool{})
	r := ptrext.Of(Repo{pool: pool})
	ok, err := r.ClaimRun(context.Background(), "t1", day("2026-08-10"), "owner", time.Minute)
	if err != nil || !ok {
		t.Fatalf("ClaimRun with UPDATE 1 tag must claim: %v %v", ok, err)
	}
}

func TestNonEmptyJSON(t *testing.T) {
	if nonEmptyJSON("") != "{}" || nonEmptyJSON(`{"a":1}`) != `{"a":1}` {
		t.Fatal("nonEmptyJSON wrong")
	}
}
