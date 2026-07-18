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

func TestRunChecks_ExecutesOnlyNamedChecks(t *testing.T) {
	origRegistry := make([]Check, len(registry))
	copy(origRegistry, registry)
	registry = nil
	defer func() { registry = origRegistry }()

	executed := map[string]bool{}
	Register(Check{
		Name:     "test:selected",
		Category: CategoryConfig,
		Run: func(_ context.Context, _ *Environment) Result {
			executed["test:selected"] = true
			return Result{Name: "test:selected", Category: CategoryConfig, Status: StatusPass, Message: "ok"}
		},
	})
	Register(Check{
		Name:     "test:skipped",
		Category: CategoryDatabase,
		Run: func(_ context.Context, _ *Environment) Result {
			executed["test:skipped"] = true
			return Result{Name: "test:skipped", Category: CategoryDatabase, Status: StatusFail, Message: "should not run"}
		},
	})

	report := RunChecks(context.Background(), &Environment{}, []string{"test:selected", "missing"})
	if report.Status != StatusPass {
		t.Errorf("status = %q; want pass", report.Status)
	}
	if len(report.Checks) != 1 || report.Checks[0].Name != "test:selected" {
		t.Fatalf("checks = %+v; want only test:selected", report.Checks)
	}
	if !executed["test:selected"] {
		t.Error("selected check did not run")
	}
	if executed["test:skipped"] {
		t.Error("unnamed check ran")
	}
	if report.Elapsed == "" {
		t.Error("elapsed is empty")
	}
}

func TestRunChecks_CancelledContextFailsSelectedChecks(t *testing.T) {
	origRegistry := make([]Check, len(registry))
	copy(origRegistry, registry)
	registry = nil
	defer func() { registry = origRegistry }()

	Register(Check{
		Name:     "test:selected",
		Category: CategoryConfig,
		Run: func(_ context.Context, _ *Environment) Result {
			t.Fatal("cancelled context should fail before running selected check")
			return Result{}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	report := RunChecks(ctx, &Environment{}, []string{"test:selected"})
	if report.Status != StatusFail {
		t.Errorf("status = %q; want fail", report.Status)
	}
	if len(report.Checks) != 1 {
		t.Fatalf("checks count = %d; want 1", len(report.Checks))
	}
	if report.Checks[0].Status != StatusFail {
		t.Errorf("selected check status = %q; want fail", report.Checks[0].Status)
	}
}
