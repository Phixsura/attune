// ptrext:file-allow test fixtures use report struct pointers.
package preflight

import (
	"bytes"
	"strings"
	"testing"
)

func TestFormatText_ContainsOverall(t *testing.T) {
	report := Report{
		Status: StatusPass,
		Checks: []Result{
			{Name: "config:parse", Category: CategoryConfig, Status: StatusPass, Message: "OK"},
		},
		Elapsed: "12ms",
	}
	var buf bytes.Buffer
	FormatText(&buf, report)
	out := buf.String()
	if !strings.Contains(out, "Overall:") {
		t.Errorf("output missing 'Overall:' line:\n%s", out)
	}
	if !strings.Contains(out, "12ms") {
		t.Errorf("output missing elapsed time:\n%s", out)
	}
}

func TestFormatText_ShowsRemediation(t *testing.T) {
	report := Report{
		Status: StatusFail,
		Checks: []Result{
			{
				Name:        "database:connectivity",
				Category:    CategoryDatabase,
				Status:      StatusFail,
				Message:     "Database ping failed",
				Remediation: "Check database.url",
			},
		},
		Elapsed: "5ms",
	}
	var buf bytes.Buffer
	FormatText(&buf, report)
	out := buf.String()
	if !strings.Contains(out, "Check database.url") {
		t.Errorf("output missing remediation text:\n%s", out)
	}
}

func TestFormatText_SkipsRemediationOnPass(t *testing.T) {
	report := Report{
		Status: StatusPass,
		Checks: []Result{
			{
				Name:        "config:parse",
				Category:    CategoryConfig,
				Status:      StatusPass,
				Message:     "OK",
				Remediation: "should not appear",
			},
		},
		Elapsed: "1ms",
	}
	var buf bytes.Buffer
	FormatText(&buf, report)
	if strings.Contains(buf.String(), "should not appear") {
		t.Error("remediation shown for passing check")
	}
}

func TestFormatText_SkippedStatus(t *testing.T) {
	report := Report{
		Status: StatusPass,
		Checks: []Result{
			{Name: "config:parse", Category: CategoryConfig, Status: StatusPass, Message: "OK"},
			{Name: "auth:oidc_reachable", Category: CategoryAuth, Status: StatusSkipped, Message: "OIDC not configured"},
		},
		Elapsed: "1ms",
	}
	var buf bytes.Buffer
	FormatText(&buf, report)
	out := buf.String()
	if !strings.Contains(out, "OIDC not configured") {
		t.Errorf("output missing skipped check message:\n%s", out)
	}
	if !strings.Contains(out, "auth:oidc_reachable") {
		t.Errorf("output missing skipped check name:\n%s", out)
	}
}

func TestFormatText_OverallPassedCount(t *testing.T) {
	report := Report{
		Status: StatusWarn,
		Checks: []Result{
			{Name: "a", Category: CategoryConfig, Status: StatusPass, Message: "OK"},
			{Name: "b", Category: CategoryConfig, Status: StatusPass, Message: "OK"},
			{Name: "c", Category: CategoryConfig, Status: StatusWarn, Message: "warn"},
		},
		Elapsed: "1ms",
	}
	var buf bytes.Buffer
	FormatText(&buf, report)
	out := buf.String()
	if !strings.Contains(out, "2 passed") {
		t.Errorf("output missing '2 passed' count:\n%s", out)
	}
	if !strings.Contains(out, "1 warnings") {
		t.Errorf("output missing '1 warnings' count:\n%s", out)
	}
}

func TestFormatText_SkippedCount(t *testing.T) {
	report := Report{
		Status: StatusPass,
		Checks: []Result{
			{Name: "a", Category: CategoryConfig, Status: StatusPass, Message: "OK"},
			{Name: "b", Category: CategoryAuth, Status: StatusSkipped, Message: "not configured"},
		},
		Elapsed: "1ms",
	}
	var buf bytes.Buffer
	FormatText(&buf, report)
	out := buf.String()
	if !strings.Contains(out, "1 skipped") {
		t.Errorf("output missing '1 skipped' count:\n%s", out)
	}
}

func TestFormatText_GroupsByCategory(t *testing.T) {
	report := Report{
		Status: StatusPass,
		Checks: []Result{
			{Name: "config:parse", Category: CategoryConfig, Status: StatusPass, Message: "OK"},
			{Name: "database:connectivity", Category: CategoryDatabase, Status: StatusPass, Message: "OK"},
		},
		Elapsed: "1ms",
	}
	var buf bytes.Buffer
	FormatText(&buf, report)
	out := buf.String()
	configIdx := strings.Index(out, "Config")
	dbIdx := strings.Index(out, "Database")
	if configIdx < 0 || dbIdx < 0 {
		t.Fatalf("missing category headers:\n%s", out)
	}
	if configIdx >= dbIdx {
		t.Error("Config header should appear before Database header")
	}
}
