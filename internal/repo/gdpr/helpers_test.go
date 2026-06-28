// SPDX-License-Identifier: Apache-2.0

package gdpr

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

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
	if len(dest) != len(r.values) {
		return errors.New("scan destination count mismatch")
	}
	for i := range dest {
		if err := assignScannedValue(dest[i], r.values[i]); err != nil {
			return err
		}
	}
	return nil
}

type fakeRows struct {
	rows [][]any
	idx  int
	err  error
}

func (r *fakeRows) Close() {}

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
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.rows) {
		return errors.New("scan called without current row")
	}
	for i := range dest {
		if err := assignScannedValue(dest[i], r.rows[r.idx-1][i]); err != nil {
			return err
		}
	}
	return nil
}

func (r *fakeRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.rows) {
		return nil, errors.New("values called without current row")
	}
	return r.rows[r.idx-1], nil
}

func (r *fakeRows) RawValues() [][]byte {
	return nil
}

func (r *fakeRows) Conn() *pgx.Conn {
	return nil
}

type fakeQueryer struct {
	rows pgx.Rows
	err  error
}

func (q fakeQueryer) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if q.err != nil {
		return nil, q.err
	}
	return q.rows, nil
}

func assignScannedValue(dest any, src any) error {
	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Pointer || dv.IsNil() {
		return errors.New("scan destination must be a non-nil pointer")
	}
	target := dv.Elem()
	if src == nil {
		target.Set(reflect.Zero(target.Type()))
		return nil
	}
	sv := reflect.ValueOf(src)
	if sv.Type().AssignableTo(target.Type()) {
		target.Set(sv)
		return nil
	}
	if sv.Type().ConvertibleTo(target.Type()) {
		target.Set(sv.Convert(target.Type()))
		return nil
	}
	return errors.New("scan source type mismatch")
}

func TestScanExportJob(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0).UTC()
	startedAt := now.Add(time.Minute)
	completedAt := now.Add(2 * time.Minute)
	expiresAt := now.Add(3 * time.Minute)
	downloadedAt := now.Add(4 * time.Minute)
	revokedAt := now.Add(5 * time.Minute)
	claimedAt := now.Add(6 * time.Minute)
	heartbeatAt := now.Add(7 * time.Minute)

	job, err := scanExportJob(fakeRow{values: []any{
		"job-1", "tenant-1", "subject-1", "hash-1", "Subject One", ExportJobCompleted,
		[]byte("zip"), "export.zip", 1, 2, 3, 4, "", "admin", "admin-1", now, ptrext.Of(startedAt), ptrext.Of(completedAt),
		ptrext.Of(expiresAt), ptrext.Of(downloadedAt), ptrext.Of(revokedAt), ptrext.Of(claimedAt), ptrext.Of(heartbeatAt),
	}})
	if err != nil {
		t.Fatalf("scanExportJob() err = %v", err)
	}
	if job.ID != "job-1" || job.Status != ExportJobCompleted || job.ArchiveFilename != "export.zip" {
		t.Fatalf("unexpected job: %#v", job)
	}
	if job.StartedAt == nil || !job.StartedAt.Equal(startedAt) {
		t.Fatalf("StartedAt = %v, want %v", job.StartedAt, startedAt)
	}
	if job.HeartbeatAt == nil || !job.HeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("HeartbeatAt = %v, want %v", job.HeartbeatAt, heartbeatAt)
	}
	if job.CreatedByType != "admin" {
		t.Fatalf("CreatedByType = %q, want admin", job.CreatedByType)
	}
}

func TestScanExportJobReturnsScanError(t *testing.T) {
	t.Parallel()
	_, err := scanExportJob(fakeRow{err: errors.New("boom")})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("scanExportJob() err = %v, want boom", err)
	}
}

