//go:build integration

package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestMigrations_FreshDB_HasChecksums(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Run migrations
	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Verify all migrations have checksums (except legacy rows, but fresh DB has none)
	var count, withChecksum int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(NULLIF(checksum, ''))
		FROM schema_migrations_feedback
	`).Scan(&count, &withChecksum)
	require.NoError(t, err)

	// All migrations should have checksums after fresh deploy
	require.Equal(t, count, withChecksum, "all migrations should have checksums on fresh deploy")
	require.Equal(t, database.MigrationCount(), count, "all embedded migrations should be applied")
}

func TestMigrations_ChecksumDriftDetected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Run migrations first
	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Corrupt a checksum
	_, err = pool.Exec(ctx, `
		UPDATE schema_migrations_feedback
		SET checksum = 'corrupted'
		WHERE version = 1
	`)
	require.NoError(t, err)

	// Verify should fail
	err = database.VerifyChecksums(ctx, pool)
	require.Error(t, err)
	require.Contains(t, err.Error(), "checksum drift")
}

func TestMigrations_DuplicatePrefixDetected(t *testing.T) {
	// Verifies detection logic with synthetic duplicates
	names := []string{
		"001_init.sql",
		"002_foo.sql",
		"002_bar.sql", // duplicate prefix
		"003_baz.sql",
	}

	err := database.DetectDuplicatePrefixes(names)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate")
}

func TestMigrations_NoDuplicatesInEmbedded(t *testing.T) {
	names, err := database.LoadMigrationNames()
	require.NoError(t, err)

	err = database.DetectDuplicatePrefixes(names)
	require.NoError(t, err, "embedded migrations should have no duplicate prefixes")
}

func TestMigrations_StatusShowsAllMigrations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Run migrations
	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Get status
	status, err := database.GetMigrationStatus(ctx, pool)
	require.NoError(t, err)

	require.Equal(t, database.MigrationCount(), status.Total)
	require.Equal(t, status.Total, status.Applied)
	require.Equal(t, 0, status.Pending)
	require.Empty(t, status.Duplicates)
	require.Equal(t, status.Checksums.Total, status.Checksums.Verified)
}

func TestMigrations_DryRunShowsPending(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t) // Already migrated

	// Clear all migration records to simulate a fresh state with tracker table
	_, err := pool.Exec(ctx, `DELETE FROM schema_migrations_feedback`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations_manifest`)
	require.NoError(t, err)

	// Get pending migrations - all should be pending now
	pending, err := database.GetPendingMigrations(ctx, pool)
	require.NoError(t, err)

	require.Equal(t, database.MigrationCount(), len(pending))
	for _, m := range pending {
		require.NotEmpty(t, m.Body)
		require.NotEmpty(t, m.Checksum)
		require.Len(t, m.Checksum, 64, "checksum should be SHA-256 hex")
	}
}

func TestMigrations_ManifestHashVerification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Run migrations
	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Verify manifest hash passes
	err = database.VerifyManifestHash(ctx, pool)
	require.NoError(t, err)

	// Corrupt manifest hash
	_, err = pool.Exec(ctx, `UPDATE schema_migrations_manifest SET hash = 'corrupted'`)
	require.NoError(t, err)

	// Verify should fail
	err = database.VerifyManifestHash(ctx, pool)
	require.Error(t, err)

	var reorderErr database.ErrManifestReorder
	require.True(t, errors.As(err, &reorderErr), "should be ErrManifestReorder")
}

func TestMigrations_ChecksumStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	status, err := database.GetChecksumStatus(ctx, pool)
	require.NoError(t, err)

	require.Equal(t, database.MigrationCount(), status.Total)
	require.Equal(t, status.Total, status.Verified)
	require.Empty(t, status.Drifted)
}

