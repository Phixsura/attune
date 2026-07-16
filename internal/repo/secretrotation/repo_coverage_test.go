// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow pgx fake rows need scan target writes.

package secretrotation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestWithLockedSecretsReturnsPoolError(t *testing.T) {
	r := New(nil)
	if err := r.WithLockedSecrets(context.Background(), true, func(context.Context, Queries) error {
		return nil
	}); err == nil {
		t.Fatalf("WithLockedSecrets(nil pool) error = nil")
	}
}

func TestCheckOneMapsCommandResults(t *testing.T) {
	if err := checkOne(pgconn.NewCommandTag("UPDATE 1"), nil, "update row"); err != nil {
		t.Fatalf("checkOne(update 1) error = %v", err)
	}
	if err := checkOne(pgconn.NewCommandTag("UPDATE 0"), nil, "update row"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("checkOne(update 0) error = %v, want ErrNoRows", err)
	}
	boom := errors.New("boom")
	if err := checkOne(pgconn.NewCommandTag("UPDATE 1"), boom, "update row"); !errors.Is(err, boom) {
		t.Fatalf("checkOne(error) = %v, want boom", err)
	}
}

func TestTxQueriesCheckKeyExists(t *testing.T) {
	ctx := context.Background()
	if err := (ptrext.Of(TxQueries{tx: ptrext.Of(fakeSecretRotationTx{row: fakeSecretRotationRow{values: []any{1}}})})).
		CheckKeyExists(ctx, "key-1"); err != nil {
		t.Fatalf("CheckKeyExists(success) error = %v", err)
	}
	err := (ptrext.Of(TxQueries{tx: ptrext.Of(fakeSecretRotationTx{row: fakeSecretRotationRow{err: pgx.ErrNoRows}})})).
		CheckKeyExists(ctx, "key-1")
	if !errors.Is(err, ErrSecretKeyNotFound) {
		t.Fatalf("CheckKeyExists(no rows) error = %v, want ErrSecretKeyNotFound", err)
	}
	boom := errors.New("query failed")
	err = (ptrext.Of(TxQueries{tx: ptrext.Of(fakeSecretRotationTx{row: fakeSecretRotationRow{err: boom}})})).
		CheckKeyExists(ctx, "key-1")
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "check secret key") {
		t.Fatalf("CheckKeyExists(query error) = %v, want wrapped boom", err)
	}
}

func TestTxQueriesListMethodsMapQueryErrors(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("query failed")
	q := ptrext.Of(TxQueries{tx: ptrext.Of(fakeSecretRotationTx{queryErr: boom})})
	if _, err := q.ListLLMCredentialsForUpdate(ctx); !errors.Is(err, boom) ||
		!strings.Contains(err.Error(), "list llm credentials") {
		t.Fatalf("ListLLMCredentialsForUpdate() error = %v, want wrapped boom", err)
	}
	if _, err := q.ListInboundConfigsForUpdate(ctx); !errors.Is(err, boom) ||
		!strings.Contains(err.Error(), "list inbound configs") {
		t.Fatalf("ListInboundConfigsForUpdate() error = %v, want wrapped boom", err)
	}
}

func TestTxQueriesUpdateAndRetireMethodsMapCommandResults(t *testing.T) {
	ctx := context.Background()
	id := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")

	success := ptrext.Of(TxQueries{tx: ptrext.Of(fakeSecretRotationTx{tag: pgconn.NewCommandTag("UPDATE 1")})})
	if err := success.UpdateLLMCredential(ctx, id, "key-2", []byte("cipher")); err != nil {
		t.Fatalf("UpdateLLMCredential(success) error = %v", err)
	}
	if err := success.UpdateInboundConfig(ctx, id, []byte("config")); err != nil {
		t.Fatalf("UpdateInboundConfig(success) error = %v", err)
	}
	if err := success.RetireKey(ctx, "key-1"); err != nil {
		t.Fatalf("RetireKey(success) error = %v", err)
	}

	missing := ptrext.Of(TxQueries{tx: ptrext.Of(fakeSecretRotationTx{tag: pgconn.NewCommandTag("UPDATE 0")})})
	if err := missing.UpdateLLMCredential(ctx, id, "key-2", []byte("cipher")); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("UpdateLLMCredential(missing) error = %v, want ErrNoRows", err)
	}
	if err := missing.UpdateInboundConfig(ctx, id, []byte("config")); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("UpdateInboundConfig(missing) error = %v, want ErrNoRows", err)
	}
	if err := missing.RetireKey(ctx, "key-1"); !errors.Is(err, ErrSecretKeyNotFound) {
		t.Fatalf("RetireKey(missing) error = %v, want ErrSecretKeyNotFound", err)
	}
}

type fakeSecretRotationTx struct {
	tag      pgconn.CommandTag
	execErr  error
	queryErr error
	row      fakeSecretRotationRow
}

func (f *fakeSecretRotationTx) Begin(context.Context) (pgx.Tx, error) { return f, nil }
func (f *fakeSecretRotationTx) Commit(context.Context) error          { return nil }
func (f *fakeSecretRotationTx) Rollback(context.Context) error        { return nil }
func (f *fakeSecretRotationTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}

func (f *fakeSecretRotationTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}
func (f *fakeSecretRotationTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (f *fakeSecretRotationTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}

func (f *fakeSecretRotationTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	if f.tag.String() == "" {
		f.tag = pgconn.NewCommandTag("UPDATE 1")
	}
	return f.tag, f.execErr
}

func (f *fakeSecretRotationTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, f.queryErr
}
func (f *fakeSecretRotationTx) QueryRow(context.Context, string, ...any) pgx.Row { return f.row }
func (f *fakeSecretRotationTx) Conn() *pgx.Conn                                  { return nil }

type fakeSecretRotationRow struct {
	values []any
	err    error
}

func (r fakeSecretRotationRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		switch d := dest[i].(type) {
		case *int:
			*d = r.values[i].(int)
		}
	}
	return nil
}
