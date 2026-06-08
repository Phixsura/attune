// SPDX-License-Identifier: Apache-2.0

package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Phixsura/attune/internal/infra/database"
)

// Integration coverage (lark rows present + env unset/set) is deferred to
// the #12 testdb harness — Plan Task 24 gate 20 covers it via the manual
// smoke + migration timing exercise.

func TestConfirmLarkDelete_IntegrationCoverage_DeferredToTestdb(t *testing.T) {
	t.Skip("requires testdb harness — tracked in #12")
}

// Unit smoke — verifies the env-bypass branch without needing a DB. The
// guard MUST return nil immediately when the operator has opted in,
// regardless of database state (it never even runs the count query).
func TestConfirmLarkDelete_EnvOptInBypassesDB(t *testing.T) {
	t.Setenv("ATTUNE_CONFIRM_LARK_DELETE", "yes")
	if err := database.ConfirmLarkDelete(context.Background(), nil); err != nil {
		t.Errorf("env opt-in path returned %v; want nil", err)
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
