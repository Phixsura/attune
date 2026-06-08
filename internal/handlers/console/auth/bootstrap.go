// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/repo/admin"
)

// BootstrapAdmin runs at startup. If the admins table is empty AND the
// ATTUNE_BOOTSTRAP_ADMIN_{EMAIL,PASSWORD}[_FILE] envs are set, creates
// the first admin. If empty AND envs are not set, returns a fatal error
// so the operator sees the misconfiguration at boot rather than
// discovering "console is locked" later.
//
// On subsequent starts the table is non-empty: BootstrapAdmin logs the
// skip and returns nil. Bootstrap is therefore idempotent and safe to
// call unconditionally on every start.
func BootstrapAdmin(ctx context.Context, repo *admin.Repo) error {
	const where = "auth.BootstrapAdmin"
	n, err := repo.Count(ctx)
	if err != nil {
		return fmt.Errorf("[%s] count admins: %w", where, err)
	}
	if n > 0 {
		logext.Infof(ctx, "[%s] %d admin(s) exist, skipping bootstrap", where, n)
		return nil
	}
	email := config.GetOrFile("ATTUNE_BOOTSTRAP_ADMIN_EMAIL")
	pass := config.GetOrFile("ATTUNE_BOOTSTRAP_ADMIN_PASSWORD")
	if email == "" || pass == "" {
		return fmt.Errorf(
			"[%s] no admins exist and ATTUNE_BOOTSTRAP_ADMIN_{EMAIL,PASSWORD}[_FILE] are unset; "+
				"console is unreachable until both are provided",
			where,
		)
	}
	hash, err := HashPassword(pass)
	if err != nil {
		return fmt.Errorf("[%s] hash password: %w", where, err)
	}
	if err := repo.Bootstrap(ctx, admin.NewAdmin{
		Email:        email,
		PasswordHash: hash,
		DisplayName:  email,
		Role:         "admin",
	}); err != nil && !errors.Is(err, admin.ErrAlreadyBootstrapped) {
		return fmt.Errorf("[%s] bootstrap: %w", where, err)
	}
	logext.Warnf(ctx, "[%s] created first admin %s — change password and unset ATTUNE_BOOTSTRAP_ADMIN_* env immediately", where, email)
	return nil
}
