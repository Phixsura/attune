// SPDX-License-Identifier: Apache-2.0

package checks

import (
	"context"
	"fmt"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/preflight"
	adminrepo "github.com/Phixsura/attune/internal/repo/admin"
)

type bootstrapAdminCounter interface {
	Count(context.Context) (int, error)
}

func init() {
	preflight.Register(preflight.Check{
		Name:     "auth:bootstrap_admin",
		Category: preflight.CategoryAuth,
		Run:      checkBootstrapAdmin,
	})
}

func checkBootstrapAdmin(ctx context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "auth:bootstrap_admin",
		Category: preflight.CategoryAuth,
	}
	if env.Cfg == nil {
		r.Status = preflight.StatusFail
		r.Message = "Config not loaded"
		return r
	}
	if env.Cfg.ConsoleSessionKey == "" {
		r.Status = preflight.StatusSkipped
		r.Message = "Console not enabled"
		return r
	}
	if env.Pool == nil {
		r.Status = preflight.StatusFail
		r.Message = "Database not available"
		r.Remediation = "Ensure database.url is reachable so the admins table can be inspected."
		return r
	}
	return bootstrapAdminResult(ctx, env.Cfg, adminrepo.NewRepo(env.Pool))
}

func bootstrapAdminResult(ctx context.Context, cfg *config.Config, counter bootstrapAdminCounter) preflight.Result {
	r := preflight.Result{
		Name:     "auth:bootstrap_admin",
		Category: preflight.CategoryAuth,
	}
	if cfg == nil {
		r.Status = preflight.StatusFail
		r.Message = "Config not loaded"
		return r
	}
	if counter == nil {
		r.Status = preflight.StatusFail
		r.Message = "Database not available"
		r.Remediation = "Ensure database.url is reachable so the admins table can be inspected."
		return r
	}
	n, err := counter.Count(ctx)
	if err != nil {
		r.Status = preflight.StatusFail
		r.Message = "Unable to inspect admins table"
		r.Remediation = "Ensure database.url is reachable and the attune process can query the admins table."
		return r
	}
	if n > 0 {
		r.Status = preflight.StatusWarn
		if cfg.Console.BootstrapAdmin.Email != "" || cfg.Console.BootstrapAdmin.Password != "" {
			r.Message = fmt.Sprintf("%d admin(s) already exist; bootstrap seed is still configured and will be skipped", n)
			r.Remediation = "Remove console.bootstrap_admin from config after the first admin login."
		} else {
			r.Message = fmt.Sprintf("%d admin(s) already exist", n)
		}
		return r
	}
	email := cfg.Console.BootstrapAdmin.Email
	pass := cfg.Console.BootstrapAdmin.Password
	if email == "" || pass == "" {
		r.Status = preflight.StatusFail
		r.Message = "No admins exist and console.bootstrap_admin.{email,password} are unset"
		r.Remediation = "Set console.bootstrap_admin.email and console.bootstrap_admin.password for the first production start, then clear them after the first admin login."
		return r
	}
	r.Status = preflight.StatusPass
	r.Message = "Bootstrap admin seed configured for first admin creation"
	return r
}
