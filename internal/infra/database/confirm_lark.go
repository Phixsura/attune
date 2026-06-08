// SPDX-License-Identifier: Apache-2.0

package database

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrDestructiveMigrationGuard surfaces when ConfirmLarkDelete refuses to
// proceed because user_feedback contains lark-* rows AND the operator
// has not opted in via ATTUNE_CONFIRM_LARK_DELETE=yes. cmd/attune treats
// this as a fatal startup error so the operator sees the count and the
// path forward immediately.
var ErrDestructiveMigrationGuard = errors.New("database: destructive lark-delete guard active")

// ConfirmLarkDelete — run BEFORE the lark-removing migration in
// migrations/ runs (#66 Plan T19). Aborts if there are any lark-*
// user_feedback rows AND ATTUNE_CONFIRM_LARK_DELETE != "yes". Wraps the
// count into the returned error so operators see exactly how many rows
// they would lose.
//
// Returns nil if:
//   - ATTUNE_CONFIRM_LARK_DELETE = "yes" (operator explicitly opted in), or
//   - The lark-* row count is zero (fresh install or already migrated).
func ConfirmLarkDelete(ctx context.Context, pool *pgxpool.Pool) error {
	if os.Getenv("ATTUNE_CONFIRM_LARK_DELETE") == "yes" {
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
			"set ATTUNE_CONFIRM_LARK_DELETE=yes to proceed, or export those rows first "+
			"(see CHANGELOG.md ### Removed for the upgrade preflight)",
		ErrDestructiveMigrationGuard, n,
	)
}
