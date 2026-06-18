package enrichmentruntime

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type fakeRow struct {
	values []any
	err    error
}

func (f fakeRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	for i := range dest {
		rv := reflect.ValueOf(dest[i])
		if rv.Kind() != reflect.Pointer || rv.IsNil() {
			panic("unsupported scan destination")
		}
		rv.Elem().Set(reflect.ValueOf(f.values[i]))
	}
	return nil
}

type fakeTx struct {
	row      pgx.Row
	execSQL  []string
	execArgs [][]any
	execErr  error
}

func (f *fakeTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTx) Commit(context.Context) error {
	return nil
}

func (f *fakeTx) Rollback(context.Context) error {
	return nil
}

func (f *fakeTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("not implemented")
}

func (f *fakeTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (f *fakeTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (f *fakeTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTx) Exec(_ context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	f.execArgs = append(f.execArgs, arguments)
	return pgconn.CommandTag{}, f.execErr
}

func (f *fakeTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return f.row
}

func (f *fakeTx) Conn() *pgx.Conn {
	return nil
}

func TestNewAndSanitizeQPS(t *testing.T) {
	t.Parallel()

	repo := New(nil)
	require.NotNil(t, repo)
	require.Nil(t, repo.pool)

	require.Equal(t, 0.0, sanitizeQPS(math.NaN()))
	require.Equal(t, 0.0, sanitizeQPS(math.Inf(1)))
	require.Equal(t, 1.25, sanitizeQPS(1.25))
}

func TestScanPolicyCoversHappyPathAndErrors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 18, 16, 0, 0, 0, time.UTC)
	policy, err := scanPolicy(fakeRow{
		values: []any{
			int32(64), int32(4), int32(8), int64(1000), int64(3000),
			true, 2.5, int32(6),
			uint64(9), now, "admin-1", "adjust",
			"cfg-v1", int32(1), uint64(8),
		},
	})
	require.NoError(t, err)
	require.Equal(t, int32(64), policy.Spec.QueueLen)
	require.Equal(t, time.Second, policy.Spec.BatchWindow)
	require.Equal(t, 3*time.Second, policy.Spec.SweepInterval)
	require.True(t, policy.Spec.LLMRateLimitEnabled)
	require.Equal(t, 2.5, policy.Spec.LLMMaxQPS)
	require.Equal(t, uint32(1), policy.SpecVersion)
	require.Equal(t, uint64(8), policy.LastKnownGoodVersion)

	_, err = scanPolicy(fakeRow{err: pgx.ErrNoRows})
	require.ErrorIs(t, err, ErrPolicyNotFound)

	boom := errors.New("boom")
	_, err = scanPolicy(fakeRow{err: boom})
	require.ErrorIs(t, err, boom)
}

func TestGetPolicyTxCoversFoundMissingAndError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 18, 16, 30, 0, 0, time.UTC)
	foundPolicy, found, err := getPolicyTx(context.Background(), ptrext.Of(fakeTx{
		row: fakeRow{
			values: []any{
				int32(64), int32(4), int32(8), int64(1000), int64(3000),
				true, 2.5, int32(6),
				uint64(2), now, "admin-1", "adjust",
				"cfg-v1", int32(1), uint64(1),
			},
		},
	}))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(2), foundPolicy.Version)

	_, found, err = getPolicyTx(context.Background(), ptrext.Of(fakeTx{row: fakeRow{err: pgx.ErrNoRows}}))
	require.NoError(t, err)
	require.False(t, found)

	boom := errors.New("boom")
	_, found, err = getPolicyTx(context.Background(), ptrext.Of(fakeTx{row: fakeRow{err: boom}}))
	require.ErrorIs(t, err, boom)
	require.True(t, found)
}

func TestUpsertPolicyTxCoversInsertUpdateAndExecError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 18, 17, 0, 0, 0, time.UTC)
	policy := Policy{
		Spec: Spec{
			QueueLen:            64,
			Workers:             4,
			BatchSize:           8,
			BatchWindow:         time.Second,
			SweepInterval:       3 * time.Second,
			LLMRateLimitEnabled: true,
			LLMMaxQPS:           math.NaN(),
			LLMBurst:            6,
		},
		Version:                  3,
		UpdatedAt:                now,
		UpdatedBy:                "admin-1",
		UpdateReason:             "adjust",
		BootstrapSnapshotVersion: "cfg-v1",
		SpecVersion:              1,
		LastKnownGoodVersion:     2,
	}

	insertTx := ptrext.Of(fakeTx{})
	require.NoError(t, upsertPolicyTx(context.Background(), insertTx, policy, false))
	require.Len(t, insertTx.execSQL, 1)
	require.Contains(t, insertTx.execSQL[0], "INSERT INTO enrichment_runtime_policy")
	require.Equal(t, defaultPolicyKey, insertTx.execArgs[0][0])
	require.Equal(t, 0.0, insertTx.execArgs[0][7])

	updateTx := ptrext.Of(fakeTx{})
	require.NoError(t, upsertPolicyTx(context.Background(), updateTx, policy, true))
	require.Len(t, updateTx.execSQL, 1)
	require.Contains(t, updateTx.execSQL[0], "UPDATE enrichment_runtime_policy")

	errTx := ptrext.Of(fakeTx{execErr: errors.New("exec failed")})
	require.EqualError(t, upsertPolicyTx(context.Background(), errTx, policy, true), "exec failed")
}
