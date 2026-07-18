// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow pgx fake rows need scan target writes.

package secretlock

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestWithTxRejectsMissingPoolAndEmptyKeyIsWritable(t *testing.T) {
	ctx := context.Background()
	if err := WithTx(ctx, nil, true, func(context.Context, Tx) error { return nil }); err == nil {
		t.Fatalf("WithTx(nil pool) error = nil")
	}
	if err := EnsureWritableKey(ctx, nil, ""); err != nil {
		t.Fatalf("EnsureWritableKey(empty) error = %v", err)
	}
}

func TestLockTxMapsExecErrors(t *testing.T) {
	ctx := context.Background()
	if err := LockTx(ctx, ptrext.Of(fakeSecretLockTx{})); err != nil {
		t.Fatalf("LockTx(success) error = %v", err)
	}
	boom := errors.New("boom")
	err := LockTx(ctx, ptrext.Of(fakeSecretLockTx{execErr: boom}))
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "lock secret key registry") {
		t.Fatalf("LockTx(error) = %v, want wrapped boom", err)
	}
}

func TestEnsureWritableKeyStatusChecks(t *testing.T) {
	ctx := context.Background()
	readErr := errors.New("read failed")
	for _, tc := range []struct {
		name    string
		row     fakeSecretLockRow
		wantErr error
	}{
		{name: "enabled", row: fakeSecretLockRow{status: "ENABLED"}},
		{name: "missing", row: fakeSecretLockRow{err: pgx.ErrNoRows}, wantErr: ErrWritableKeyUnavailable},
		{name: "read error", row: fakeSecretLockRow{err: readErr}, wantErr: readErr},
		{name: "disabled", row: fakeSecretLockRow{status: "DISABLED"}, wantErr: ErrWritableKeyUnavailable},
		{name: "retired", row: fakeSecretLockRow{status: "ENABLED", retired: true}, wantErr: ErrWritableKeyUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := EnsureWritableKey(ctx, ptrext.Of(fakeSecretLockTx{row: tc.row}), "key-1")
			if tc.wantErr == nil && err != nil {
				t.Fatalf("EnsureWritableKey() error = %v, want nil", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("EnsureWritableKey() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

type fakeSecretLockTx struct {
	execErr error
	row     fakeSecretLockRow
}

func (f *fakeSecretLockTx) Begin(context.Context) (pgx.Tx, error) { return f, nil }
func (f *fakeSecretLockTx) Commit(context.Context) error          { return nil }
func (f *fakeSecretLockTx) Rollback(context.Context) error        { return nil }
func (f *fakeSecretLockTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (f *fakeSecretLockTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (f *fakeSecretLockTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (f *fakeSecretLockTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (f *fakeSecretLockTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("SELECT 1"), f.execErr
}
func (f *fakeSecretLockTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (f *fakeSecretLockTx) QueryRow(context.Context, string, ...any) pgx.Row        { return f.row }
func (f *fakeSecretLockTx) Conn() *pgx.Conn                                         { return nil }

type fakeSecretLockRow struct {
	status  string
	retired bool
	err     error
}

func (r fakeSecretLockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if status, ok := dest[0].(*string); ok {
		*status = r.status
	}
	if retired, ok := dest[1].(*bool); ok {
		*retired = r.retired
	}
	return nil
}
