// ptrext:file-allow test fixtures use Environment struct pointers.
package preflight

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// --- Status and Category constants ---

func TestStatusConstants(t *testing.T) {
	t.Parallel()

	require.Equal(t, Status("pass"), StatusPass)
	require.Equal(t, Status("warn"), StatusWarn)
	require.Equal(t, Status("fail"), StatusFail)
	require.Equal(t, Status("skipped"), StatusSkipped)
}

func TestCategoryConstants(t *testing.T) {
	t.Parallel()

	require.Equal(t, Category("config"), CategoryConfig)
	require.Equal(t, Category("database"), CategoryDatabase)
	require.Equal(t, Category("migration"), CategoryMigration)
	require.Equal(t, Category("backup"), CategoryBackup)
	require.Equal(t, Category("encryption"), CategoryEncryption)
	require.Equal(t, Category("auth"), CategoryAuth)
	require.Equal(t, Category("metrics"), CategoryMetrics)
	require.Equal(t, Category("worker"), CategoryWorker)
}

func TestAllCategories_Length(t *testing.T) {
	t.Parallel()

	require.Len(t, AllCategories, 8, "expected 8 categories in display order")
}

func TestAllCategories_Order(t *testing.T) {
	t.Parallel()

	expected := []Category{
		CategoryConfig, CategoryDatabase, CategoryMigration, CategoryBackup,
		CategoryEncryption, CategoryAuth, CategoryMetrics, CategoryWorker,
	}
	require.Equal(t, expected, AllCategories)
}

// --- WorstStatus edge cases ---

func TestWorstStatus_OnlySkipped(t *testing.T) {
	t.Parallel()

	results := []Result{
		{Status: StatusSkipped},
		{Status: StatusSkipped},
	}
	require.Equal(t, StatusPass, WorstStatus(results), "all skipped should yield pass")
}

func TestWorstStatus_FailShortCircuits(t *testing.T) {
	t.Parallel()

	results := []Result{
		{Status: StatusFail},
		{Status: StatusPass},
		{Status: StatusWarn},
	}
	require.Equal(t, StatusFail, WorstStatus(results))
}

func TestWorstStatus_SinglePass(t *testing.T) {
	t.Parallel()

	require.Equal(t, StatusPass, WorstStatus([]Result{{Status: StatusPass}}))
}

func TestWorstStatus_SingleWarn(t *testing.T) {
	t.Parallel()

	require.Equal(t, StatusWarn, WorstStatus([]Result{{Status: StatusWarn}}))
}

func TestWorstStatus_SingleFail(t *testing.T) {
	t.Parallel()

	require.Equal(t, StatusFail, WorstStatus([]Result{{Status: StatusFail}}))
}

// --- StatusIcon ---

func TestStatusIcon_AllStatuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status Status
		want   string
	}{
		{StatusPass, "\033[32m"},
		{StatusWarn, "\033[33m"},
		{StatusFail, "\033[31m"},
		{StatusSkipped, "\033[90m"},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			t.Parallel()
			icon := StatusIcon(tc.status)
			require.Contains(t, icon, tc.want, "icon should contain ANSI color code")
		})
	}
}

// --- StatusLabel ---

func TestStatusLabel_AllStatuses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status Status
		substr string
	}{
		{StatusPass, "PASS"},
		{StatusWarn, "WARN"},
		{StatusFail, "FAIL"},
	}
	for _, tc := range cases {
		t.Run(string(tc.status), func(t *testing.T) {
			t.Parallel()
			label := StatusLabel(tc.status)
			require.Contains(t, label, tc.substr)
		})
	}
}

func TestStatusLabel_Unknown(t *testing.T) {
	t.Parallel()

	label := StatusLabel(Status("custom"))
	require.Equal(t, "CUSTOM", label)
}

// --- categoryTitle ---

func TestCategoryTitle_Unknown(t *testing.T) {
	t.Parallel()

	require.Equal(t, "custom", categoryTitle(Category("custom")))
}

