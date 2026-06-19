package main

import (
	"context"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/pkg/logext"
)

// buildRateLimiter is the per-tenant ingest rate limiter.
// Logs at boot — WARN if disabled (test/migration), INFO otherwise.
// onLimit hook increments the Prometheus counter so Grafana can show
// who's hitting the wall.
//
// Lives in its own file so cmd/attune/main.go stays under the attune
// 300-line discipline.
func buildRateLimiter(cfg *config.Config) *ratelimit.Limiter {
	const where = "main.buildRateLimiter"
	ctx := context.Background()
	if cfg.RateLimitDisabled {
		logext.Warnf(ctx, "[%s] DISABLED — every ingest request bypasses (test/migration only)", where)
	} else {
		logext.Infof(ctx, "[%s] enabled,per_minute:%d,burst:%d",
			where, cfg.RateLimitPerMinute, cfg.RateLimitBurst)
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

// buildPerKeyRateLimiter enforces each API key's own rate_limit_rpm (a no-op for
// keys without one). Shares the per-tenant limiter's disable switch.
func buildPerKeyRateLimiter(cfg *config.Config) *ratelimit.PerKeyLimiter {
	return ratelimit.NewPerKey(
		cfg.RateLimitDisabled,
		func(tenantID string) {
			metrics.APIKeyRateLimitedTotal.WithLabelValues(tenantID).Inc()
		},
	)
}
