package checks

import (
	"context"
	"fmt"

	"github.com/Phixsura/attune/internal/preflight"
)

func init() {
	preflight.Register(preflight.Check{
		Name:     "worker:enricher",
		Category: preflight.CategoryWorker,
		Run:      checkEnricherWorkers,
	})
}

func checkEnricherWorkers(_ context.Context, env *preflight.Environment) preflight.Result {
	r := preflight.Result{
		Name:     "worker:enricher",
		Category: preflight.CategoryWorker,
	}
	if env.Cfg == nil {
		r.Status = preflight.StatusFail
		r.Message = "Config not loaded"
		return r
	}
	workers := env.Cfg.EnricherWorkers
	if workers <= 0 {
		r.Status = preflight.StatusFail
		r.Message = fmt.Sprintf("Enricher workers set to %d", workers)
		r.Remediation = "Set enricher.workers to a positive integer (default: 3)."
		return r
	}
	r.Status = preflight.StatusPass
	r.Message = fmt.Sprintf("%d enricher worker(s) configured", workers)
	return r
}
