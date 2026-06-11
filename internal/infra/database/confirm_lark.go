// SPDX-License-Identifier: Apache-2.0

package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrDestructiveMigrationGuard surfaces when ConfirmLarkDelete refuses to
// proceed because user_feedback contains lark-* rows AND the operator has not
// opted in via migrations.confirm_lark_delete. cmd/attune treats this as a
// fatal startup error so the operator sees the count and the path forward
// immediately.
var ErrDestructiveMigrationGuard = errors.New("database: destructive lark-delete guard active")

// ConfirmLarkDelete — run BEFORE the lark-removing migration in
// migrations/ runs (#66 Plan T19). Aborts if there are any lark-* user_feedback
// rows AND confirmed is false. Wraps the count into the returned error so
// operators see exactly how many rows they would lose.
//
// Returns nil if:
//   - confirmed is true (operator explicitly opted in), or
//   - The lark-* row count is zero (fresh install or already migrated).
func ConfirmLarkDelete(ctx context.Context, pool *pgxpool.Pool, confirmed bool) error {
	if confirmed {
		return nil
	}
	// Fresh install: user_feedback table does not yet exist (migration
	// 001_init.sql creates it). No lark-* rows possible → no guard
	// trigger. Without this short-circuit a brand-new deploy would
	// fail boot on `relation "user_feedback" does not exist` (review
	// B3, #66).
	var tableExists bool
	if err := pool.QueryRow(ctx,
		`SELECT to_regclass('public.user_feedback') IS NOT NULL`,
	).Scan(&tableExists); err != nil {
		return fmt.Errorf("ConfirmLarkDelete check table: %w", err)
	}
	if !tableExists {
		return nil
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_feedback WHERE source LIKE 'lark-%'`,
	).Scan(&n); err != nil {
		return fmt.Errorf("ConfirmLarkDelete count: %w", err)
	}
	if n == 0 {
		return nil
	}
	return fmt.Errorf(
		"%w: refusing to delete %d lark-* user_feedback rows; "+
			"set migrations.confirm_lark_delete=true to proceed, or export those rows first "+
			"(see CHANGELOG.md ### Removed for the upgrade preflight)",
		ErrDestructiveMigrationGuard, n,
	)
}
