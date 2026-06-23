package checks

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/Phixsura/attune/internal/preflight"
)

func init() {
	preflight.Register(preflight.Check{
		Name:     "migration:pending",
		Category: preflight.CategoryMigration,
		Run:      checkMigrationPending,
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
