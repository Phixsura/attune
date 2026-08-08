// SPDX-License-Identifier: Apache-2.0

package signalgraph

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestSignalGraphRepoListAndDetailPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	subjectID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	eventID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	identityID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	pool := ptrext.Of(fakeSignalRepoPool{
		rows: []fakeSignalRepoRow{
			{values: []any{2, 3, 9}},
			{values: subjectSummaryValues(subjectID, now)},
		},
		queries: []*fakeSignalRepoRows{
			{rows: [][]any{recentMergeValues(eventID, subjectID, now)}},
			{rows: [][]any{subjectSummaryValues(subjectID, now)}},
			{rows: [][]any{identityValues(identityID, now)}},
			{rows: [][]any{eventValues(eventID, now)}},
			{rows: [][]any{{int64(101), "intercom", "user-1", "Login failed", []byte(`{"tier":"enterprise"}`), now}}},
		},
	})
	repo := Repo{pool: pool}

	merges, err := repo.ListRecentMerges(ctx, "tenant-a", 1)
	if err != nil || len(merges) != 1 || merges[0].Subject.ID != subjectID {
		t.Fatalf("ListRecentMerges() = %#v, %v", merges, err)
	}
	roster, err := repo.ListSubjectRoster(ctx, "tenant-a", 6)
	if err != nil || roster.ActiveSubjectCount != 2 || len(roster.Subjects) != 1 {
		t.Fatalf("ListSubjectRoster() = %#v, %v", roster, err)
	}
	detail, err := repo.SubjectDetail(ctx, "tenant-a", subjectID, 5)
	if err != nil || len(detail.Identities) != 1 || len(detail.Events[0].Evidence) != 1 {
		t.Fatalf("SubjectDetail() = %#v, %v", detail, err)
	}
}

func TestSignalGraphRepoMergeAndSplitTxPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	subjectID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tx := ptrext.Of(fakeSignalRepoTx{rows: []fakeSignalRepoRow{
		{values: []any{2, sql.NullInt64{Int64: 101, Valid: true}, sql.NullInt64{Int64: 102, Valid: true}}},
		{err: pgx.ErrNoRows},
		{values: subjectCreateValues(subjectID, now)},
		{values: []any{subjectID}},
		{values: subjectSummaryValues(subjectID, now)},
		{values: []any{"ada@example.test", 2, []int64{101, 102}}},
		{values: subjectSummaryValues(subjectID, now)},
	}})
	repo := Repo{}
	merge, err := repo.MergeIdentityReviewTx(ctx, tx, MergeIdentityReviewInput{
		TenantID: "tenant-a", ActorID: "admin-1", IdentityKind: "email",
		IdentityValue: "Ada@Example.Test", IdentityValueNormalized: "ada@example.test",
		FeedbackIDs: []int64{101, 102}, Note: "same customer",
	})
	if err != nil || !merge.CreatedSubject || merge.EvidenceCount != 2 {
		t.Fatalf("MergeIdentityReviewTx() = %#v, %v", merge, err)
	}
	split, err := repo.SplitIdentityReviewTx(ctx, tx, SplitIdentityReviewInput{
		TenantID: "tenant-a", ActorID: "admin-1", SubjectID: subjectID,
		IdentityKind: "email", IdentityValueNormalized: "ada@example.test", Note: "split customer",
	})
	if err != nil || split.Subject.ID != subjectID || split.EvidenceCount != 2 {
		t.Fatalf("SplitIdentityReviewTx() = %#v, %v", split, err)
	}
	if tx.execIdx != 3 {
		t.Fatalf("exec count = %d, want merge event, split event, refresh", tx.execIdx)
	}
}

func TestSignalGraphRepoBoundsAndNormalization(t *testing.T) {
	t.Parallel()

	if boundedRecentMergeLimit(0) != 10 || boundedRecentMergeLimit(100) != 50 {
		t.Fatal("boundedRecentMergeLimit did not clamp values")
	}
	if boundedSubjectRosterLimit(0) != 6 || boundedSubjectRosterLimit(100) != 50 {
		t.Fatal("boundedSubjectRosterLimit did not clamp values")
	}
	if boundedSubjectEventLimit(0) != 20 || boundedSubjectEventLimit(101) != 100 {
		t.Fatal("boundedSubjectEventLimit did not clamp values")
	}
	if boundedSubjectEventEvidenceLimit(0) != 5 || boundedSubjectEventEvidenceLimit(99) != 10 {
		t.Fatal("boundedSubjectEventEvidenceLimit did not clamp values")
	}
	if nullableInt64(sql.NullInt64{}) != nil || nullableInt64(sql.NullInt64{Int64: 42, Valid: true}) != int64(42) {
		t.Fatal("nullableInt64 did not preserve null and present values")
	}
	if NormalizeIdentityValue("email", " Ada@Example.Test ") != "ada@example.test" {
		t.Fatal("NormalizeIdentityValue did not lowercase email identities")
	}
	if NormalizeIdentityValue("external", " value-1 ") != "value-1" {
		t.Fatal("NormalizeIdentityValue did not trim non-email identities")
	}
}

