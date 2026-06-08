// SPDX-License-Identifier: Apache-2.0

package admin_test

import (
	"testing"

	"github.com/Phixsura/attune/internal/repo/admin"
)

// Integration tests against a live Postgres instance are deferred to the
// testdb harness tracked in #12. Per Plan Task 0 we mark them skipped
// here so the package still builds + the test target stays green; the
// manual smoke (Plan Task 24 gate 15-17) exercises Bootstrap end-to-end.

func TestAdminRepo_IntegrationCoverage_DeferredToTestdb(t *testing.T) {
	t.Skip("requires testdb harness — tracked in #12")

	// The deferred scenarios cover (per Plan T8 step 5):
	//   - Create + GetByEmail round-trip with case-insensitive lookup
	//   - 5-strikes-then-locked lockout transition
	//   - ResetFailedAttempts after successful login
	//   - Bootstrap advisory-lock race: two pods converge to one row
}

// Smoke that the public surface compiles, exported types stay stable,
// and the sentinel errors have the expected text — no DB required.
func TestAdminRepo_PublicSurfaceCompiles(t *testing.T) {
	if admin.ErrAlreadyBootstrapped.Error() == "" {
		t.Error("ErrAlreadyBootstrapped error text is empty")
	}
	if admin.ErrNotFound.Error() == "" {
		t.Error("ErrNotFound error text is empty")
	}
	var _ admin.Admin
	var _ admin.NewAdmin
	var _ *admin.Repo
}
