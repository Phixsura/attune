package checks

import (
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/preflight"
	"github.com/Phixsura/attune/internal/restoredrill"
)

func TestGradeRestoreDrill(t *testing.T) {
	const fresh = 7 * 24 * time.Hour
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
			got := gradeRestoreDrill(tc.ok, tc.status, tc.age, fresh)
			if got.Status != tc.want {
				t.Fatalf("gradeRestoreDrill(ok=%v,status=%q,age=%s).Status = %q, want %q\nmessage: %s",
					tc.ok, tc.status, tc.age, got.Status, tc.want, got.Message)
			}
			if got.Name != "backup:restore_drill" || got.Category != preflight.CategoryBackup {
				t.Fatalf("unexpected name/category: %q / %q", got.Name, got.Category)
			}
		})
	}
}
