// SPDX-License-Identifier: Apache-2.0

package publicvisibility

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, value := range r.values {
		target := reflect.ValueOf(dest[i]).Elem()
		if value == nil {
			target.Set(reflect.Zero(target.Type()))
			continue
		}
		source := reflect.ValueOf(value)
		if source.Type().AssignableTo(target.Type()) {
			target.Set(source)
			continue
		}
		target.Set(source.Convert(target.Type()))
	}
	return nil
}

type fakeQueryer struct {
	row  pgx.Row
	sql  string
	args []any
}

func (q *fakeQueryer) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.sql = sql
	q.args = args
	return q.row
}

type fakeRows struct {
	rows   []fakeRow
	index  int
	err    error
	closed bool
}

func (r *fakeRows) Close() {
	r.closed = true
}

func (r *fakeRows) Err() error {
	return r.err
}

func (r *fakeRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r *fakeRows) Next() bool {
	if r.index >= len(r.rows) {
		r.closed = true
		return false
	}
	r.index++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	return r.rows[r.index-1].Scan(dest...)
}

func (r *fakeRows) Values() ([]any, error) {
	if r.index == 0 {
		return nil, errors.New("next was not called")
	}
	return r.rows[r.index-1].values, nil
}

func (r *fakeRows) RawValues() [][]byte {
	return nil
}

func (r *fakeRows) Conn() *pgx.Conn {
	return nil
}

func TestColumnHelpers(t *testing.T) {
	t.Parallel()

	repository := New(nil)
	if repository == nil {
		t.Fatal("New() = nil, want repository")
	}
	if !strings.Contains(policyColumns(), "portal_access_mode") {
		t.Fatalf("policyColumns() = %q, want policy columns", policyColumns())
	}
	if !strings.Contains(subjectColumns(), "submitted_by_fingerprint") {
		t.Fatalf("subjectColumns() = %q, want subject columns", subjectColumns())
	}
	if !strings.Contains(profileColumns(), "included_in_roadmap") {
		t.Fatalf("profileColumns() = %q, want profile columns", profileColumns())
	}
	if !strings.Contains(prefixedPolicyColumns("pol"), "pol.tenant_id") {
		t.Fatalf("prefixedPolicyColumns() = %q, want alias prefix", prefixedPolicyColumns("pol"))
	}
	if !strings.Contains(prefixedSubjectColumns("pms"), "pms.subject_id") {
		t.Fatalf("prefixedSubjectColumns() = %q, want alias prefix", prefixedSubjectColumns("pms"))
	}
	if !strings.Contains(prefixedProfileColumns("prp"), "prp.public_slug") {
		t.Fatalf("prefixedProfileColumns() = %q, want alias prefix", prefixedProfileColumns("prp"))
	}
}

func TestScanHelpers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	policy, err := scanPolicy(fakeRow{values: policyValues(now)})
	if err != nil {
		t.Fatalf("scanPolicy() error = %v", err)
	}
	if policy.TenantID != "tenant-a" || policy.PortalAccessMode != AccessModePublic {
		t.Fatalf("scanPolicy() = %#v, want policy", policy)
	}

	subjectID := uuid.New()
	subject, err := scanSubject(fakeRow{values: subjectValues(subjectID, now)})
	if err != nil {
		t.Fatalf("scanSubject() error = %v", err)
	}
	if subject.ID != subjectID || subject.State != ModerationStatePending {
		t.Fatalf("scanSubject() = %#v, want subject", subject)
	}

	requestID := uuid.New()
	profile, err := scanProfile(fakeRow{values: profileValues(requestID, now)})
	if err != nil {
		t.Fatalf("scanProfile() error = %v", err)
	}
	if profile.RequestID != requestID || profile.PublicSlug != "pricing-api" {
		t.Fatalf("scanProfile() = %#v, want profile", profile)
	}

	scanErr := errors.New("scan failed")
	if _, err := scanPolicy(fakeRow{err: scanErr}); !errors.Is(err, scanErr) {
		t.Fatalf("scanPolicy() error = %v, want %v", err, scanErr)
	}
	if _, err := scanSubject(fakeRow{err: scanErr}); !errors.Is(err, scanErr) {
		t.Fatalf("scanSubject() error = %v, want %v", err, scanErr)
	}
	if _, err := scanProfile(fakeRow{err: scanErr}); !errors.Is(err, scanErr) {
		t.Fatalf("scanProfile() error = %v, want %v", err, scanErr)
	}
}

