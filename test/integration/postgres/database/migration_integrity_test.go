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
