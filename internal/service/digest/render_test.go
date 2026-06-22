// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"testing"
)

func TestRenderSparkline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		counts []int
		want   string
	}{
		{"empty slice", nil, ""},
		{"all zeros", []int{0, 0, 0}, "▁▁▁"},
		{"uniform values", []int{5, 5, 5, 5}, "████"},
		{"ascending", []int{0, 2, 4, 6, 8}, "▁▂▄▆█"},
		{"descending", []int{8, 6, 4, 2, 0}, "█▆▄▂▁"},
		{"single value", []int{10}, "█"},
		{"spike in middle", []int{1, 1, 10, 1, 1}, "▁▁█▁▁"},
		{"varying values", []int{3, 1, 4, 1, 5, 9, 2, 6}, "▃▁▄▁▄█▂▅"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderSparkline(tt.counts)
			if got != tt.want {
				t.Errorf("renderSparkline(%v) = %q, want %q", tt.counts, got, tt.want)
			}
		})
	}
}

func TestDeltaArrow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		delta DeltaValue
		want  string
	}{
		{"up with positive change", DeltaValue{Direction: "up", Change: 5}, "↑5"},
		{"up with zero change", DeltaValue{Direction: "up", Change: 0}, "↑"},
		{"down with negative change", DeltaValue{Direction: "down", Change: -3}, "↓3"},
		{"down with zero change", DeltaValue{Direction: "down", Change: 0}, "↓"},
		{"flat direction", DeltaValue{Direction: "flat", Change: 0}, ""},
		{"empty direction", DeltaValue{Direction: "", Change: 10}, ""},
		{"unknown direction", DeltaValue{Direction: "unknown", Change: 5}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deltaArrow(tt.delta)
			if got != tt.want {
				t.Errorf("deltaArrow(%+v) = %q, want %q", tt.delta, got, tt.want)
			}
		})
	}
}

func TestLifecycleBadge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		lc   ThemeLifecycle
		want string
	}{
		{"new lifecycle", LifecycleNew, "[NEW] "},
		{"regressed lifecycle", LifecycleRegressed, "[BACK] "},
		{"ongoing lifecycle", LifecycleOngoing, ""},
		{"empty lifecycle", ThemeLifecycle(""), ""},
		{"unknown lifecycle", ThemeLifecycle("unknown"), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lifecycleBadge(tt.lc)
			if got != tt.want {
				t.Errorf("lifecycleBadge(%q) = %q, want %q", tt.lc, got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"empty string", "", 10, ""},
		{"shorter than limit", "hello", 10, "hello"},
		{"exactly at limit", "hello", 5, "hello"},
		{"needs truncation", "hello world", 5, "hello…"},
		{"zero limit", "hello", 0, "…"},
		{"single char limit", "hello", 1, "h…"},
		{"unicode string no truncate", "你好世界", 20, "你好世界"},
		{"unicode string truncate", "hello world", 8, "hello wo…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}
