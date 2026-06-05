package main

import (
	"context"
	"log/slog"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
)

// buildRateLimiter is the per-tenant ingest rate limiter (Phase 3.3).
// Logs at boot — WARN if disabled (test/migration), INFO otherwise.
// onLimit hook increments the Prometheus counter so Grafana can show
// who's hitting the wall.
//
// Lives in its own file so cmd/attune/main.go stays under the attune
// 300-line discipline.
func buildRateLimiter(cfg *config.Config) *ratelimit.Limiter {
	ctx := context.Background()
	if cfg.RateLimitDisabled {
		slog.WarnContext(ctx, "rate-limit: DISABLED — every ingest request bypasses (test/migration only)")
	} else {
		slog.InfoContext(ctx, "rate-limit: enabled",
			"per_minute", cfg.RateLimitPerMinute,
			"burst", cfg.RateLimitBurst)
	}
	return ratelimit.New(
		cfg.RateLimitPerMinute,
		cfg.RateLimitBurst,
		cfg.RateLimitDisabled,
		func(tenantID string) {
			metrics.IngestRateLimitTotal.WithLabelValues(tenantID).Inc()
		},
	)
}
