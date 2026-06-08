// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"
	"os"

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
		// Defence-in-depth: nudge the operator to unset the bootstrap
		// envs after first start (review M6, #66). The plaintext
		// password in /proc/<pid>/environ on Linux is exposed to any
		// sidecar or debugger running as the same uid; once the admin
		// row exists it must not linger.
		warnBootstrapEnvStillSet(ctx, where)
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
	// Same length floor as the self-service change-password endpoint
	// (#66 review M-2). Fail boot loudly rather than persisting a weak
	// hash that the admin can't strengthen later without re-bootstrapping.
	if len(pass) < minNewPasswordLen {
		return fmt.Errorf(
			"[%s] ATTUNE_BOOTSTRAP_ADMIN_PASSWORD must be at least %d characters (got %d)",
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
	logext.Warnf(ctx, "[%s] created first admin %s — change password and unset ATTUNE_BOOTSTRAP_ADMIN_* env immediately", where, email)
	return nil
}

// warnBootstrapEnvStillSet emits a Warn log on every subsequent start
// when the bootstrap env vars are still populated. We don't fail the
// boot — the deployment may have stalled mid-upgrade — but we want a
// loud-and-loop signal so log dashboards catch it.
func warnBootstrapEnvStillSet(ctx context.Context, where string) {
	stale := []string{}
	for _, name := range []string{
		"ATTUNE_BOOTSTRAP_ADMIN_EMAIL",
		"ATTUNE_BOOTSTRAP_ADMIN_EMAIL_FILE",
		"ATTUNE_BOOTSTRAP_ADMIN_PASSWORD",
		"ATTUNE_BOOTSTRAP_ADMIN_PASSWORD_FILE",
	} {
		if os.Getenv(name) != "" {
			stale = append(stale, name)
		}
	}
	if len(stale) > 0 {
		logext.Warnf(ctx,
			"[%s] ATTUNE_BOOTSTRAP_ADMIN_* still set after first start; "+
				"unset these to keep credentials off /proc/<pid>/environ: %v",
			where, stale)
	}
}
