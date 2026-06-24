// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/restoredrill"
)

func TestWithRecoveryOpts_Valid(t *testing.T) {
	o, err := withRecoveryOpts(restoredrill.Options{}, "2026-06-24T02:00:00Z", "5m", "24h", "30m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.BackupTakenAt == nil {
		t.Fatal("BackupTakenAt not set")
	}
	if o.RestoreDuration == nil || ptrext.Indirect(o.RestoreDuration) != 5*time.Minute {
		t.Fatalf("RestoreDuration = %v", o.RestoreDuration)
	}
	if o.RPOTarget != 24*time.Hour || o.RTOTarget != 30*time.Minute {
		t.Fatalf("targets = %v / %v", o.RPOTarget, o.RTOTarget)
	}
}

func TestWithRecoveryOpts_EmptyIsNoop(t *testing.T) {
	o, err := withRecoveryOpts(restoredrill.Options{}, "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o.BackupTakenAt != nil || o.RestoreDuration != nil || o.RPOTarget != 0 || o.RTOTarget != 0 {
		t.Fatalf("expected zero options, got %+v", o)
	}
}

func TestWithRecoveryOpts_Invalid(t *testing.T) {
	cases := []struct{ takenAt, dur, rpo, rto string }{
		{"not-a-time", "", "", ""},
		{"", "xyz", "", ""},
		{"", "", "bad", ""},
		{"", "", "", "bad"},
	}
	for _, c := range cases {
		if _, err := withRecoveryOpts(restoredrill.Options{}, c.takenAt, c.dur, c.rpo, c.rto); err == nil {
			t.Fatalf("expected error for %+v", c)
		}
	}
}

func TestOptDur(t *testing.T) {
	if got := optDur(nil); got != "-" {
		t.Fatalf("optDur(nil) = %q, want -", got)
	}
	if got := optDur(ptrext.Of(int64(90))); got != "1m30s" {
		t.Fatalf("optDur(90) = %q, want 1m30s", got)
	}
}

func TestDrillIcon(t *testing.T) {
	for _, tc := range []struct {
		s    restoredrill.Status
		want string
	}{
		{restoredrill.StatusPass, "✓"},
		{restoredrill.StatusWarn, "!"},
		{restoredrill.StatusFail, "✗"},
		{restoredrill.StatusSkip, "-"},
	} {
		if got := drillIcon(tc.s); got != tc.want {
			t.Fatalf("drillIcon(%q) = %q, want %q", tc.s, got, tc.want)
		}
	}
}