// --- countByStatus ---

func TestCountByStatus_Empty(t *testing.T) {
	t.Parallel()

	pass, warn, fail, skipped := countByStatus(nil)
	require.Equal(t, 0, pass)
	require.Equal(t, 0, warn)
	require.Equal(t, 0, fail)
	require.Equal(t, 0, skipped)
}

func TestCountByStatus_Mixed(t *testing.T) {
	t.Parallel()

	results := []Result{
		{Status: StatusPass},
		{Status: StatusPass},
		{Status: StatusWarn},
		{Status: StatusFail},
		{Status: StatusSkipped},
		{Status: StatusSkipped},
	}
	pass, warn, fail, skipped := countByStatus(results)
	require.Equal(t, 2, pass)
	require.Equal(t, 1, warn)
	require.Equal(t, 1, fail)
	require.Equal(t, 2, skipped)
}

func TestCountByStatus_AllPass(t *testing.T) {
	t.Parallel()

	results := []Result{{Status: StatusPass}, {Status: StatusPass}, {Status: StatusPass}}
	pass, warn, fail, skipped := countByStatus(results)
	require.Equal(t, 3, pass)
	require.Equal(t, 0, warn)
	require.Equal(t, 0, fail)
	require.Equal(t, 0, skipped)
}

// --- FormatText coverage ---

func TestFormatText_HeaderLine(t *testing.T) {
	t.Parallel()

	report := Report{
		Status:  StatusPass,
		Checks:  []Result{},
		Elapsed: "1ms",
	}
	var buf bytes.Buffer
	FormatText(&buf, report)
	out := buf.String()
	require.Contains(t, out, "attune preflight")
	require.Contains(t, out, "production readiness")
}

func TestFormatText_EmptyChecks(t *testing.T) {
	t.Parallel()

	report := Report{
		Status:  StatusPass,
		Checks:  []Result{},
		Elapsed: "0ms",
	}
	var buf bytes.Buffer
	FormatText(&buf, report)
	out := buf.String()
	require.Contains(t, out, "0 checks")
	require.Contains(t, out, "0 passed")
}

func TestFormatText_WarnRemediationShown(t *testing.T) {
	t.Parallel()

	report := Report{
		Status: StatusWarn,
		Checks: []Result{
			{
				Name:        "test:warn",
				Category:    CategoryConfig,
				Status:      StatusWarn,
				Message:     "something degraded",
				Remediation: "fix the thing",
			},
		},
		Elapsed: "1ms",
	}
	var buf bytes.Buffer
	FormatText(&buf, report)
	require.Contains(t, buf.String(), "fix the thing")
}

func TestFormatText_PassRemediationHidden(t *testing.T) {
	t.Parallel()

	report := Report{
		Status: StatusPass,
		Checks: []Result{
			{
				Name:        "test:pass",
				Category:    CategoryConfig,
				Status:      StatusPass,
				Message:     "all good",
				Remediation: "should not show",
			},
		},
		Elapsed: "1ms",
	}
	var buf bytes.Buffer
	FormatText(&buf, report)
	require.NotContains(t, buf.String(), "should not show")
}

func TestFormatText_MultipleCategoryHeaders(t *testing.T) {
	t.Parallel()

	report := Report{
		Status: StatusPass,
		Checks: []Result{
			{Name: "a", Category: CategoryConfig, Status: StatusPass, Message: "ok"},
			{Name: "b", Category: CategoryConfig, Status: StatusPass, Message: "ok"},
			{Name: "c", Category: CategoryDatabase, Status: StatusPass, Message: "ok"},
			{Name: "d", Category: CategoryAuth, Status: StatusPass, Message: "ok"},
		},
		Elapsed: "1ms",
	}
	var buf bytes.Buffer
	FormatText(&buf, report)
	out := buf.String()

	// Config header appears once, Database once, Auth once
	require.Equal(t, 1, strings.Count(out, " Config\n"))
	require.Equal(t, 1, strings.Count(out, " Database\n"))
	require.Equal(t, 1, strings.Count(out, " Auth\n"))
}