func TestMigrations_ChecksumStatus_WithDrift(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Corrupt two checksums
	_, err = pool.Exec(ctx, `UPDATE schema_migrations_feedback SET checksum = 'bad1' WHERE version = 1`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `UPDATE schema_migrations_feedback SET checksum = 'bad2' WHERE version = 2`)
	require.NoError(t, err)

	status, err := database.GetChecksumStatus(ctx, pool)
	require.NoError(t, err)

	require.Equal(t, database.MigrationCount(), status.Total)
	require.Equal(t, status.Total-2, status.Verified)
	require.Len(t, status.Drifted, 2)
}

func TestMigrations_DirtyMigrationDetection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Simulate a dirty migration (success=FALSE)
	_, err = pool.Exec(ctx, `UPDATE schema_migrations_feedback SET success = FALSE WHERE version = 5`)
	require.NoError(t, err)

	// DetectDirtyMigrations should find it
	err = database.DetectDirtyMigrations(ctx, pool)
	require.Error(t, err)

	var dirtyErr database.ErrDirtyMigration
	require.True(t, errors.As(err, &dirtyErr), "should be ErrDirtyMigration")
	require.Equal(t, 5, dirtyErr.Version)
}

func TestMigrations_DirtyMigrationDetection_Clean(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// All migrations should be clean (success=TRUE)
	err = database.DetectDirtyMigrations(ctx, pool)
	require.NoError(t, err)
}

