// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/preflight"
)

func validateConfigSafety(ctx context.Context, cfg *config.Config) error {
	checks := []string{"config:tls_consistency"}
	if cfg != nil && cfg.ConsoleSessionKey != "" {
		checks = append([]string{"config:base_url", "auth:session_key"}, checks...)
	}
	report := preflight.RunChecks(ctx, ptrext.Of(preflight.Environment{Cfg: cfg}), checks)
	return startupSafetyError("config safety", report)
}

func validateBootstrapSafety(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool) error {
	report := preflight.RunChecks(ctx, ptrext.Of(preflight.Environment{Cfg: cfg, Pool: pool}), []string{"auth:bootstrap_admin"})
	return startupSafetyError("bootstrap safety", report)
}

func startupSafetyError(scope string, report preflight.Report) error {
	if report.Status != preflight.StatusFail {
		return nil
	}
	var parts []string
	for _, check := range report.Checks {
		if check.Status != preflight.StatusFail {
			continue
		}
		detail := check.Message
		if check.Remediation != "" {
			detail += "; " + check.Remediation
		}
		parts = append(parts, fmt.Sprintf("%s: %s", check.Name, detail))
	}
	if len(parts) == 0 {
		return fmt.Errorf("%s failed", scope)
	}
	return fmt.Errorf("%s failed: %s", scope, strings.Join(parts, "; "))
}