func TestFormatText_FailureCount(t *testing.T) {
	t.Parallel()

	report := Report{
		Status: StatusFail,
		Checks: []Result{
			{Name: "a", Category: CategoryConfig, Status: StatusFail, Message: "bad"},
			{Name: "b", Category: CategoryConfig, Status: StatusFail, Message: "bad"},
		},
		Elapsed: "5ms",
	}
	var buf bytes.Buffer
	FormatText(&buf, report)
	out := buf.String()
	require.Contains(t, out, "2 failures")
}

// --- RunAll ---

func TestRunAll_NormalExecution(t *testing.T) {
	t.Parallel()

	origRegistry := make([]Check, len(registry))
	copy(origRegistry, registry)
	registry = nil
	defer func() { registry = origRegistry }()

	Register(Check{
		Name:     "test:only",
		Category: CategoryConfig,
		Run: func(_ context.Context, _ *Environment) Result {
			return Result{Name: "test:only", Category: CategoryConfig, Status: StatusPass, Message: "ok"}
		},
	})

	report := RunAll(context.Background(), &Environment{})
	require.Equal(t, StatusPass, report.Status)
	require.Len(t, report.Checks, 1)
	require.NotEmpty(t, report.Elapsed)
}

func TestRunAll_EmptyRegistry(t *testing.T) {
	t.Parallel()

	origRegistry := make([]Check, len(registry))
	copy(origRegistry, registry)
	registry = nil
	defer func() { registry = origRegistry }()

	report := RunAll(context.Background(), &Environment{})
	require.Equal(t, StatusPass, report.Status)
	require.Empty(t, report.Checks)
}

// --- Report/Result JSON ---

func TestResult_Fields(t *testing.T) {
	t.Parallel()

	r := Result{
		Name:        "test:check",
		Category:    CategoryDatabase,
		Status:      StatusFail,
		Message:     "something broke",
		Remediation: "fix it",
	}
	require.Equal(t, "test:check", r.Name)
	require.Equal(t, CategoryDatabase, r.Category)
	require.Equal(t, StatusFail, r.Status)
	require.Equal(t, "something broke", r.Message)
	require.Equal(t, "fix it", r.Remediation)
}

func TestReport_Fields(t *testing.T) {
	t.Parallel()

	report := Report{
		Status: StatusWarn,
		Checks: []Result{
			{Name: "a", Status: StatusPass},
			{Name: "b", Status: StatusWarn},
		},
		Elapsed: "100ms",
	}
	require.Equal(t, StatusWarn, report.Status)
	require.Len(t, report.Checks, 2)
	require.Equal(t, "100ms", report.Elapsed)
}

// --- Registry ---

func TestRegistered_EmptyAfterClear(t *testing.T) {
	t.Parallel()

	orig := registry
	defer func() { registry = orig }()

	registry = nil
	require.Empty(t, Registered())
}

func TestRegister_PreservesOrder(t *testing.T) {
	t.Parallel()

	orig := registry
	defer func() { registry = orig }()

	registry = nil
	Register(Check{Name: "first"})
	Register(Check{Name: "second"})
	Register(Check{Name: "third"})

	got := Registered()
	require.Len(t, got, 3)
	require.Equal(t, "first", got[0].Name)
	require.Equal(t, "second", got[1].Name)
	require.Equal(t, "third", got[2].Name)
}

func TestRegistered_MutationSafe(t *testing.T) {
	t.Parallel()

	orig := registry
	defer func() { registry = orig }()

	registry = nil
	Register(Check{Name: "stable"})

	copy1 := Registered()
	copy1[0].Name = "mutated"

	copy2 := Registered()
	require.Equal(t, "stable", copy2[0].Name, "mutation of copy should not affect registry")
}
