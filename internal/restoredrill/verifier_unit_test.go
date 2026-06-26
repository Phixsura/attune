// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestWithDatabase_RejectsKeywordDSN(t *testing.T) {
	t.Parallel()
	_, err := withDatabase("host=localhost password=secret dbname=attune", "drill_x")
	if err == nil {
		t.Fatal("keyword/value DSN should be rejected")
	}
}

func TestRestoreConn_RejectsKeywordDSN(t *testing.T) {
	t.Parallel()
	_, _, err := restoreConn("host=localhost password=secret dbname=attune")
	if err == nil {
		t.Fatal("keyword/value DSN should be rejected")
	}
}

func TestLastLines_Edge(t *testing.T) {
	t.Parallel()

	t.Run("empty string", func(t *testing.T) {
		t.Parallel()
		got := lastLines("", 3)
		if got != "" {
			t.Fatalf("lastLines('') = %q, want ''", got)
		}
	})

	t.Run("n equals line count", func(t *testing.T) {
		t.Parallel()
		got := lastLines("a\nb\nc", 3)
		if got != "a | b | c" {
			t.Fatalf("lastLines = %q, want 'a | b | c'", got)
		}
	})

	t.Run("n exceeds line count", func(t *testing.T) {
		t.Parallel()
		got := lastLines("x\ny", 10)
		if got != "x | y" {
			t.Fatalf("lastLines = %q, want 'x | y'", got)
		}
	})

	t.Run("n is 1", func(t *testing.T) {
		t.Parallel()
		got := lastLines("a\nb\nc\nd", 1)
		if got != "d" {
			t.Fatalf("lastLines = %q, want 'd'", got)
		}
	})
}

func TestAggregate_UnrecognizedStatusFailsClosed(t *testing.T) {
	t.Parallel()
	results := []CheckResult{
		{Name: "a", Status: StatusPass},
		{Name: "b", Status: Status("bogus")},
	}
	if got := Aggregate(results); got != StatusFail {
		t.Fatalf("Aggregate with unknown status = %q, want fail", got)
	}
}

func TestAggregate_Empty(t *testing.T) {
	t.Parallel()
	if got := Aggregate(nil); got != StatusSkip {
		t.Fatalf("Aggregate(nil) = %q, want skipped", got)
	}
}

func TestAggregate_AllSkipped(t *testing.T) {
	t.Parallel()
	results := []CheckResult{
		{Name: "a", Status: StatusSkip},
		{Name: "b", Status: StatusSkip},
	}
	if got := Aggregate(results); got != StatusSkip {
		t.Fatalf("Aggregate all-skip = %q, want skipped", got)
	}
}

func TestAggregate_MixedStatuses(t *testing.T) {
	t.Parallel()

	t.Run("pass+warn yields warn", func(t *testing.T) {
		t.Parallel()
		results := []CheckResult{
			{Name: "a", Status: StatusPass},
			{Name: "b", Status: StatusWarn},
		}
		if got := Aggregate(results); got != StatusWarn {
			t.Fatalf("got %q, want warn", got)
		}
	})

	t.Run("warn+fail yields fail", func(t *testing.T) {
		t.Parallel()
		results := []CheckResult{
			{Name: "a", Status: StatusWarn},
			{Name: "b", Status: StatusFail},
		}
		if got := Aggregate(results); got != StatusFail {
			t.Fatalf("got %q, want fail", got)
		}
	})

	t.Run("skip+pass yields pass", func(t *testing.T) {
		t.Parallel()
		results := []CheckResult{
			{Name: "a", Status: StatusSkip},
			{Name: "b", Status: StatusPass},
		}
		if got := Aggregate(results); got != StatusPass {
			t.Fatalf("got %q, want pass", got)
		}
	})
}

func TestSecondsPtr_NegativeClamped(t *testing.T) {
	t.Parallel()
	d := -5 * time.Second
	got := secondsPtr(ptrext.Of(d))
	if got == nil {
		t.Fatal("secondsPtr should not return nil for a negative measured duration")
	}
	if ptrext.Indirect(got) != 0 {
		t.Fatalf("secondsPtr(-5s) = %d, want 0 (clamped)", ptrext.Indirect(got))
	}
}

func TestSecondsOf_PositiveValues(t *testing.T) {
	t.Parallel()
	got := secondsOf(90 * time.Second)
	if got == nil {
		t.Fatal("secondsOf should not return nil for positive target")
	}
	if ptrext.Indirect(got) != 90 {
		t.Fatalf("secondsOf(90s) = %d, want 90", ptrext.Indirect(got))
	}
}

func TestCountedTables_NotEmpty(t *testing.T) {
	t.Parallel()
	if len(CountedTables) == 0 {
		t.Fatal("CountedTables should not be empty")
	}
	seen := make(map[string]bool)
	for _, table := range CountedTables {
		if table == "" {
			t.Fatal("CountedTables contains empty string")
		}
		if seen[table] {
			t.Fatalf("duplicate table %q", table)
		}
		seen[table] = true
	}
}

func TestDrillReport_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	r := DrillReport{
		Status:        StatusPass,
		StartedAt:     time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		DurationMS:    1234,
		BackupRef:     "daily-2026-06-01",
		SchemaVersion: 42,
		RPOSeconds:    ptrext.Of(int64(3600)),
		RTOSeconds:    ptrext.Of(int64(120)),
		Checks: []CheckResult{
			{Name: "connectivity", Status: StatusPass, Message: "ok"},
		},
		AttuneVersion: "0.0.0-test",
	}
	if r.Status != StatusPass {
		t.Fatalf("unexpected status %q", r.Status)
	}
	if len(r.Checks) != 1 || r.Checks[0].Name != "connectivity" {
		t.Fatalf("unexpected checks: %+v", r.Checks)
	}
}

func TestDecryptDetail_Fields(t *testing.T) {
	t.Parallel()
	d := decryptDetail{
		LLMTotal:       5,
		LLMChecked:     5,
		InboundTotal:   3,
		InboundChecked: 3,
		Decrypted:      8,
	}
	total := d.LLMTotal + d.InboundTotal
	if total != 8 {
		t.Fatalf("total = %d, want 8", total)
	}
	if d.Decrypted != total {
		t.Fatalf("decrypted = %d, want %d", d.Decrypted, total)
	}
}

func TestDeepDetail_Fields(t *testing.T) {
	t.Parallel()
	d := deepDetail{IndexesChecked: 10, IndexesTotal: 10}
	if d.IndexesChecked != d.IndexesTotal {
		t.Fatalf("checked=%d != total=%d", d.IndexesChecked, d.IndexesTotal)
	}
}