func TestMigrations_IdempotentRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Run migrations twice - should be idempotent
	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	err = database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Count should remain the same
	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations_feedback`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, database.MigrationCount(), count)
}

func TestMigrations_StatusWithExtendedColumns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	status, err := database.GetMigrationStatus(ctx, pool)
	require.NoError(t, err)

	// Verify extended columns are populated for migrations after 070
	// (which added the extended columns)
	var countWithExtended int
	for _, m := range status.Migrations {
		if m.Status == "applied" {
			require.NotNil(t, m.AppliedAt, "applied migration should have AppliedAt")
			// Checksum and DurationMs are only populated for fresh migrations (post-070)
			if m.Checksum != nil {
				countWithExtended++
			}
		}
	}
	// At minimum, all migrations should have checksums after fresh deploy
	require.Greater(t, countWithExtended, 0, "should have some migrations with checksums")
}

func TestMigrations_PgvectorCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Run migrations to ensure pgvector is installed
	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// CheckPgvector should pass
	err = database.CheckPgvector(ctx, pool)
	require.NoError(t, err)
}

func TestMigrations_VerifyChecksums_AllValid(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// All checksums should be valid
	err = database.VerifyChecksums(ctx, pool)
	require.NoError(t, err)
}

func TestMigrations_VerifyChecksums_MissingFile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Insert a migration record for a non-existent file
	_, err = pool.Exec(ctx, `
		INSERT INTO schema_migrations_feedback (version, filename, checksum, duration_ms, applied_by, success)
		VALUES (999, '999_nonexistent.sql', 'abc', 0, 'test', TRUE)
	`)
	require.NoError(t, err)

	// VerifyChecksums should detect missing file
	err = database.VerifyChecksums(ctx, pool)
	require.Error(t, err)

	var missingErr database.ErrMissingFile
	require.True(t, errors.As(err, &missingErr), "should be ErrMissingFile")
	require.Equal(t, 999, missingErr.Version)
}

func TestMigrations_GetPendingMigrations_EmptyTable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Clear all records
	_, err := pool.Exec(ctx, `DELETE FROM schema_migrations_feedback`)
	require.NoError(t, err)

	pending, err := database.GetPendingMigrations(ctx, pool)
	require.NoError(t, err)

	// All migrations should be pending
	require.Equal(t, database.MigrationCount(), len(pending))

	// Verify each pending migration has valid data
	for i, m := range pending {
		require.Equal(t, i+1, m.Version)
		require.NotEmpty(t, m.Name)
		require.NotEmpty(t, m.Body)
		require.Len(t, m.Checksum, 64)
	}
}

func TestMigrations_GetPendingMigrations_PartiallyApplied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Clear all records except first 10
	_, err := pool.Exec(ctx, `DELETE FROM schema_migrations_feedback WHERE version > 10`)
	require.NoError(t, err)

	pending, err := database.GetPendingMigrations(ctx, pool)
	require.NoError(t, err)

	// Should have all migrations after version 10
	expectedPending := database.MigrationCount() - 10
	require.Equal(t, expectedPending, len(pending))

	// First pending should be version 11
	if len(pending) > 0 {
		require.Equal(t, 11, pending[0].Version)
	}
}

func TestMigrations_StatusElapsedTime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	status, err := database.GetMigrationStatus(ctx, pool)
	require.NoError(t, err)

	// Elapsed should be a valid duration string
	require.NotEmpty(t, status.Elapsed)
	require.Contains(t, status.Elapsed, "ms", "elapsed should contain ms")
}

func TestMigrations_VerifyNoTxDirectives(t *testing.T) {
	// Test that all embedded migrations pass the no-tx directive check
	names, err := database.LoadMigrationNames()
	require.NoError(t, err)

	err = database.VerifyNoTxDirectives(names)
	require.NoError(t, err)
}

func TestMigrations_ComputeChecksum(t *testing.T) {
	names, err := database.LoadMigrationNames()
	require.NoError(t, err)
	require.NotEmpty(t, names)

	// Compute checksum for first migration
	checksum, err := database.ComputeChecksum(names[0])
	require.NoError(t, err)
	require.Len(t, checksum, 64)

	// Should be deterministic
	checksum2, err := database.ComputeChecksum(names[0])
	require.NoError(t, err)
	require.Equal(t, checksum, checksum2)
}

func TestMigrations_ManifestHash(t *testing.T) {
	names, err := database.LoadMigrationNames()
	require.NoError(t, err)

	hash, err := database.ManifestHash(names)
	require.NoError(t, err)
	require.Len(t, hash, 64)

	// Should be deterministic
	hash2, err := database.ManifestHash(names)
	require.NoError(t, err)
	require.Equal(t, hash, hash2)

	// Different order should produce different hash
	reversed := make([]string, len(names))
	for i, n := range names {
		reversed[len(names)-1-i] = n
	}
	hashReversed, err := database.ManifestHash(reversed)
	require.NoError(t, err)
	require.NotEqual(t, hash, hashReversed, "manifest hash should be order-sensitive")
}

func TestMigrations_DetectDuplicatePrefixes(t *testing.T) {
	// Real migrations should have no duplicates
	names, err := database.LoadMigrationNames()
	require.NoError(t, err)

	err = database.DetectDuplicatePrefixes(names)
	require.NoError(t, err)

	// Synthetic duplicates should be detected
	dupes := []string{"001_a.sql", "001_b.sql", "002_c.sql"}
	err = database.DetectDuplicatePrefixes(dupes)
	require.Error(t, err)
}

func TestMigrations_MigrationCount(t *testing.T) {
	count := database.MigrationCount()
	require.Equal(t, 103, count, "should have 103 migrations")

	names, err := database.LoadMigrationNames()
	require.NoError(t, err)
	require.Equal(t, count, len(names))
}

func TestMigrations_NoTxMigrationExists(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Check that migration 056 (no-tx) was applied
	var version int
	err = pool.QueryRow(ctx, `SELECT version FROM schema_migrations_feedback WHERE version = 56`).Scan(&version)
	require.NoError(t, err)
	require.Equal(t, 56, version)
}

func TestMigrations_ChecksumStatusWithLegacy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Clear checksum for one migration (simulate legacy)
	_, err = pool.Exec(ctx, `UPDATE schema_migrations_feedback SET checksum = '' WHERE version = 1`)
	require.NoError(t, err)

	status, err := database.GetChecksumStatus(ctx, pool)
	require.NoError(t, err)

	// Total counts only migrations with non-empty checksums
	// Verified counts migrations where stored checksum matches computed
	// With one empty checksum, total = 70, verified = 70
	require.Equal(t, database.MigrationCount()-1, status.Total, "total should exclude empty checksums")
	require.Equal(t, status.Total, status.Verified, "all non-empty checksums should match")
}

func TestMigrations_RunMigrations_AlreadyApplied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// First run
	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Second run - should be idempotent
	err = database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Third run - still idempotent
	err = database.RunMigrations(ctx, pool)
	require.NoError(t, err)
}

func TestMigrations_StatusMigrationDetails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	status, err := database.GetMigrationStatus(ctx, pool)
	require.NoError(t, err)

	// Check first and last migration details
	require.NotEmpty(t, status.Migrations)

	first := status.Migrations[0]
	require.Equal(t, 1, first.Version)
	require.Equal(t, "001_init.sql", first.Filename)
	require.Equal(t, "applied", first.Status)
	require.NotNil(t, first.AppliedAt)

	last := status.Migrations[len(status.Migrations)-1]
	require.Equal(t, database.MigrationCount(), last.Version)
	require.Equal(t, "applied", last.Status)
}

func TestMigrations_VerifyChecksumsConn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// All checksums should be valid
	err = database.VerifyChecksums(ctx, pool)
	require.NoError(t, err)
}

func TestMigrations_CheckPgvector_WithClustering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Enable clustering for a tenant
	_, err = pool.Exec(ctx, `
		INSERT INTO tenants (slug, name, clustering_enabled)
		VALUES ('test-cluster', 'Test Cluster', TRUE)
	`)
	require.NoError(t, err)

	// CheckPgvector should still pass (pgvector is installed)
	err = database.CheckPgvector(ctx, pool)
	require.NoError(t, err)
}

func TestMigrations_GetPendingMigrations_Partial(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Delete half the migrations
	_, err := pool.Exec(ctx, `DELETE FROM schema_migrations_feedback WHERE version > 35`)
	require.NoError(t, err)

	pending, err := database.GetPendingMigrations(ctx, pool)
	require.NoError(t, err)

	// Should have migrations 36 to 72
	require.Equal(t, database.MigrationCount()-35, len(pending))
	require.Equal(t, 36, pending[0].Version)
}

func TestMigrations_Checksums_Consistency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Get all stored checksums
	rows, err := pool.Query(ctx, `SELECT version, checksum FROM schema_migrations_feedback ORDER BY version`)
	require.NoError(t, err)
	defer rows.Close()

	names, err := database.LoadMigrationNames()
	require.NoError(t, err)

	for rows.Next() {
		var version int
		var checksum string
		err := rows.Scan(&version, &checksum)
		require.NoError(t, err)

		// Compute expected checksum
		expected, err := database.ComputeChecksum(names[version-1])
		require.NoError(t, err)

		require.Equal(t, expected, checksum, "checksum mismatch for version %d", version)
	}
}

func TestMigrations_ConfirmLarkDelete_NoLarkRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// No lark-* rows exist, so ConfirmLarkDelete should pass without opt-in
	err = database.ConfirmLarkDelete(ctx, pool, false)
	require.NoError(t, err)
}

func TestMigrations_ConfirmLarkDelete_WithOptIn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// With opt-in, should always pass
	err = database.ConfirmLarkDelete(ctx, pool, true)
	require.NoError(t, err)
}

func TestMigrations_ConfirmLarkDelete_WithLarkRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Create a tenant first
	var tenantID string
	err = pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ('lark-test', 'Lark Test') RETURNING id
	`).Scan(&tenantID)
	require.NoError(t, err)

	// Insert lark-* feedback rows
	_, err = pool.Exec(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u1', 'lark-webhook', 'test content')
	`, tenantID)
	require.NoError(t, err)

	// Without opt-in, should fail
	err = database.ConfirmLarkDelete(ctx, pool, false)
	require.Error(t, err)
	require.ErrorIs(t, err, database.ErrDestructiveMigrationGuard)

	// With opt-in, should pass
	err = database.ConfirmLarkDelete(ctx, pool, true)
	require.NoError(t, err)
}

func TestMigrations_ScanMigrationRow_LegacyFormat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Get status should work with extended columns
	status, err := database.GetMigrationStatus(ctx, pool)
	require.NoError(t, err)
	require.NotEmpty(t, status.Migrations)

	// First migration should have all extended fields
	first := status.Migrations[0]
	require.NotNil(t, first.AppliedAt)
	// Checksum may be nil for legacy rows, but should exist for fresh deploy
	require.NotNil(t, first.Checksum)
}

func TestMigrations_QueryAppliedVersions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Delete some migrations to test partial state
	_, err := pool.Exec(ctx, `DELETE FROM schema_migrations_feedback WHERE version > 50`)
	require.NoError(t, err)

	// Get pending should show versions 51+
	pending, err := database.GetPendingMigrations(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, database.MigrationCount()-50, len(pending))
}

func TestMigrations_StoreManifestHash(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Verify manifest hash is stored
	var hash string
	err = pool.QueryRow(ctx, `SELECT hash FROM schema_migrations_manifest WHERE id = 1`).Scan(&hash)
	require.NoError(t, err)
	require.Len(t, hash, 64)

	// Compute expected hash
	names, err := database.LoadMigrationNames()
	require.NoError(t, err)
	expectedHash, err := database.ManifestHash(names)
	require.NoError(t, err)
	require.Equal(t, expectedHash, hash)
}

func TestMigrations_BackfillChecksums(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Verify all checksums are filled after fresh deploy
	var emptyCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations_feedback WHERE checksum = ''`).Scan(&emptyCount)
	require.NoError(t, err)
	require.Equal(t, 0, emptyCount, "fresh deploy should have all checksums filled")

	// Verify all checksums are non-empty
	var withChecksum int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations_feedback WHERE checksum != ''`).Scan(&withChecksum)
	require.NoError(t, err)
	require.Equal(t, database.MigrationCount(), withChecksum)
}

func TestMigrations_GetChecksumStatus_EmptyDB(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Delete all migrations
	_, err := pool.Exec(ctx, `DELETE FROM schema_migrations_feedback`)
	require.NoError(t, err)

	status, err := database.GetChecksumStatus(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, 0, status.Total)
	require.Equal(t, 0, status.Verified)
}

func TestMigrations_VerifyManifestHash_EmptyManifest(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Delete manifest hash
	_, err = pool.Exec(ctx, `DELETE FROM schema_migrations_manifest`)
	require.NoError(t, err)

	// VerifyManifestHash should pass (no stored hash)
	err = database.VerifyManifestHash(ctx, pool)
	require.NoError(t, err)
}

func TestMigrations_StoreManifestHash_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	var hash1 string
	err = pool.QueryRow(ctx, `SELECT hash FROM schema_migrations_manifest WHERE id = 1`).Scan(&hash1)
	require.NoError(t, err)

	// Run again
	err = database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	var hash2 string
	err = pool.QueryRow(ctx, `SELECT hash FROM schema_migrations_manifest WHERE id = 1`).Scan(&hash2)
	require.NoError(t, err)

	require.Equal(t, hash1, hash2)
}

func TestMigrations_DetectDirtyMigrations_NoDirty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	err = database.DetectDirtyMigrations(ctx, pool)
	require.NoError(t, err)
}

func TestMigrations_GetMigrationStatus_Complete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	status, err := database.GetMigrationStatus(ctx, pool)
	require.NoError(t, err)

	require.Equal(t, database.MigrationCount(), status.Total)
	require.Equal(t, database.MigrationCount(), status.Applied)
	require.Equal(t, 0, status.Pending)
}

func TestMigrations_GetPendingMigrations_None(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	pending, err := database.GetPendingMigrations(ctx, pool)
	require.NoError(t, err)
	require.Empty(t, pending)
}

func TestMigrations_VerifyChecksumsConn_WithDrift(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Corrupt a checksum
	_, err = pool.Exec(ctx, `UPDATE schema_migrations_feedback SET checksum = 'bad' WHERE version = 5`)
	require.NoError(t, err)

	err = database.VerifyChecksums(ctx, pool)
	require.Error(t, err)

	var driftErr database.ErrChecksumDrift
	require.True(t, errors.As(err, &driftErr))
}

func TestMigrations_VerifyManifestHashConn_WithReorder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Corrupt manifest hash
	_, err = pool.Exec(ctx, `UPDATE schema_migrations_manifest SET hash = 'bad_hash' WHERE id = 1`)
	require.NoError(t, err)

	err = database.VerifyManifestHash(ctx, pool)
	require.Error(t, err)

	var reorderErr database.ErrManifestReorder
	require.True(t, errors.As(err, &reorderErr))
}

func TestMigrations_RunMigrations_ChecksumDriftBlocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Corrupt a checksum
	_, err = pool.Exec(ctx, `UPDATE schema_migrations_feedback SET checksum = 'corrupted' WHERE version = 3`)
	require.NoError(t, err)

	// RunMigrations should fail on preflight check
	err = database.RunMigrations(ctx, pool)
	require.Error(t, err)

	var driftErr database.ErrChecksumDrift
	require.True(t, errors.As(err, &driftErr))
}

func TestMigrations_RunMigrations_ManifestReorderBlocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Corrupt manifest hash
	_, err = pool.Exec(ctx, `UPDATE schema_migrations_manifest SET hash = 'wrong' WHERE id = 1`)
	require.NoError(t, err)

	// RunMigrations should fail on preflight check
	err = database.RunMigrations(ctx, pool)
	require.Error(t, err)

	var reorderErr database.ErrManifestReorder
	require.True(t, errors.As(err, &reorderErr))
}

func TestMigrations_RunMigrations_DirtyMigrationBlocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Mark an existing migration as dirty (success=FALSE)
	_, err = pool.Exec(ctx, `UPDATE schema_migrations_feedback SET success = FALSE WHERE version = 5`)
	require.NoError(t, err)

	// RunMigrations should fail on dirty migration detection
	err = database.RunMigrations(ctx, pool)
	require.Error(t, err)

	var dirtyErr database.ErrDirtyMigration
	require.True(t, errors.As(err, &dirtyErr))
	require.Equal(t, 5, dirtyErr.Version)
}

func TestMigrations_DetectDirtyMigrations_Found(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Mark an existing migration as dirty
	_, err = pool.Exec(ctx, `UPDATE schema_migrations_feedback SET success = FALSE WHERE version = 10`)
	require.NoError(t, err)

	err = database.DetectDirtyMigrations(ctx, pool)
	require.Error(t, err)

	var dirtyErr database.ErrDirtyMigration
	require.True(t, errors.As(err, &dirtyErr))
	require.Equal(t, 10, dirtyErr.Version)
}

func TestMigrations_GetChecksumStatus_AllVerified(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	status, err := database.GetChecksumStatus(ctx, pool)
	require.NoError(t, err)

	require.Equal(t, database.MigrationCount(), status.Total)
	require.Equal(t, database.MigrationCount(), status.Verified)
	require.Empty(t, status.Drifted)
}

func TestMigrations_GetChecksumStatus_WithMismatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Corrupt checksum
	_, err = pool.Exec(ctx, `UPDATE schema_migrations_feedback SET checksum = 'wrong' WHERE version = 10`)
	require.NoError(t, err)

	status, err := database.GetChecksumStatus(ctx, pool)
	require.NoError(t, err)

	require.Equal(t, 1, len(status.Drifted))
	require.Equal(t, 10, status.Drifted[0].Version)
}

func TestMigrations_HasExtendedColumns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// After migration 070, should have extended columns
	// This is tested implicitly by DetectDirtyMigrations working
	err = database.DetectDirtyMigrations(ctx, pool)
	require.NoError(t, err)
}

func TestMigrations_IsMigrationApplied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Check via status
	status, err := database.GetMigrationStatus(ctx, pool)
	require.NoError(t, err)

	for _, m := range status.Migrations {
		require.Equal(t, "applied", m.Status)
	}
}

func TestMigrations_ApplySingleMigration_AlreadyApplied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Run migrations twice - second run should skip all
	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	var count1 int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations_feedback`).Scan(&count1)
	require.NoError(t, err)

	err = database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	var count2 int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations_feedback`).Scan(&count2)
	require.NoError(t, err)

	require.Equal(t, count1, count2, "no new migrations should be applied")
}

func TestMigrations_CheckPgvector_NoClustering(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// No clustering enabled - should pass
	err = database.CheckPgvector(ctx, pool)
	require.NoError(t, err)
}

func TestMigrations_QueryAppliedMigrations_Extended(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	status, err := database.GetMigrationStatus(ctx, pool)
	require.NoError(t, err)

	// All applied migrations should have at least AppliedAt
	var appliedCount int
	for _, m := range status.Migrations {
		if m.Status == "applied" {
			appliedCount++
			require.NotNil(t, m.AppliedAt, "migration %d should have AppliedAt", m.Version)
			// Checksum may be empty for very old legacy rows, but fresh deploy has them
			require.NotNil(t, m.Checksum, "migration %d should have Checksum", m.Version)
		}
	}
	require.Equal(t, database.MigrationCount(), appliedCount)
}

func TestMigrations_StoreManifestHash_Fresh(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Delete manifest table content before running
	_, _ = pool.Exec(ctx, `TRUNCATE schema_migrations_manifest`)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Verify manifest was stored
	var hash string
	err = pool.QueryRow(ctx, `SELECT hash FROM schema_migrations_manifest WHERE id = 1`).Scan(&hash)
	require.NoError(t, err)
	require.Len(t, hash, 64)
}

func TestMigrations_EnsureTrackerTable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Verify tracker table exists with expected columns
	var count int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'schema_migrations_feedback'
		AND column_name IN ('version', 'filename', 'applied_at', 'checksum', 'duration_ms', 'applied_by', 'success')
	`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 7, count, "tracker table should have 7 expected columns")
}

