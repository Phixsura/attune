package checks

import (
	"context"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/preflight"
	"github.com/Phixsura/attune/internal/restoredrill"
)

func TestCheckRestoreDrillSkipsWithoutDatabasePool(t *testing.T) {
	t.Parallel()

	got := checkRestoreDrill(context.Background(), ptrext.Of(preflight.Environment{}))
	if got.Name != "backup:restore_drill" || got.Category != preflight.CategoryBackup {
		t.Fatalf("result identity = %#v, want backup restore-drill", got)
	}
	if got.Status != preflight.StatusSkipped || got.Message != "Database pool not available" {
		t.Fatalf("result = %#v, want skipped missing pool", got)
	}
}

func TestAssessLastRun(t *testing.T) {
	const fresh = restoredrill.DefaultFreshnessWindow
	cases := []struct {
		name   string
		ok     bool
		status restoredrill.Status
		age    time.Duration
		want   preflight.Status
	}{
		{"no drill recorded warns", false, "", 0, preflight.StatusWarn},
		{"recorded failure fails", true, restoredrill.StatusFail, time.Hour, preflight.StatusFail},
		{"recorded warn warns", true, restoredrill.StatusWarn, time.Hour, preflight.StatusWarn},
		{"recent pass passes", true, restoredrill.StatusPass, 2 * 24 * time.Hour, preflight.StatusPass},
		{"stale pass warns", true, restoredrill.StatusPass, 30 * 24 * time.Hour, preflight.StatusWarn},
		{"skipped does not pass", true, restoredrill.StatusSkip, time.Hour, preflight.StatusWarn},
		{"unknown status does not pass", true, restoredrill.Status("bogus"), time.Hour, preflight.StatusWarn},
		{"future timestamp warns", true, restoredrill.StatusPass, -time.Hour, preflight.StatusWarn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := restoredrill.AssessLastRun(tc.ok, restoredrill.LastRun{Status: tc.status}, tc.age, fresh)
			if preflight.Status(got.Status) != tc.want {
				t.Fatalf("AssessLastRun(ok=%v,status=%q,age=%s).Status = %q, want %q\nmessage: %s",
					tc.ok, tc.status, tc.age, got.Status, tc.want, got.Message)
			}
			if tc.ok {
				if got.Message == "" {
					t.Fatal("expected a human-readable message")
				}
			} else if got.Remediation == "" {
				t.Fatal("expected remediation for missing drill")
			}
		})
	}
}
