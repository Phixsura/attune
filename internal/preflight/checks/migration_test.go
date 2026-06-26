// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"testing"

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
