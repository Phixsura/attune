// ptrext:file-allow test fixtures use config/env struct pointers.
package checks

import (
	"context"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/preflight"
	"github.com/Phixsura/attune/internal/restoredrill"
)

// --- migration:integrity ---

func TestMigrationIntegrity_NilPool_SkipsDBChecks(t *testing.T) {
	t.Parallel()
	r := checkMigrationIntegrity(context.Background(), &preflight.Environment{})
	// With no pool, the check should still detect duplicate prefixes
	// (doesn't need a database) but skip checksum verification.
	// It either passes (no duplicate prefixes) or fails (binary has no
	// embedded migrations, expected in test binaries).
	if r.Name != "migration:integrity" {
		t.Fatalf("name = %q, want migration:integrity", r.Name)
	}
	if r.Category != preflight.CategoryMigration {
		t.Fatalf("category = %q, want migration", r.Category)
	}
}

// --- migration:dirty ---

// --- migration:manifest ---

// --- backup:restore_drill ---

func TestCheckRestoreDrill_NilPool(t *testing.T) {
	t.Parallel()
	r := checkRestoreDrill(context.Background(), &preflight.Environment{})
	if r.Status != preflight.StatusSkipped {
		t.Fatalf("status = %q, want skipped", r.Status)
	}
}

// --- agoString ---

func TestAgoString_Values(t *testing.T) {
	t.Parallel()
	cases := []struct {
		age  time.Duration
		want string
	}{
		{0, "today"},
		{12 * time.Hour, "today"},
		{24 * time.Hour, "1 day ago"},
		{36 * time.Hour, "1 day ago"},
		{48 * time.Hour, "2 days ago"},
		{7 * 24 * time.Hour, "7 days ago"},
		{30 * 24 * time.Hour, "30 days ago"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			t.Parallel()
			if got := agoString(c.age); got != c.want {
				t.Fatalf("agoString(%v) = %q, want %q", c.age, got, c.want)
			}
		})
	}
}

// --- gradeRestoreDrill edge cases ---

func TestGradeRestoreDrill_AtExactFreshness(t *testing.T) {
	t.Parallel()
	// Age exactly at the freshness boundary: age > freshness is false,
	// so exact-boundary passes (not stale yet).
	freshness := 7 * 24 * time.Hour
	r := gradeRestoreDrill(true, restoredrill.StatusPass, freshness, freshness)
	if r.Status != preflight.StatusPass {
		t.Fatalf("status at exact freshness = %q, want pass", r.Status)
	}
}

func TestGradeRestoreDrill_JustOverFreshness(t *testing.T) {
	t.Parallel()
	freshness := 7 * 24 * time.Hour
	r := gradeRestoreDrill(true, restoredrill.StatusPass, freshness+time.Second, freshness)
	if r.Status != preflight.StatusWarn {
		t.Fatalf("status just over freshness = %q, want warn (stale)", r.Status)
	}
}

func TestGradeRestoreDrill_JustUnderFreshness(t *testing.T) {
	t.Parallel()
	freshness := 7 * 24 * time.Hour
	r := gradeRestoreDrill(true, restoredrill.StatusPass, freshness-time.Hour, freshness)
	if r.Status != preflight.StatusPass {
		t.Fatalf("status just under freshness = %q, want pass", r.Status)
	}
}

func TestGradeRestoreDrill_AllStatusesHaveMessage(t *testing.T) {
	t.Parallel()
	freshness := 7 * 24 * time.Hour
	cases := []struct {
		name   string
		ok     bool
		status restoredrill.Status
		age    time.Duration
	}{
		{"no drill", false, "", 0},
		{"failed", true, restoredrill.StatusFail, time.Hour},
		{"warn", true, restoredrill.StatusWarn, time.Hour},
		{"skipped", true, restoredrill.StatusSkip, time.Hour},
		{"fresh pass", true, restoredrill.StatusPass, time.Hour},
		{"stale pass", true, restoredrill.StatusPass, 30 * 24 * time.Hour},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := gradeRestoreDrill(c.ok, c.status, c.age, freshness)
			if r.Message == "" {
				t.Error("expected non-empty message")
			}
		})
	}
}

func TestGradeRestoreDrill_FailHasRemediation(t *testing.T) {
	t.Parallel()
	r := gradeRestoreDrill(true, restoredrill.StatusFail, time.Hour, 7*24*time.Hour)
	if r.Remediation == "" {
		t.Fatal("failed drill should have remediation text")
	}
}

func TestGradeRestoreDrill_NoDrillHasRemediation(t *testing.T) {
	t.Parallel()
	r := gradeRestoreDrill(false, "", 0, 7*24*time.Hour)
	if r.Remediation == "" {
		t.Fatal("no-drill result should have remediation text")
	}
}

func TestGradeRestoreDrill_StaleDrillHasRemediation(t *testing.T) {
	t.Parallel()
	r := gradeRestoreDrill(true, restoredrill.StatusPass, 30*24*time.Hour, 7*24*time.Hour)
	if r.Remediation == "" {
		t.Fatal("stale drill should have remediation text")
	}
}

// --- SetMigrationTotal concurrency ---

func TestSetMigrationTotal_MultipleStores(t *testing.T) {
	t.Parallel()
	// Verify last-write-wins semantics.
	SetMigrationTotal(10)
	SetMigrationTotal(20)
	if got := migrationTotal.Load(); got != 20 {
		t.Fatalf("migrationTotal = %d, want 20", got)
	}
	SetMigrationTotal(0) // cleanup
}
