package checks

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/preflight"
)

func init() {
	preflight.Register(preflight.Check{
		Name:     "metrics:registry",
		Category: preflight.CategoryMetrics,
		Run:      checkMetricsRegistry,
	})
}

func checkMetricsRegistry(_ context.Context, _ *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "metrics:registry",
		Category: preflight.CategoryMetrics,
	}

	// Gather all registered metric families to verify the registry is functional.
	families, err := metrics.Registry.Gather()
	if err != nil {
		// CastError from multierr is common when a collector has issues.
		// A partial gather is still useful — treat as warn.
		if len(families) > 0 {
			r.Status = preflight.StatusWarn
			r.Message = fmt.Sprintf("Metrics registry partial: %d families gathered with errors", len(families))
			r.Remediation = "Some metric collectors failed to gather. Check /metrics endpoint output for details."
			return r
		}
		r.Status = preflight.StatusFail
		r.Message = "Metrics registry gather failed"
		r.Remediation = "Verify internal/infra/metrics collectors are registered correctly. Check for duplicate metric names or descriptor conflicts."
		return r
	}

	count := len(families)
	if count == 0 {
		r.Status = preflight.StatusWarn
		r.Message = "No metric families registered"
		r.Remediation = "Metric families are registered at import time. Ensure internal/infra/metrics is imported by the server entrypoint."
		return r
	}

	r.Status = preflight.StatusPass
	r.Message = fmt.Sprintf("%d metric families registered", count)
	return r
}

// Ensure metrics.Registry satisfies the Gatherer interface at compile time.
var _ prometheus.Gatherer = metrics.Registry
