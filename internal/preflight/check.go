// Package preflight implements the production readiness check framework
// for attune. It provides a single registry of checks consumed by both
// the `attune doctor` CLI and the Console system/preflight HTTP endpoint.
package preflight

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/config"
)

// Status represents the outcome of a single preflight check.
// Values align with IETF draft-inadarei-api-health-check-06.
type Status string

const (
	StatusPass    Status = "pass"
	StatusWarn    Status = "warn"
	StatusFail    Status = "fail"
	StatusSkipped Status = "skipped"
)

// Category groups related checks for display purposes.
type Category string

const (
	CategoryConfig     Category = "config"
	CategoryDatabase   Category = "database"
	CategoryMigration  Category = "migration"
	CategoryEncryption Category = "encryption"
	CategoryAuth       Category = "auth"
	CategoryMetrics    Category = "metrics"
	CategoryWorker     Category = "worker"
)

// AllCategories lists categories in display order.
var AllCategories = []Category{
	CategoryConfig,
	CategoryDatabase,
	CategoryMigration,
	CategoryEncryption,
	CategoryAuth,
	CategoryMetrics,
	CategoryWorker,
}

// Result is the outcome of a single check.
type Result struct {
	Name        string   `json:"name"`
	Category    Category `json:"category"`
	Status      Status   `json:"status"`
	Message     string   `json:"message"`
	Remediation string   `json:"remediation,omitempty"`
}

// Report is the aggregate outcome of all checks.
type Report struct {
	Status  Status   `json:"status"`
	Checks  []Result `json:"checks"`
	Elapsed string   `json:"elapsed"`
}

// Environment carries the dependencies checks need without exposing
// the full server wiring. Fields may be nil when the corresponding
// subsystem is unreachable (checks must tolerate this).
type Environment struct {
	Cfg  *config.Config
	Pool *pgxpool.Pool
}

// CheckFunc is the signature every check implements.
type CheckFunc func(ctx context.Context, env *Environment) Result

// Check is a named, categorized preflight check.
type Check struct {
	Name     string
	Category Category
	Run      CheckFunc
}

// WorstStatus returns the most severe status in a slice of results.
// Severity order: fail > warn > pass. Skipped checks are ignored.
func WorstStatus(results []Result) Status {
	worst := StatusPass
	for _, r := range results {
		if r.Status == StatusFail {
			return StatusFail
		}
		if r.Status == StatusWarn && worst != StatusFail {
			worst = StatusWarn
		}
	}
	return worst
}

// RunAll executes every registered check and returns the aggregate report.
// If the context is cancelled, remaining checks are marked as failed.
func RunAll(ctx context.Context, env *Environment) Report {
	start := time.Now()
	checks := Registered()
	results := make([]Result, 0, len(checks))
	for _, c := range checks {
		if ctx.Err() != nil {
			results = append(results, Result{
				Name:     c.Name,
				Category: c.Category,
				Status:   StatusFail,
				Message:  "Skipped — preflight timeout exceeded",
			})
			continue
		}
		results = append(results, c.Run(ctx, env))
	}
	return Report{
		Status:  WorstStatus(results),
		Checks:  results,
		Elapsed: time.Since(start).Truncate(time.Millisecond).String(),
	}
}
