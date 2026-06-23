//go:build integration

package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestMigrations_FreshDB_HasChecksums(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := testdb.New(t)

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

	pool := testdb.New(t)

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

	pool := testdb.New(t)

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

	pool := testdb.New(t)

	// Don't run migrations, just create tracker table
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations_feedback (
			version INT PRIMARY KEY,
			filename TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	require.NoError(t, err)

	// Get pending migrations
	pending, err := database.GetPendingMigrations(ctx, pool)
	require.NoError(t, err)

	require.Equal(t, database.MigrationCount(), len(pending))
	for _, m := range pending {
		require.NotEmpty(t, m.Body)
		require.NotEmpty(t, m.Checksum)
		require.Len(t, m.Checksum, 64, "checksum should be SHA-256 hex")
	}
}
