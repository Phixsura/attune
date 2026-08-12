// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/repo/feedback"
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

func TestRenderMarkdownItems(t *testing.T) {
	t.Parallel()

	items := []feedback.DigestFeedbackRow{
		{ID: 1, Title: "Login fails"},
		{ID: 2, Title: "Slow page load"},
	}

	t.Run("without deep link", func(t *testing.T) {
		t.Parallel()
		out := renderMarkdownItems(items, "")
		require.Contains(t, out, "#1 Login fails")
		require.Contains(t, out, "#2 Slow page load")
		require.NotContains(t, out, "[→]")
	})

	t.Run("with deep link", func(t *testing.T) {
		t.Parallel()
		out := renderMarkdownItems(items, "https://app.example.com")
		require.Contains(t, out, "[→](https://app.example.com/feedback/1)")
		require.Contains(t, out, "[→](https://app.example.com/feedback/2)")
	})
}

func TestRenderMarkdownHeader_WithDeltas(t *testing.T) {
	t.Parallel()

	view := DigestView{
		RunDate:   "2024-01-15",
		Sparkline: []int{1, 2, 3, 4, 5, 6, 7},
		Result: Result{
			Stats: feedback.DigestWindowStats{Total: 42, Enriched: 38, Urgent: 3},
		},
		Deltas: Deltas{
			Feedback: DeltaValue{Direction: "up", Change: 5},
			Enriched: DeltaValue{Direction: "down", Change: -2},
			Urgent:   DeltaValue{Direction: "up", Change: 1},
		},
	}

	out := renderMarkdownHeader(view)
	require.Contains(t, out, "2024-01-15")
	require.Contains(t, out, "↑5")
	require.Contains(t, out, "↓2")
	require.Contains(t, out, "3 urgent")
}

func TestRenderMarkdownHeader_NoSparkline(t *testing.T) {
	t.Parallel()

	view := DigestView{
		RunDate: "2024-01-15",
		Result:  Result{Stats: feedback.DigestWindowStats{Total: 10, Enriched: 8}},
	}

	out := renderMarkdownHeader(view)
	require.NotContains(t, out, "7-day trend")
}

func TestRenderMarkdown_WithItems(t *testing.T) {
	t.Parallel()

	view := DigestView{
		RunDate: "2024-01-15",
		Result: Result{
			Stats: feedback.DigestWindowStats{Total: 2, Enriched: 1},
			Items: []feedback.DigestFeedbackRow{
				{ID: 1, Title: "Bug report"},
			},
		},
	}

	out := renderMarkdown(view)
	require.Contains(t, out, "Recent Feedback")
	require.Contains(t, out, "#1 Bug report")
}

func TestRenderPayload_WithItems(t *testing.T) {
	t.Parallel()

	view := DigestView{
		TenantID: "t1",
		RunDate:  "2024-01-15",
		Result: Result{
			Stats: feedback.DigestWindowStats{Total: 1, Enriched: 1},
			Items: []feedback.DigestFeedbackRow{
				{ID: 42, Title: "Item title"},
			},
		},
	}

	data, err := RenderPayload(view)
	require.NoError(t, err)
	s := string(data)
	require.True(t, strings.Contains(s, "t1"))
	require.True(t, strings.Contains(s, "Item title"))
}

func TestRenderMarkdownThemes_SingleCount(t *testing.T) {
	t.Parallel()

	themes := []Theme{
		{Title: "Auth", Count: 1, ExampleTitles: []string{"Login issue"}},
	}

	out := renderMarkdownThemes(themes, "")
	require.Contains(t, out, "1 report\n")
	require.NotContains(t, out, "reports")
}

func TestRenderMarkdownThemes_WithDeepLink(t *testing.T) {
	t.Parallel()

	themes := []Theme{
		{Title: "Auth", Count: 3, ExampleIDs: []int64{1, 2, 3}},
	}

	out := renderMarkdownThemes(themes, "https://app.example.com")
	require.Contains(t, out, "[View →](https://app.example.com/feedback?theme=Auth)")
}

func TestRenderMarkdownAnomaliesSection(t *testing.T) {
	view := DigestView{
		TenantID:     "t1",
		RunDate:      "2026-08-10",
		DeepLinkBase: "https://app.example.com",
		Result: Result{
			Tier:  TierThemeless,
			Stats: feedback.DigestWindowStats{Total: 3},
			Anomalies: []AnomalySummary{
				{SliceDisplay: "severity=critical", Direction: "spike", Observed: 31, ExpectedMed: 12, EventID: "ev-1"},
				{SliceDisplay: "zendesk", Direction: "drop", Observed: 0, ExpectedMed: 9},
			},
		},
	}
	md := renderMarkdown(view)
	if !strings.Contains(md, "## Anomalies") {
		t.Fatalf("anomaly section missing:\n%s", md)
	}
	if !strings.Contains(md, "[SPIKE] severity=critical: observed 31 vs expected 12") {
		t.Fatalf("spike line missing:\n%s", md)
	}
	if !strings.Contains(md, "[DROP] zendesk: observed 0 vs expected 9") {
		t.Fatalf("drop line missing:\n%s", md)
	}
	// Lines deep-link to the event (theme/item precedent); a summary
	// without an event id gets no dangling link.
	if !strings.Contains(md, "[→](https://app.example.com/analytics/anomalies?event=ev-1)") {
		t.Fatalf("anomaly deep link missing:\n%s", md)
	}
	if strings.Contains(md, "?event=)") {
		t.Fatalf("missing event id must not render an empty link:\n%s", md)
	}

	// Section omitted entirely when no anomalies.
	view.Result.Anomalies = nil
	if strings.Contains(renderMarkdown(view), "Anomalies") {
		t.Fatal("empty anomaly list must omit the section")
	}
}