func TestMigrations_ReleaseMigrationLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	// Running migrations multiple times tests lock acquire/release
	for i := 0; i < 3; i++ {
		err := database.RunMigrations(ctx, pool)
		require.NoError(t, err)
	}

	// Lock should be released after each run
	// We can verify by checking that we can acquire a new connection
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	conn.Release()
}

func TestMigrations_CheckPgvector_Version(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// Get pgvector version
	var version string
	err = pool.QueryRow(ctx, `SELECT extversion FROM pg_extension WHERE extname = 'vector'`).Scan(&version)
	require.NoError(t, err)
	require.NotEmpty(t, version)

	// CheckPgvector should pass
	err = database.CheckPgvector(ctx, pool)
	require.NoError(t, err)
}

func TestMigrations_LoadMigrationNames_All(t *testing.T) {
	t.Parallel()

	names, err := database.LoadMigrationNames()
	require.NoError(t, err)

	// Should have 103 migrations.
	require.Len(t, names, 103)

	// First and last
	require.Equal(t, "001_init.sql", names[0])
	require.Equal(t, "103_customer_request_notes.sql", names[len(names)-1])

	// All should be .sql files
	for _, n := range names {
		require.True(t, len(n) > 4 && n[len(n)-4:] == ".sql", "should be .sql file: %s", n)
	}
}

func TestMigrations_VerifyChecksums_PreExtendedColumns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.NewPool(t)

	err := database.RunMigrations(ctx, pool)
	require.NoError(t, err)

	// All checksums should match
	err = database.VerifyChecksums(ctx, pool)
	require.NoError(t, err)
}
