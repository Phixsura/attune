// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow scan fixtures

package restoredrill

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestAssessLastRunGradesRecordedState(t *testing.T) {
	t.Parallel()

	freshness := 7 * 24 * time.Hour
	for _, tc := range []struct {
		name       string
		ok         bool
		last       LastRun
		age        time.Duration
		wantStatus Status
		wantText   string
	}{
		{name: "none recorded", ok: false, wantStatus: StatusWarn, wantText: "No restore drill"},
		{name: "failed", ok: true, last: LastRun{Status: StatusFail}, age: 48 * time.Hour, wantStatus: StatusFail, wantText: "FAILED"},
		{name: "warn", ok: true, last: LastRun{Status: StatusWarn}, age: 24 * time.Hour, wantStatus: StatusWarn, wantText: "warnings"},
		{name: "skip", ok: true, last: LastRun{Status: StatusSkip}, age: time.Hour, wantStatus: StatusWarn, wantText: "did not verify"},
		{name: "future pass", ok: true, last: LastRun{Status: StatusPass}, age: -time.Minute, wantStatus: StatusWarn, wantText: "future timestamp"},
		{name: "stale pass", ok: true, last: LastRun{Status: StatusPass}, age: 8 * 24 * time.Hour, wantStatus: StatusWarn, wantText: "stale"},
		{name: "fresh pass", ok: true, last: LastRun{Status: StatusPass}, age: time.Hour, wantStatus: StatusPass, wantText: "passed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := AssessLastRun(tc.ok, tc.last, tc.age, freshness)
			if got.Status != tc.wantStatus {
				t.Fatalf("AssessLastRun() status = %q, want %q", got.Status, tc.wantStatus)
			}
			if !strings.Contains(got.Message, tc.wantText) {
				t.Fatalf("AssessLastRun() message = %q, want containing %q", got.Message, tc.wantText)
			}
		})
	}
}

func TestAgoString(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		age  time.Duration
		want string
	}{
		{name: "today", age: 23 * time.Hour, want: "today"},
		{name: "one day", age: 24 * time.Hour, want: "1 day ago"},
		{name: "many days", age: 72 * time.Hour, want: "3 days ago"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := agoString(tc.age); got != tc.want {
				t.Fatalf("agoString(%s) = %q, want %q", tc.age, got, tc.want)
			}
		})
	}
}

func TestReadLastMapsNoRowsAndSuccess(t *testing.T) {
	t.Parallel()

	if got, ok, err := ReadLast(context.Background(), ptrext.Of(fakeLastRunQuerier{
		row: fakeLastRunRow{err: pgx.ErrNoRows},
	})); err != nil || ok || got.Status != "" {
		t.Fatalf("ReadLast(no rows) = %+v, %t, %v; want zero false nil", got, ok, err)
	}

	ranAt := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	report := json.RawMessage(`{"status":"pass"}`)
	got, ok, err := ReadLast(context.Background(), ptrext.Of(fakeLastRunQuerier{
		row: fakeLastRunRow{values: []any{ranAt, "pass", "backup-1", int64(1234), report}},
	}))
	if err != nil || !ok {
		t.Fatalf("ReadLast(success) ok=%t err=%v, want true nil", ok, err)
	}
	if !got.RanAt.Equal(ranAt) || got.Status != StatusPass || got.BackupRef != "backup-1" || got.DurationMS != 1234 {
		t.Fatalf("ReadLast(success) = %+v", got)
	}
	if string(got.Report) != string(report) {
		t.Fatalf("ReadLast report = %s, want %s", got.Report, report)
	}
}

func TestReadLastWrapsScanErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("scan failed")
	_, ok, err := ReadLast(context.Background(), ptrext.Of(fakeLastRunQuerier{row: fakeLastRunRow{err: boom}}))
	if ok || !errors.Is(err, boom) || !strings.Contains(err.Error(), "read last restore drill") {
		t.Fatalf("ReadLast(scan error) ok=%t err=%v, want wrapped scan error", ok, err)
	}
}

type fakeLastRunQuerier struct {
	row fakeLastRunRow
}

func (q *fakeLastRunQuerier) QueryRow(context.Context, string, ...any) pgx.Row {
	return q.row
}

type fakeLastRunRow struct {
	values []any
	err    error
}

func (r fakeLastRunRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("unexpected scan destination count")
	}
	for i, value := range r.values {
		switch d := dest[i].(type) {
		case *time.Time:
			*d = value.(time.Time)
		case *string:
			*d = value.(string)
		case *int64:
			*d = value.(int64)
		case *json.RawMessage:
			*d = append((*d)[:0], value.(json.RawMessage)...)
		default:
			return errors.New("unexpected scan destination type")
		}
	}
	return nil
}
