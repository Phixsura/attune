package checks

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/preflight"
)

func init() {
	preflight.Register(preflight.Check{
		Name:     "migration:pending",
		Category: preflight.CategoryMigration,
		Run:      checkMigrationPending,
	})
	preflight.Register(preflight.Check{
		Name:     "migration:integrity",
		Category: preflight.CategoryMigration,
		Run:      checkMigrationIntegrity,
	})
}

func checkMigrationPending(ctx context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "migration:pending",
		Category: preflight.CategoryMigration,
	}
	if env.Pool == nil {
		r.Status = preflight.StatusFail
		r.Message = "Database pool not available"
		r.Remediation = "Fix database connectivity first."
		return r
	}

	// Count applied migrations from the tracker table.
	var applied int
	err := env.Pool.QueryRow(ctx,
		`SELECT count(*) FROM schema_migrations_feedback`,
	).Scan(&applied)
	if err != nil {
		r.Status = preflight.StatusFail
		r.Message = "Cannot read migration tracker table"
		r.Remediation = "Run 'attune server' once to apply migrations, or check database permissions."
		return r
	}

	total := int(migrationTotal.Load())
	if total == 0 {
		r.Status = preflight.StatusWarn
		r.Message = "Migration total unknown"
		return r
	}

	pending := total - applied
	if pending > 0 {
		r.Status = preflight.StatusFail
		r.Message = fmt.Sprintf("%d pending migration(s) (%d/%d applied)", pending, applied, total)
		r.Remediation = "Run 'attune server' to apply pending migrations, or run migrations manually."
		return r
	}

	r.Status = preflight.StatusPass
	r.Message = fmt.Sprintf("All migrations applied (%d/%d)", applied, total)
	return r
}

var migrationTotal atomic.Int32

// SetMigrationTotal stores the expected migration count (called once at startup).
func SetMigrationTotal(n int) { migrationTotal.Store(int32(n)) }

// checkMigrationIntegrity verifies checksums and duplicate prefix detection.
func checkMigrationIntegrity(ctx context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "migration:integrity",
		Category: preflight.CategoryMigration,
	}

	// Check for duplicate prefixes (doesn't need database)
	names, err := database.LoadMigrationNames()
	if err != nil {
		r.Status = preflight.StatusFail
		r.Message = "Cannot load migration file list"
		r.Remediation = "Check that the binary is built correctly."
		return r
	}

	if err := database.DetectDuplicatePrefixes(names); err != nil {
		r.Status = preflight.StatusFail
		r.Message = "Duplicate migration prefixes detected"
		r.Remediation = "Renumber migrations to ensure unique prefixes. Run 'attune migrations verify' for details."
		return r
	}

	// Check checksums (needs database)
	if env.Pool == nil {
		r.Status = preflight.StatusSkipped
		r.Message = "Database pool not available; skipping checksum verification"
		return r
	}

	if err := database.VerifyChecksums(ctx, env.Pool); err != nil {
		r.Status = preflight.StatusFail
		r.Message = "Migration checksum drift detected"
		r.Remediation = "Restore original migration files or update stored checksums. See docs/private-deploy.md for recovery steps."
		return r
	}

	r.Status = preflight.StatusPass
	r.Message = "All migration files verified (no duplicates, checksums match)"
	return r
}
