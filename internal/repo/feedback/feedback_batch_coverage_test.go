// SPDX-License-Identifier: Apache-2.0

package feedback

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

func TestProcessTagUpdateBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := FeedbackRepo{}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	deletedAt := now.Add(time.Minute)
	boom := errors.New("boom")
	update := BatchTagUpdate{
		FeedbackID: 10,
		AddTags:    []string{"aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb"},
		RemoveTags: []string{"bbbbbbbb-1111-2222-3333-cccccccccccc"},
	}

	tests := []struct {
		name     string
		tx       *fakeFeedbackBatchTx
		since    *time.Time
		wantCode string
		wantExec int
	}{
		{
			name:     "not found",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{err: pgx.ErrNoRows}}}),
			wantCode: BatchErrNotFound,
		},
		{
			name:     "query error",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{err: boom}}}),
			wantCode: BatchErrNotFound,
		},
		{
			name:     "deleted",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, ptrext.Of(deletedAt)}}}}),
			wantCode: BatchErrDeleted,
		},
		{
			name:     "version conflict",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, nil}}}}),
			since:    ptrext.Of(now.Add(-time.Second)),
			wantCode: BatchErrVersionConflict,
		},
		{
			name:     "add error",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, nil}}}, execErrs: []error{boom}}),
			wantCode: BatchErrInvalidState,
			wantExec: 1,
		},
		{
			name: "remove error",
			tx: ptrext.Of(fakeFeedbackBatchTx{
				rows:     []fakeFeedbackBatchRow{{values: []any{now, nil}}},
				execErrs: []error{nil, boom},
			}),
			wantCode: BatchErrInvalidState,
			wantExec: 2,
		},
		{
			name: "touch error",
			tx: ptrext.Of(fakeFeedbackBatchTx{
				rows:     []fakeFeedbackBatchRow{{values: []any{now, nil}}},
				execErrs: []error{nil, nil, boom},
			}),
			wantCode: BatchErrInvalidState,
			wantExec: 3,
		},
		{
			name:     "success",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, nil}}}}),
			wantExec: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			failure := repo.processTagUpdate(ctx, tc.tx, "tenant-1", update, tc.since)

			requireBatchFailure(t, failure, tc.wantCode)
			if tc.wantExec > 0 && tc.tx.execIdx != tc.wantExec {
				t.Fatalf("exec count = %d, want %d", tc.tx.execIdx, tc.wantExec)
			}
		})
	}
}

func TestProcessWorkflowUpdateBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := FeedbackRepo{}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	deletedAt := now.Add(time.Minute)
	current := "aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb"
	target := "cccccccc-1111-2222-3333-dddddddddddd"
	boom := errors.New("boom")

	tests := []struct {
		name     string
		tx       *fakeFeedbackBatchTx
		comment  string
		since    *time.Time
		wantCode string
		wantExec int
	}{
		{
			name:     "not found",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{err: pgx.ErrNoRows}}}),
			wantCode: BatchErrNotFound,
		},
		{
			name:     "query error",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{err: boom}}}),
			wantCode: BatchErrNotFound,
		},
		{
			name:     "deleted",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, ptrext.Of(deletedAt), nil}}}}),
			wantCode: BatchErrDeleted,
		},
		{
			name:     "version conflict",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, nil, ptrext.Of(current)}}}}),
			since:    ptrext.Of(now.Add(-time.Second)),
			wantCode: BatchErrVersionConflict,
		},
		{
			name: "already in target state",
			tx:   ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, nil, ptrext.Of(target)}}}}),
		},
		{
			name:     "update error",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, nil, ptrext.Of(current)}}}, execErrs: []error{boom}}),
			wantCode: BatchErrInvalidState,
			wantExec: 1,
		},
		{
			name:     "success without transition audit",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, nil, nil}}}}),
			wantExec: 1,
		},
		{
			name:     "success with transition audit",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, nil, ptrext.Of(current)}}}}),
			comment:  "triaged",
			wantExec: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			failure := repo.processWorkflowUpdate(ctx, tc.tx, "tenant-1", 10, target, tc.comment, tc.since)

			requireBatchFailure(t, failure, tc.wantCode)
			if tc.wantExec > 0 && tc.tx.execIdx != tc.wantExec {
				t.Fatalf("exec count = %d, want %d", tc.tx.execIdx, tc.wantExec)
			}
		})
	}
}

func TestProcessSoftDeleteBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := FeedbackRepo{}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	deletedAt := now.Add(time.Minute)
	boom := errors.New("boom")

	tests := []struct {
		name     string
		tx       *fakeFeedbackBatchTx
		since    *time.Time
		wantCode string
		wantExec int
	}{
		{
			name:     "not found",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{err: pgx.ErrNoRows}}}),
			wantCode: BatchErrNotFound,
		},
		{
			name:     "query error",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{err: boom}}}),
			wantCode: BatchErrNotFound,
		},
		{
			name:     "already deleted",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, ptrext.Of(deletedAt)}}}}),
			wantCode: BatchErrDeleted,
		},
		{
			name:     "version conflict",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, nil}}}}),
			since:    ptrext.Of(now.Add(-time.Second)),
			wantCode: BatchErrVersionConflict,
		},
		{
			name:     "update error",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, nil}}}, execErrs: []error{boom}}),
			wantCode: BatchErrInvalidState,
			wantExec: 1,
		},
		{
			name:     "success",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now, nil}}}}),
			wantExec: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			failure := repo.processSoftDelete(ctx, tc.tx, "tenant-1", 10, tc.since)

			requireBatchFailure(t, failure, tc.wantCode)
			if tc.wantExec > 0 && tc.tx.execIdx != tc.wantExec {
				t.Fatalf("exec count = %d, want %d", tc.tx.execIdx, tc.wantExec)
			}
		})
	}
}

func TestProcessHardDeleteBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := FeedbackRepo{}
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	boom := errors.New("boom")

	tests := []struct {
		name     string
		tx       *fakeFeedbackBatchTx
		since    *time.Time
		wantCode string
		wantExec int
	}{
		{
			name:     "not found",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{err: pgx.ErrNoRows}}}),
			wantCode: BatchErrNotFound,
		},
		{
			name:     "query error",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{err: boom}}}),
			wantCode: BatchErrNotFound,
		},
		{
			name:     "version conflict",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now}}}}),
			since:    ptrext.Of(now.Add(-time.Second)),
			wantCode: BatchErrVersionConflict,
		},
		{
			name:     "delete tags error",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now}}}, execErrs: []error{boom}}),
			wantCode: BatchErrInvalidState,
			wantExec: 1,
		},
		{
			name: "delete feedback error",
			tx: ptrext.Of(fakeFeedbackBatchTx{
				rows:     []fakeFeedbackBatchRow{{values: []any{now}}},
				execErrs: []error{nil, nil, boom},
			}),
			wantCode: BatchErrInvalidState,
			wantExec: 3,
		},
		{
			name: "feedback disappeared",
			tx: ptrext.Of(fakeFeedbackBatchTx{
				rows:  []fakeFeedbackBatchRow{{values: []any{now}}},
				execs: []pgconn.CommandTag{pgconn.NewCommandTag("DELETE 1"), pgconn.NewCommandTag("DELETE 0"), pgconn.NewCommandTag("DELETE 0")},
			}),
			wantCode: BatchErrNotFound,
			wantExec: 3,
		},
		{
			name:     "success",
			tx:       ptrext.Of(fakeFeedbackBatchTx{rows: []fakeFeedbackBatchRow{{values: []any{now}}}}),
			wantExec: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			failure := repo.processHardDelete(ctx, tc.tx, "tenant-1", 10, tc.since)

			requireBatchFailure(t, failure, tc.wantCode)
			if tc.wantExec > 0 && tc.tx.execIdx != tc.wantExec {
				t.Fatalf("exec count = %d, want %d", tc.tx.execIdx, tc.wantExec)
			}
		})
	}
}

func requireBatchFailure(t *testing.T, failure *ItemFailure, wantCode string) {
	t.Helper()
	if wantCode == "" {
		if failure != nil {
			t.Fatalf("failure = %+v, want nil", ptrext.Indirect(failure))
		}
		return
	}
	if failure == nil {
		t.Fatalf("failure = nil, want code %s", wantCode)
	}
	if failure.Code != wantCode {
		t.Fatalf("failure code = %s, want %s", failure.Code, wantCode)
	}
}

type fakeFeedbackBatchTx struct {
	rows     []fakeFeedbackBatchRow
	rowIdx   int
	execs    []pgconn.CommandTag
	execErrs []error
	execIdx  int
}

func (tx *fakeFeedbackBatchTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }
func (tx *fakeFeedbackBatchTx) Commit(context.Context) error          { return nil }
func (tx *fakeFeedbackBatchTx) Rollback(context.Context) error        { return nil }
func (tx *fakeFeedbackBatchTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected CopyFrom call in fakeFeedbackBatchTx")
}
func (tx *fakeFeedbackBatchTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (tx *fakeFeedbackBatchTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (tx *fakeFeedbackBatchTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected Prepare call in fakeFeedbackBatchTx")
}

func (tx *fakeFeedbackBatchTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	idx := tx.execIdx
	tx.execIdx++
	if idx < len(tx.execErrs) && tx.execErrs[idx] != nil {
		return pgconn.CommandTag{}, tx.execErrs[idx]
	}
	if idx < len(tx.execs) {
		return tx.execs[idx], nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *fakeFeedbackBatchTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query call in fakeFeedbackBatchTx")
}

func (tx *fakeFeedbackBatchTx) QueryRow(context.Context, string, ...any) pgx.Row {
	if tx.rowIdx >= len(tx.rows) {
		return fakeFeedbackBatchRow{err: errors.New("unexpected QueryRow call in fakeFeedbackBatchTx")}
	}
	row := tx.rows[tx.rowIdx]
	tx.rowIdx++
	return row
}
func (tx *fakeFeedbackBatchTx) Conn() *pgx.Conn { return nil }

type fakeFeedbackBatchRow struct {
	values []any
	err    error
}

func (r fakeFeedbackBatchRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("scan destination count mismatch")
	}
	for i := range dest {
		if err := assignFeedbackBatchScanValue(dest[i], r.values[i]); err != nil {
			return err
		}
	}
	return nil
}

func assignFeedbackBatchScanValue(dest any, src any) error {
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
	return errors.New(strings.Join([]string{"scan source type mismatch:", source.Type().String(), "to", target.Type().String()}, " "))
}
