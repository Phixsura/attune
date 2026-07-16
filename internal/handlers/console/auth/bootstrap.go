// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/repo/admin"
)

// BootstrapConfig is the YAML-provided first-admin seed.
type BootstrapConfig struct {
	Email    string
	Password string
}

type bootstrapAdminStore interface {
	Count(ctx context.Context) (int, error)
	Bootstrap(ctx context.Context, n admin.NewAdmin) error
}

// BootstrapAdmin runs at startup. If the admins table is empty AND the YAML
// console.bootstrap_admin credentials are set, creates the first admin. If the
// table is empty and credentials are absent, returns a fatal error so the
// operator sees the misconfiguration at boot rather than discovering "console
// is locked" later.
//
// On subsequent starts the table is non-empty: BootstrapAdmin logs the
// skip and returns nil. Bootstrap is therefore idempotent and safe to call
// unconditionally on every start.
func BootstrapAdmin(ctx context.Context, repo bootstrapAdminStore, cfg BootstrapConfig) error {
	const where = "auth.BootstrapAdmin"
	n, err := repo.Count(ctx)
	if err != nil {
		return fmt.Errorf("[%s] count admins: %w", where, err)
	}
	if n > 0 {
		logext.Infof(ctx, "[%s] %d admin(s) exist, skipping bootstrap", where, n)
		return nil
	}
	email := cfg.Email
	pass := cfg.Password
	if email == "" || pass == "" {
		return fmt.Errorf(
			"[%s] no admins exist and console.bootstrap_admin.{email,password} are unset; "+
				"console is unreachable until both are provided in config",
			where,
		)
	}
	// Same length floor as the self-service change-password endpoint
	// (#66 review M-2). Fail boot loudly rather than persisting a weak
	// hash that the admin can't strengthen later without re-bootstrapping.
	if len(pass) < minNewPasswordLen {
		return fmt.Errorf(
			"[%s] console.bootstrap_admin.password must be at least %d characters (got %d)",
			where, minNewPasswordLen, len(pass),
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
	logext.Warnf(ctx, "[%s] created first admin %s — change password after first login", where, email)
	return nil
}