func TestLoadHelpersMapNoRowsAndUseExpectedQueries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	query := ptrext.Of(fakeQueryer{row: fakeRow{values: policyValues(now)}})
	policy, err := loadPolicy(context.Background(), query, "tenant-a")
	if err != nil {
		t.Fatalf("loadPolicy() error = %v", err)
	}
	if policy.TenantID != "tenant-a" || query.args[0] != "tenant-a" {
		t.Fatalf("loadPolicy() = %#v args=%#v, want tenant lookup", policy, query.args)
	}

	subjectID := uuid.New()
	query = ptrext.Of(fakeQueryer{row: fakeRow{values: subjectValues(subjectID, now)}})
	subject, err := loadSubject(context.Background(), query, "tenant-a", subjectID, true)
	if err != nil {
		t.Fatalf("loadSubject() error = %v", err)
	}
	if subject.ID != subjectID || !strings.Contains(query.sql, "FOR UPDATE") {
		t.Fatalf("loadSubject() = %#v sql=%q, want locked subject lookup", subject, query.sql)
	}

	query = ptrext.Of(fakeQueryer{row: fakeRow{err: pgx.ErrNoRows}})
	if _, err := loadPolicy(context.Background(), query, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("loadPolicy() error = %v, want %v", err, ErrNotFound)
	}
	if _, err := loadSubject(context.Background(), query, "missing", uuid.New(), false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("loadSubject() error = %v, want %v", err, ErrNotFound)
	}
}

func TestScanSubjects(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	rows := ptrext.Of(fakeRows{rows: []fakeRow{
		{values: subjectValues(uuid.New(), now)},
		{values: subjectValues(uuid.New(), now.Add(time.Minute))},
	}})
	subjects, err := scanSubjects(rows)
	if err != nil {
		t.Fatalf("scanSubjects() error = %v", err)
	}
	if len(subjects) != 2 || !rows.closed {
		t.Fatalf("scanSubjects() = %#v closed=%v, want two closed rows", subjects, rows.closed)
	}

	rows = ptrext.Of(fakeRows{err: errors.New("read failed")})
	if _, err := scanSubjects(rows); err == nil {
		t.Fatal("scanSubjects() error = nil, want rows error")
	}

	rows = ptrext.Of(fakeRows{rows: []fakeRow{{err: errors.New("scan failed")}}})
	if _, err := scanSubjects(rows); err == nil {
		t.Fatal("scanSubjects() error = nil, want row scan error")
	}
}

func TestPaginationAndWriteErrorHelpers(t *testing.T) {
	t.Parallel()

	if boundedLimit(0) != 50 || boundedLimit(101) != 100 || boundedLimit(20) != 20 {
		t.Fatalf("boundedLimit() returned unexpected values")
	}
	if offset, err := parseCursor(" 7 "); err != nil || offset != 7 {
		t.Fatalf("parseCursor() = %d, %v, want offset", offset, err)
	}
	if offset, err := parseCursor(" "); err != nil || offset != 0 {
		t.Fatalf("parseCursor(empty) = %d, %v, want zero", offset, err)
	}
	if _, err := parseCursor("-1"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("parseCursor(-1) error = %v, want %v", err, ErrInvalidInput)
	}
	if _, err := parseCursor("bad"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("parseCursor(bad) error = %v, want %v", err, ErrInvalidInput)
	}

	err := mapWriteError(ptrext.Of(pgconn.PgError{Code: "23505", ConstraintName: "public_subject_key"}))
	if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "public_subject_key") {
		t.Fatalf("mapWriteError() error = %v, want invalid input with constraint", err)
	}
	base := errors.New("plain error")
	if !errors.Is(mapWriteError(base), base) {
		t.Fatalf("mapWriteError() did not preserve plain error")
	}
}

func policyValues(now time.Time) []any {
	return []any{
		"tenant-a", AccessModePublic, true, true, true, true, false,
		WriteModeIdentified, WriteModeDisabled, WriteModeAnonymous,
		ModerationStateApproved, ModerationStatePending, IdentityModeDisplayName,
		true, false, true, false, "admin-1", now, now.Add(time.Minute),
	}
}

func subjectValues(id uuid.UUID, now time.Time) []any {
	return []any{
		id, "tenant-a", SurfaceRequest, "request-profile-1", ModerationStatePending,
		"", "", "Ada", "fingerprint-1", "", ptrext.Of(now),
		now, now.Add(time.Minute),
	}
}

func profileValues(requestID uuid.UUID, now time.Time) []any {
	return []any{
		uuid.New(), "tenant-a", requestID, "pricing-api", "Pricing API", "Summary",
		"planned", "next", true, false, ptrext.Of(now), "admin-1",
		now, now.Add(time.Minute),
	}
}
