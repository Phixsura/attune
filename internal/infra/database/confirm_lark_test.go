// SPDX-License-Identifier: Apache-2.0

package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Phixsura/attune/internal/infra/database"
)

// Unit smoke — verifies the config-bypass branch without needing a DB. The
// guard MUST return nil immediately when the operator has opted in, regardless
// of database state (it never even runs the count query).
func TestConfirmLarkDelete_ConfigOptInBypassesDB(t *testing.T) {
	if err := database.ConfirmLarkDelete(context.Background(), nil, true); err != nil {
		t.Errorf("config opt-in path returned %v; want nil", err)
	}
}

// Sentinel error text guard.
func TestConfirmLarkDelete_ErrSentinelExported(t *testing.T) {
	if !errors.Is(database.ErrDestructiveMigrationGuard, database.ErrDestructiveMigrationGuard) {
		t.Error("ErrDestructiveMigrationGuard not identity-equal to itself")
	}
	if database.ErrDestructiveMigrationGuard.Error() == "" {
		t.Error("ErrDestructiveMigrationGuard error text is empty")
	}
}
