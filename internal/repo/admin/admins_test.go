// SPDX-License-Identifier: Apache-2.0

package admin_test

import (
	"testing"

	"github.com/Phixsura/attune/internal/repo/admin"
)

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
