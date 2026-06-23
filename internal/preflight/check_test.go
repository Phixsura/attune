// ptrext:file-allow test fixtures use Environment struct pointers.
package preflight

import (
	"context"
	"testing"
)

func TestWorstStatus_AllPass(t *testing.T) {
	results := []Result{
		{Status: StatusPass},
		{Status: StatusPass},
	}
	if got := WorstStatus(results); got != StatusPass {
		t.Errorf("WorstStatus = %q; want %q", got, StatusPass)
	}
}

func TestWorstStatus_WarnOverridesPass(t *testing.T) {
	results := []Result{
		{Status: StatusPass},
		{Status: StatusWarn},
		{Status: StatusPass},
	}
	if got := WorstStatus(results); got != StatusWarn {
		t.Errorf("WorstStatus = %q; want %q", got, StatusWarn)
	}
}

func TestWorstStatus_FailOverridesAll(t *testing.T) {
	results := []Result{
		{Status: StatusPass},
		{Status: StatusWarn},
		{Status: StatusFail},
		{Status: StatusPass},
	}
	if got := WorstStatus(results); got != StatusFail {
		t.Errorf("WorstStatus = %q; want %q", got, StatusFail)
	}
}

func TestWorstStatus_SkippedIgnored(t *testing.T) {
	results := []Result{
		{Status: StatusSkipped},
		{Status: StatusPass},
	}
	if got := WorstStatus(results); got != StatusPass {
		t.Errorf("WorstStatus = %q; want %q", got, StatusPass)
	}
}

func TestWorstStatus_Empty(t *testing.T) {
	if got := WorstStatus(nil); got != StatusPass {
		t.Errorf("WorstStatus(nil) = %q; want %q", got, StatusPass)
	}
}

func TestRunAll_CancelledContext(t *testing.T) {
	origRegistry := make([]Check, len(registry))
	copy(origRegistry, registry)
	registry = nil
	defer func() { registry = origRegistry }()

	Register(Check{
		Name:     "test:a",
		Category: CategoryConfig,
		Run: func(_ context.Context, _ *Environment) Result {
			return Result{Name: "test:a", Category: CategoryConfig, Status: StatusPass, Message: "ok"}
		},
	})
	Register(Check{
		Name:     "test:b",
		Category: CategoryConfig,
		Run: func(_ context.Context, _ *Environment) Result {
			return Result{Name: "test:b", Category: CategoryConfig, Status: StatusPass, Message: "ok"}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report := RunAll(ctx, &Environment{})
	if report.Status != StatusFail {
		t.Errorf("status = %q; want fail", report.Status)
	}
	if len(report.Checks) != 2 {
		t.Fatalf("checks count = %d; want 2", len(report.Checks))
	}
	for _, c := range report.Checks {
		if c.Status != StatusFail {
			t.Errorf("check %q status = %q; want fail", c.Name, c.Status)
		}
	}
}

func TestRunAll_ExecutesChecks(t *testing.T) {
	origRegistry := make([]Check, len(registry))
	copy(origRegistry, registry)
	registry = nil
	defer func() { registry = origRegistry }()

	Register(Check{
		Name:     "test:pass",
		Category: CategoryConfig,
		Run: func(_ context.Context, _ *Environment) Result {
			return Result{Name: "test:pass", Category: CategoryConfig, Status: StatusPass, Message: "ok"}
		},
	})
	Register(Check{
		Name:     "test:warn",
		Category: CategoryDatabase,
		Run: func(_ context.Context, _ *Environment) Result {
			return Result{Name: "test:warn", Category: CategoryDatabase, Status: StatusWarn, Message: "degraded"}
		},
	})

	report := RunAll(context.Background(), &Environment{})
	if report.Status != StatusWarn {
		t.Errorf("status = %q; want warn", report.Status)
	}
	if len(report.Checks) != 2 {
		t.Fatalf("checks count = %d; want 2", len(report.Checks))
	}
	if report.Elapsed == "" {
		t.Error("elapsed is empty")
	}
}