func recentMergeValues(eventID uuid.UUID, subjectID uuid.UUID, now time.Time) []any {
	return []any{
		eventID, "email", "ada@example.test",
		[]int64{101, 102},
		2, "admin-1", now,
		subjectID, "tenant-a", "Ada", "email", "ada@example.test", "active", 1, 2, now, now,
	}
}

func subjectSummaryValues(id uuid.UUID, now time.Time) []any {
	return []any{id, "tenant-a", "Ada", "email", "ada@example.test", "active", 1, 2, now, now}
}

func subjectCreateValues(id uuid.UUID, now time.Time) []any {
	return []any{id, "tenant-a", "Ada@Example.Test", "email", "Ada@Example.Test", "active", now, now}
}

func identityValues(id uuid.UUID, now time.Time) []any {
	return []any{
		id, "email", "ada@example.test", "review", "reviewed", 2, int64(101), int64(102),
		false,
		sql.NullTime{},
		now, now,
	}
}

func eventValues(id uuid.UUID, now time.Time) []any {
	return []any{id, "review_merge", "email", "ada@example.test", []int64{101, 0, 102}, 2, "same customer", "admin-1", now}
}

type fakeSignalRepoPool struct {
	rows     []fakeSignalRepoRow
	rowIdx   int
	queries  []*fakeSignalRepoRows
	queryIdx int
}

func (p *fakeSignalRepoPool) Begin(context.Context) (pgx.Tx, error) {
	return ptrext.Of(fakeSignalRepoTx{}), nil
}

func (p *fakeSignalRepoPool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (p *fakeSignalRepoPool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if p.queryIdx >= len(p.queries) {
		p.queryIdx++
		return ptrext.Of(fakeSignalRepoRows{}), nil
	}
	rows := p.queries[p.queryIdx]
	p.queryIdx++
	return rows, nil
}

func (p *fakeSignalRepoPool) QueryRow(context.Context, string, ...any) pgx.Row {
	if p.rowIdx >= len(p.rows) {
		return fakeSignalRepoRow{err: errors.New("unexpected query row")}
	}
	row := p.rows[p.rowIdx]
	p.rowIdx++
	return row
}

type fakeSignalRepoTx struct {
	rows    []fakeSignalRepoRow
	rowIdx  int
	execIdx int
}

func (tx *fakeSignalRepoTx) Begin(context.Context) (pgx.Tx, error) {
	return tx, nil
}

func (tx *fakeSignalRepoTx) Commit(context.Context) error {
	return nil
}

func (tx *fakeSignalRepoTx) Rollback(context.Context) error {
	return nil
}

func (tx *fakeSignalRepoTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (tx *fakeSignalRepoTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (tx *fakeSignalRepoTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (tx *fakeSignalRepoTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (tx *fakeSignalRepoTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tx.execIdx++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *fakeSignalRepoTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return ptrext.Of(fakeSignalRepoRows{}), nil
}

func (tx *fakeSignalRepoTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if tx.rowIdx >= len(tx.rows) {
		return fakeSignalRepoRow{err: errors.New("unexpected tx query row")}
	}
	row := tx.rows[tx.rowIdx]
	tx.rowIdx++
	return row
}
func (tx *fakeSignalRepoTx) Conn() *pgx.Conn { return nil }

type fakeSignalRepoRow struct {
	values []any
	err    error
}

func (r fakeSignalRepoRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("scan destination count mismatch")
	}
	return assignSignalScanValues(dest, r.values)
}

type fakeSignalRepoRows struct {
	rows [][]any
	idx  int
	err  error
}

func (r *fakeSignalRepoRows) Close()                                       {}
func (r *fakeSignalRepoRows) Err() error                                   { return r.err }
func (r *fakeSignalRepoRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeSignalRepoRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeSignalRepoRows) RawValues() [][]byte                          { return nil }
func (r *fakeSignalRepoRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeSignalRepoRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeSignalRepoRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.rows) {
		return nil, errors.New("values called without current row")
	}
	return r.rows[r.idx-1], nil
}

func (r *fakeSignalRepoRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.rows) {
		return errors.New("scan called without current row")
	}
	if len(dest) != len(r.rows[r.idx-1]) {
		return errors.New("scan destination count mismatch")
	}
	return assignSignalScanValues(dest, r.rows[r.idx-1])
}

func assignSignalScanValues(dest []any, values []any) error {
	for i := range dest {
		if err := assignSignalScanValue(dest[i], values[i]); err != nil {
			return err
		}
	}
	return nil
}

func assignSignalScanValue(dest any, src any) error {
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Pointer || destValue.IsNil() {
		return errors.New("scan destination must be a non-nil pointer")
	}
	target := destValue.Elem()
	if src == nil {
		target.Set(reflect.Zero(target.Type()))
		return nil
	}
	source := reflect.ValueOf(src)
	if source.Type().AssignableTo(target.Type()) {
		target.Set(source)
		return nil
	}
	if source.Type().ConvertibleTo(target.Type()) {
		target.Set(source.Convert(target.Type()))
		return nil
	}
	return errors.New("scan source type mismatch")
}
