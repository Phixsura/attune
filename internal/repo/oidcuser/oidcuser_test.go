// SPDX-License-Identifier: Apache-2.0

package oidcuser_test

import (
	"testing"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/repo/oidcuser"
)

// TestOIDCUserRepo_PublicSurfaceCompiles verifies the public surface compiles
// and exported types/errors remain stable — no DB required.
func TestOIDCUserRepo_PublicSurfaceCompiles(t *testing.T) {
	if oidcuser.ErrNotFound.Error() == "" {
		t.Error("ErrNotFound error text is empty")
	}

	// Verify types compile
	var _ domain.OIDCUser
	var _ *oidcuser.Repo
}
