// SPDX-License-Identifier: Apache-2.0

package retention

import (
	"testing"
	"time"
)

func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy("t1")
	if p.RetainDays != 365 {
		t.Errorf("retain = %d, want 365", p.RetainDays)
	}
	if p.GraceDays != 30 {
		t.Errorf("grace = %d, want 30", p.GraceDays)
	}
	if p.Enabled {
		t.Error("default policy should be disabled")
	}
}

func TestCutoffTime(t *testing.T) {
	p := Policy{RetainDays: 90}
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	cutoff := p.CutoffTime(now)
	want := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)
	if !cutoff.Equal(want) {
		t.Errorf("cutoff = %v, want %v", cutoff, want)
	}
}

func TestPurgeTime(t *testing.T) {
	p := Policy{RetainDays: 90, GraceDays: 30}
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	purge := p.PurgeTime(now)
	want := time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC)
	if !purge.Equal(want) {
		t.Errorf("purge = %v, want %v", purge, want)
	}
}

func TestShouldRun(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		p    Policy
		want bool
	}{
		{"disabled", Policy{Enabled: false}, false},
		{"enabled zero next", Policy{Enabled: true}, true},
		{"enabled past due", Policy{Enabled: true, NextRunAt: now.Add(-time.Hour)}, true},
		{"enabled future", Policy{Enabled: true, NextRunAt: now.Add(time.Hour)}, false},
	}
	for _, tt := range tests {
		got := tt.p.ShouldRun(now)
		if got != tt.want {
			t.Errorf("%s: ShouldRun = %v, want %v", tt.name, got, tt.want)
		}
	}
}
