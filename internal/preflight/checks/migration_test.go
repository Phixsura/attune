// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/preflight"
)

func TestMigrationPending_NilPool(t *testing.T) {
	t.Parallel()
	r := checkMigrationPending(context.Background(), &preflight.Environment{})
	require.Equal(t, preflight.StatusFail, r.Status)
	require.NotEmpty(t, r.Remediation)
	require.Contains(t, r.Message, "not available")
}

func TestMigrationPending_NilPoolHasRemediation(t *testing.T) {
	t.Parallel()
	r := checkMigrationPending(context.Background(), &preflight.Environment{})
	require.NotEmpty(t, r.Remediation)
}

func TestSetMigrationTotal_StoresAndLoads(t *testing.T) {
	defer SetMigrationTotal(0)
	SetMigrationTotal(69)
	require.Equal(t, int32(69), migrationTotal.Load())
}

func TestMigrationPending_ResultMetadata(t *testing.T) {
	t.Parallel()
	r := checkMigrationPending(context.Background(), &preflight.Environment{})
	require.Equal(t, "migration:pending", r.Name)
	require.Equal(t, preflight.CategoryMigration, r.Category)
}

// ---------- checkMigrationIntegrity ----------

func TestMigrationIntegrity_NilPool_SkipChecksums(t *testing.T) {
	t.Parallel()
	// With nil pool, integrity should still check for duplicate prefixes
	// (embedded files), then skip the checksum portion. The result depends
	// on whether LoadMigrationNames and DetectDuplicatePrefixes succeed
	// (they should in a correctly-built binary).
	r := checkMigrationIntegrity(context.Background(), &preflight.Environment{})
	// Acceptable outcomes: skipped (checksums skipped) or pass (no dupes, checksums skipped).
	// The key point: nil pool should NOT panic.
	require.Contains(t, []preflight.Status{preflight.StatusSkipped, preflight.StatusPass}, r.Status)
	require.Equal(t, "migration:integrity", r.Name)
	require.Equal(t, preflight.CategoryMigration, r.Category)
}

// ---------- checkMigrationDirty ----------

func TestMigrationDirty_NilPool(t *testing.T) {
	t.Parallel()
	r := checkMigrationDirty(context.Background(), &preflight.Environment{})
	require.Equal(t, preflight.StatusSkipped, r.Status)
	require.Equal(t, "migration:dirty", r.Name)
	require.Equal(t, preflight.CategoryMigration, r.Category)
	require.Contains(t, r.Message, "not available")
}

// ---------- checkMigrationManifest ----------

func TestMigrationManifest_NilPool(t *testing.T) {
	t.Parallel()
	r := checkMigrationManifest(context.Background(), &preflight.Environment{})
	require.Equal(t, preflight.StatusSkipped, r.Status)
	require.Equal(t, "migration:manifest", r.Name)
	require.Equal(t, preflight.CategoryMigration, r.Category)
	require.Contains(t, r.Message, "not available")
}

// ---------- registration ----------

func TestMigrationChecksRegistered(t *testing.T) {
	t.Parallel()
	registered := preflight.Registered()
	names := make(map[string]bool)
	for _, c := range registered {
		names[c.Name] = true
	}
	for _, want := range []string{"migration:pending", "migration:integrity", "migration:dirty", "migration:manifest"} {
		require.True(t, names[want], "expected check %q to be registered", want)
	}
}

func TestMigrationChecksReturnDatabaseFailures(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	env := &preflight.Environment{Pool: newUnreachablePreflightPool(t)}
	SetMigrationTotal(100)
	t.Cleanup(func() { SetMigrationTotal(0) })

	for _, tc := range []struct {
		name string
		run  func(context.Context, *preflight.Environment) preflight.Result
		want string
	}{
		{name: "pending", run: checkMigrationPending, want: "Cannot read migration tracker table"},
		{name: "integrity", run: checkMigrationIntegrity, want: "Migration checksum drift detected"},
		{name: "dirty", run: checkMigrationDirty, want: "Failed to check dirty migrations"},
		{name: "manifest", run: checkMigrationManifest, want: "Failed to verify manifest hash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := tc.run(ctx, env)
			require.Equal(t, preflight.StatusFail, r.Status)
			require.Contains(t, r.Message, tc.want)
		})
	}
}

func newUnreachablePreflightPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	require.NoError(t, err)
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}