func TestScanRequest(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0).UTC()
	startedAt := now.Add(time.Minute)
	completedAt := now.Add(2 * time.Minute)
	expiresAt := now.Add(3 * time.Minute)
	downloadedAt := now.Add(4 * time.Minute)
	executeAfter := now.Add(5 * time.Minute)
	cancelledAt := now.Add(6 * time.Minute)
	revokedAt := now.Add(7 * time.Minute)

	req, err := scanRequest(fakeRow{values: []any{
		"req-1", "tenant-1", RequestTypeDelete, RequestStatusScheduled, "subject-1", "hash-1", "Subject One",
		1, 2, 3, 4, "bundle.zip", "", "admin", "admin-1", now, ptrext.Of(startedAt), ptrext.Of(completedAt), ptrext.Of(expiresAt), ptrext.Of(downloadedAt),
		ptrext.Of(executeAfter), ptrext.Of(cancelledAt), ptrext.Of(revokedAt),
	}})
	if err != nil {
		t.Fatalf("scanRequest() err = %v", err)
	}
	if req.ID != "req-1" || req.RequestType != RequestTypeDelete || req.Status != RequestStatusScheduled {
		t.Fatalf("unexpected request: %#v", req)
	}
	if req.ExecuteAfter == nil || !req.ExecuteAfter.Equal(executeAfter) {
		t.Fatalf("ExecuteAfter = %v, want %v", req.ExecuteAfter, executeAfter)
	}
	if req.RevokedAt == nil || !req.RevokedAt.Equal(revokedAt) {
		t.Fatalf("RevokedAt = %v, want %v", req.RevokedAt, revokedAt)
	}
	if req.CreatedByType != "admin" {
		t.Fatalf("CreatedByType = %q, want admin", req.CreatedByType)
	}
}

func TestFormatAndParseRequestCursor(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 123).UTC()
	cursor := formatRequestCursor(Request{ID: "req-1", CreatedAt: now})
	parsedTime, parsedID, err := parseRequestCursor(cursor)
	if err != nil {
		t.Fatalf("parseRequestCursor() err = %v", err)
	}
	if !parsedTime.Equal(now) || parsedID != "req-1" {
		t.Fatalf("parsed cursor = (%v, %q), want (%v, %q)", parsedTime, parsedID, now, "req-1")
	}
}

func TestParseRequestCursorRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	cases := []string{"", "abc", "nan:req-1", "123:"}
	for _, raw := range cases {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if _, _, err := parseRequestCursor(raw); err == nil {
				t.Fatalf("parseRequestCursor(%q) err = nil", raw)
			}
		})
	}
}

func TestNewRequestIDReturnsUUID(t *testing.T) {
	t.Parallel()
	first := newRequestID()
	second := newRequestID()
	if first == "" || second == "" || first == second {
		t.Fatalf("newRequestID() produced invalid ids: %q %q", first, second)
	}
}

func TestSubjectMatchClauseIncludesLegacyFallbacks(t *testing.T) {
	t.Parallel()
	clause := subjectMatchClause(2)
	wantFragments := []string{
		"subject_key = $2",
		"split_part(user_id, ':', 2) = $2",
		"user_id = $2",
	}
	for _, fragment := range wantFragments {
		if !strings.Contains(clause, fragment) {
			t.Fatalf("subjectMatchClause() = %q, want fragment %q", clause, fragment)
		}
	}
}

func TestSubjectInfoTxReturnsFirstDisplayAndIDs(t *testing.T) {
	t.Parallel()
	rows := ptrext.Of(fakeRows{rows: [][]any{
		{int64(10), "Alice"},
		{int64(20), "Alice Updated"},
	}})
	info, err := subjectInfoTx(context.Background(), fakeQueryer{rows: rows}, "tenant-1", "alice@example.com")
	if err != nil {
		t.Fatalf("subjectInfoTx() err = %v", err)
	}
	if info.subjectDisplay != "Alice" {
		t.Fatalf("subjectDisplay = %q, want Alice", info.subjectDisplay)
	}
	if !reflect.DeepEqual(info.feedbackIDs, []int64{10, 20}) {
		t.Fatalf("feedbackIDs = %#v", info.feedbackIDs)
	}
}

func TestSubjectInfoTxReturnsNotFound(t *testing.T) {
	t.Parallel()
	_, err := subjectInfoTx(context.Background(), fakeQueryer{rows: ptrext.Of(fakeRows{})}, "tenant-1", "missing")
	if !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("subjectInfoTx() err = %v, want %v", err, ErrSubjectNotFound)
	}
}

func TestSubjectInfoTxReturnsWrappedErrors(t *testing.T) {
	t.Parallel()
	_, err := subjectInfoTx(context.Background(), fakeQueryer{err: errors.New("query failed")}, "tenant-1", "missing")
	if err == nil || err.Error() != "query subject feedback ids: query failed" {
		t.Fatalf("subjectInfoTx() err = %v", err)
	}
}
