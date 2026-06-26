// ptrext:file-allow test-fixtures
// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// ---------------------------------------------------------------------------
// computeDelta

// ---------------------------------------------------------------------------
// computeDeltas

func TestComputeDeltas(t *testing.T) {
	t.Parallel()

	t.Run("all up", func(t *testing.T) {
		t.Parallel()
		cur := feedback.DigestWindowStats{Total: 20, Enriched: 15, Urgent: 5}
		pri := feedback.DigestWindowStats{Total: 10, Enriched: 8, Urgent: 2}
		got := computeDeltas(cur, pri)
		require.Equal(t, "up", got.Feedback.Direction)
		require.Equal(t, "up", got.Enriched.Direction)
		require.Equal(t, "up", got.Urgent.Direction)
		require.Equal(t, 10, got.Feedback.Change)
		require.Equal(t, 7, got.Enriched.Change)
		require.Equal(t, 3, got.Urgent.Change)
	})

	t.Run("all down", func(t *testing.T) {
		t.Parallel()
		cur := feedback.DigestWindowStats{Total: 5, Enriched: 3, Urgent: 1}
		pri := feedback.DigestWindowStats{Total: 20, Enriched: 15, Urgent: 8}
		got := computeDeltas(cur, pri)
		require.Equal(t, "down", got.Feedback.Direction)
		require.Equal(t, "down", got.Enriched.Direction)
		require.Equal(t, "down", got.Urgent.Direction)
	})

	t.Run("mixed directions", func(t *testing.T) {
		t.Parallel()
		cur := feedback.DigestWindowStats{Total: 20, Enriched: 5, Urgent: 3}
		pri := feedback.DigestWindowStats{Total: 10, Enriched: 15, Urgent: 3}
		got := computeDeltas(cur, pri)
		require.Equal(t, "up", got.Feedback.Direction)
		require.Equal(t, "down", got.Enriched.Direction)
		require.Equal(t, "flat", got.Urgent.Direction)
	})

	t.Run("all flat at zero", func(t *testing.T) {
		t.Parallel()
		cur := feedback.DigestWindowStats{}
		pri := feedback.DigestWindowStats{}
		got := computeDeltas(cur, pri)
		require.Equal(t, "flat", got.Feedback.Direction)
		require.Equal(t, "flat", got.Enriched.Direction)
		require.Equal(t, "flat", got.Urgent.Direction)
	})
}

// ---------------------------------------------------------------------------
// buildSparkline

// ---------------------------------------------------------------------------
// applyLifecycle

// ---------------------------------------------------------------------------
// renderMarkdownHeader
// ---------------------------------------------------------------------------

func TestRenderMarkdownHeader(t *testing.T) {
	t.Parallel()

	t.Run("with sparkline and deltas", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate:   "2026-06-20",
			Result:    Result{Stats: feedback.DigestWindowStats{Total: 50, Enriched: 40, Urgent: 5}},
			Sparkline: []int{1, 2, 3, 4, 5, 6, 7},
			Deltas: Deltas{
				Feedback: DeltaValue{Current: 50, Prior: 30, Change: 20, Direction: "up"},
				Enriched: DeltaValue{Current: 40, Prior: 35, Change: 5, Direction: "up"},
				Urgent:   DeltaValue{Current: 5, Prior: 3, Change: 2, Direction: "up"},
			},
		}
		got := renderMarkdownHeader(view)
		require.Contains(t, got, "Daily Digest — 2026-06-20")
		require.Contains(t, got, "7-day trend:")
		require.Contains(t, got, "**50** feedback")
		require.Contains(t, got, "40 enriched")
		require.Contains(t, got, "**5 urgent**")
		require.Contains(t, got, "↑20")
	})

	t.Run("without sparkline", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate: "2026-06-20",
			Result:  Result{Stats: feedback.DigestWindowStats{Total: 10, Enriched: 8}},
		}
		got := renderMarkdownHeader(view)
		require.Contains(t, got, "Daily Digest — 2026-06-20")
		require.NotContains(t, got, "7-day trend:")
		require.Contains(t, got, "**10** feedback")
	})

	t.Run("without urgent", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate: "2026-06-20",
			Result:  Result{Stats: feedback.DigestWindowStats{Total: 10, Enriched: 8, Urgent: 0}},
		}
		got := renderMarkdownHeader(view)
		require.NotContains(t, got, "urgent")
	})

	t.Run("with deltas but empty direction shows no arrows", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate: "2026-06-20",
			Result:  Result{Stats: feedback.DigestWindowStats{Total: 10, Enriched: 8}},
			Deltas:  Deltas{}, // zero-value: Direction == ""
		}
		got := renderMarkdownHeader(view)
		require.NotContains(t, got, "↑")
		require.NotContains(t, got, "↓")
	})
}

// ---------------------------------------------------------------------------
// renderMarkdownThemes
// ---------------------------------------------------------------------------

func TestRenderMarkdownThemes(t *testing.T) {
	t.Parallel()

	t.Run("single theme", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{
			{Title: "Checkout", Count: 12, ExampleIDs: []int64{1}, ExampleTitles: []string{"broken cart"}},
		}
		got := renderMarkdownThemes(themes, "")
		require.Contains(t, got, "Top Themes")
		require.Contains(t, got, "1. Checkout")
		require.Contains(t, got, "12 reports")
		require.Contains(t, got, `"broken cart"`)
	})

	t.Run("multiple themes", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{
			{Title: "Alpha", Count: 5, ExampleIDs: []int64{1}, ExampleTitles: []string{"ex a"}},
			{Title: "Beta", Count: 3, ExampleIDs: []int64{2}, ExampleTitles: []string{"ex b"}},
		}
		got := renderMarkdownThemes(themes, "")
		require.Contains(t, got, "1. Alpha")
		require.Contains(t, got, "2. Beta")
	})

	t.Run("with deep link", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{
			{Title: "Checkout", Count: 5, ExampleIDs: []int64{1}},
		}
		got := renderMarkdownThemes(themes, "https://app.example.com")
		require.Contains(t, got, "[View →](https://app.example.com/feedback?theme=Checkout)")
	})

	t.Run("without deep link omits link", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{
			{Title: "Checkout", Count: 5, ExampleIDs: []int64{1}},
		}
		got := renderMarkdownThemes(themes, "")
		require.NotContains(t, got, "[View →]")
	})

	t.Run("lifecycle badges", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{
			{Title: "New Theme", Count: 2, Lifecycle: LifecycleNew},
			{Title: "Old Theme", Count: 4, Lifecycle: LifecycleOngoing},
			{Title: "Back Theme", Count: 1, Lifecycle: LifecycleRegressed},
		}
		got := renderMarkdownThemes(themes, "")
		require.Contains(t, got, "[NEW] New Theme")
		require.NotContains(t, got, "[NEW] Old Theme")
		require.Contains(t, got, "[BACK] Back Theme")
	})

	t.Run("singular count uses report not reports", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{
			{Title: "Solo", Count: 1},
		}
		got := renderMarkdownThemes(themes, "")
		require.Contains(t, got, "1 report\n")
		require.NotContains(t, got, "1 reports")
	})

	t.Run("no example titles omits quote", func(t *testing.T) {
		t.Parallel()
		themes := []Theme{
			{Title: "Bare", Count: 3},
		}
		got := renderMarkdownThemes(themes, "")
		require.NotContains(t, got, "> \"")
	})
}

// ---------------------------------------------------------------------------
// renderMarkdownItems

// ---------------------------------------------------------------------------
// renderMarkdown
// ---------------------------------------------------------------------------

func TestRenderMarkdown(t *testing.T) {
	t.Parallel()

	t.Run("themed view includes themes section", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate: "2026-06-20",
			Result: Result{
				Stats:  feedback.DigestWindowStats{Total: 30, Enriched: 25, Urgent: 2, Unclustered: 5},
				Themes: []Theme{{Title: "Checkout", Count: 10, ExampleIDs: []int64{1}}},
			},
		}
		got := renderMarkdown(view)
		require.Contains(t, got, "Top Themes")
		require.Contains(t, got, "Checkout")
		require.Contains(t, got, "+5 unclustered")
		require.NotContains(t, got, "Recent Feedback")
	})

	t.Run("themeless view with items shows recent feedback", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate: "2026-06-20",
			Result: Result{
				Stats: feedback.DigestWindowStats{Total: 3, Enriched: 3},
				Items: []feedback.DigestFeedbackRow{{ID: 1, Title: "item one"}},
			},
		}
		got := renderMarkdown(view)
		require.Contains(t, got, "Recent Feedback")
		require.Contains(t, got, "#1 item one")
		require.NotContains(t, got, "Top Themes")
	})

	t.Run("empty view has header only", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate: "2026-06-20",
			Result: Result{
				Stats: feedback.DigestWindowStats{Total: 0, Enriched: 0},
			},
		}
		got := renderMarkdown(view)
		require.Contains(t, got, "Daily Digest")
		require.NotContains(t, got, "Top Themes")
		require.NotContains(t, got, "Recent Feedback")
		require.NotContains(t, got, "unclustered")
	})

	t.Run("unclustered omitted when zero", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate: "2026-06-20",
			Result: Result{
				Stats:  feedback.DigestWindowStats{Total: 10, Enriched: 10, Unclustered: 0},
				Themes: []Theme{{Title: "X", Count: 5}},
			},
		}
		got := renderMarkdown(view)
		require.NotContains(t, got, "unclustered")
	})

	t.Run("unclustered omitted when no themes", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate: "2026-06-20",
			Result: Result{
				Stats: feedback.DigestWindowStats{Total: 3, Enriched: 3, Unclustered: 2},
				Items: []feedback.DigestFeedbackRow{{ID: 1, Title: "x"}},
			},
		}
		got := renderMarkdown(view)
		require.NotContains(t, got, "unclustered")
	})
}

// ---------------------------------------------------------------------------
// WindowForRunDate
// ---------------------------------------------------------------------------

func TestWindowForRunDate(t *testing.T) {
	t.Parallel()

	t.Run("regular date in UTC", func(t *testing.T) {
		t.Parallel()
		runDate := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
		from, to := WindowForRunDate(runDate, time.UTC)
		require.Equal(t, "2026-06-14T00:00:00Z", from.UTC().Format(time.RFC3339))
		require.Equal(t, "2026-06-15T00:00:00Z", to.UTC().Format(time.RFC3339))
	})

	t.Run("month boundary", func(t *testing.T) {
		t.Parallel()
		runDate := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		from, to := WindowForRunDate(runDate, time.UTC)
		require.Equal(t, "2026-06-30T00:00:00Z", from.UTC().Format(time.RFC3339))
		require.Equal(t, "2026-07-01T00:00:00Z", to.UTC().Format(time.RFC3339))
	})

	t.Run("year boundary", func(t *testing.T) {
		t.Parallel()
		runDate := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
		from, to := WindowForRunDate(runDate, time.UTC)
		require.Equal(t, "2026-12-31T00:00:00Z", from.UTC().Format(time.RFC3339))
		require.Equal(t, "2027-01-01T00:00:00Z", to.UTC().Format(time.RFC3339))
	})

	t.Run("non-UTC location shifts bounds", func(t *testing.T) {
		t.Parallel()
		sh, err := time.LoadLocation("Asia/Shanghai")
		require.NoError(t, err)
		// Run date is June 15 local in Shanghai.
		runDate := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
		from, to := WindowForRunDate(runDate, sh)
		// from = June 14 00:00 +08 = June 13 16:00 UTC
		// to   = June 15 00:00 +08 = June 14 16:00 UTC
		require.Equal(t, "2026-06-13T16:00:00Z", from.UTC().Format(time.RFC3339))
		require.Equal(t, "2026-06-14T16:00:00Z", to.UTC().Format(time.RFC3339))
	})
}

// ---------------------------------------------------------------------------
// normalizeWeekday
// ---------------------------------------------------------------------------

func TestNormalizeWeekday(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		w    int
		want time.Weekday
	}{
		{"Sunday 0", 0, time.Sunday},
		{"Monday 1", 1, time.Monday},
		{"Tuesday 2", 2, time.Tuesday},
		{"Wednesday 3", 3, time.Wednesday},
		{"Thursday 4", 4, time.Thursday},
		{"Friday 5", 5, time.Friday},
		{"Saturday 6", 6, time.Saturday},
		{"7 wraps to Sunday", 7, time.Sunday},
		{"8 wraps to Monday", 8, time.Monday},
		{"14 wraps to Sunday", 14, time.Sunday},
		{"-1 wraps to Saturday", -1, time.Saturday},
		{"-7 wraps to Sunday", -7, time.Sunday},
		{"-8 wraps to Saturday", -8, time.Saturday},
		{"large positive 100", 100, time.Weekday(((100 % 7) + 7) % 7)},
		{"large negative -100", -100, time.Weekday(((-100 % 7) + 7) % 7)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeWeekday(tt.w)
			require.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// nextLocalDay
// ---------------------------------------------------------------------------

func TestNextLocalDay(t *testing.T) {
	t.Parallel()

	t.Run("regular day advance", func(t *testing.T) {
		t.Parallel()
		cand := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)
		got := nextLocalDay(cand, 9, time.UTC)
		require.Equal(t, 2026, got.Year())
		require.Equal(t, time.June, got.Month())
		require.Equal(t, 16, got.Day())
		require.Equal(t, 9, got.Hour())
	})

	t.Run("month boundary June to July", func(t *testing.T) {
		t.Parallel()
		cand := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
		got := nextLocalDay(cand, 10, time.UTC)
		require.Equal(t, time.July, got.Month())
		require.Equal(t, 1, got.Day())
	})

	t.Run("year boundary December to January", func(t *testing.T) {
		t.Parallel()
		cand := time.Date(2026, 12, 31, 8, 0, 0, 0, time.UTC)
		got := nextLocalDay(cand, 8, time.UTC)
		require.Equal(t, 2027, got.Year())
		require.Equal(t, time.January, got.Month())
		require.Equal(t, 1, got.Day())
	})

	t.Run("leap year Feb 28 to Feb 29", func(t *testing.T) {
		t.Parallel()
		cand := time.Date(2024, 2, 28, 9, 0, 0, 0, time.UTC)
		got := nextLocalDay(cand, 9, time.UTC)
		require.Equal(t, 29, got.Day())
		require.Equal(t, time.February, got.Month())
	})

	t.Run("non-leap year Feb 28 to Mar 1", func(t *testing.T) {
		t.Parallel()
		cand := time.Date(2026, 2, 28, 9, 0, 0, 0, time.UTC)
		got := nextLocalDay(cand, 9, time.UTC)
		require.Equal(t, time.March, got.Month())
		require.Equal(t, 1, got.Day())
	})

	t.Run("DST spring forward preserves local hour", func(t *testing.T) {
		t.Parallel()
		ny, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)
		// Day before US spring-forward 2026 (2026-03-08)
		cand := time.Date(2026, 3, 7, 9, 0, 0, 0, ny)
		got := nextLocalDay(cand, 9, ny)
		require.Equal(t, 8, got.Day())
		require.Equal(t, 9, got.Hour())
		// Before DST: EST (UTC-5), after: EDT (UTC-4)
		_, offset := got.Zone()
		require.Equal(t, -4*3600, offset, "should be EDT after spring forward")
	})
}

// ---------------------------------------------------------------------------
// toOutboundTarget

// ---------------------------------------------------------------------------
// newSender

// ---------------------------------------------------------------------------
// RenderPayload (additional coverage)
// ---------------------------------------------------------------------------

func TestRenderPayload_Themed(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	view := DigestView{
		TenantID: "acme",
		RunDate:  "2026-06-20",
		From:     from,
		To:       to,
		Result: Result{
			Tier:  TierThemed,
			Stats: feedback.DigestWindowStats{Total: 50, Enriched: 45, Urgent: 3, Unclustered: 5},
			Themes: []Theme{
				{Title: "Checkout", Count: 15, ExampleIDs: []int64{100, 101}, ExampleTitles: []string{"broken", "slow"}, Lifecycle: LifecycleNew},
				{Title: "Login", Count: 8, ExampleIDs: []int64{200}, ExampleTitles: []string{"timeout"}},
			},
		},
		Deltas: Deltas{
			Feedback: DeltaValue{Current: 50, Prior: 30, Change: 20, Direction: "up"},
			Enriched: DeltaValue{Current: 45, Prior: 40, Change: 5, Direction: "up"},
			Urgent:   DeltaValue{Current: 3, Prior: 1, Change: 2, Direction: "up"},
		},
		Sparkline:    []int{5, 8, 3, 12, 7, 9, 6},
		DeepLinkBase: "https://app.attune.dev",
	}

	b, err := RenderPayload(view)
	require.NoError(t, err)

	var p map[string]any
	require.NoError(t, json.Unmarshal(b, &p))

	require.Equal(t, "1", p["version"])
	require.Equal(t, "feedback.digest", p["event_type"])
	require.Equal(t, "acme", p["tenant_id"])
	require.Equal(t, "2026-06-20", p["run_date"])
	require.Equal(t, "digest:acme:2026-06-20", p["idempotency_key"])
	require.Equal(t, "https://app.attune.dev", p["deep_link_base"])

	// Verify themes array structure.
	themes, ok := p["themes"].([]any)
	require.True(t, ok)
	require.Len(t, themes, 2)

	th0, ok := themes[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "Checkout", th0["title"])
	require.Equal(t, float64(15), th0["count"])

	// Verify markdown is present and non-empty.
	md, ok := p["markdown"].(string)
	require.True(t, ok)
	require.NotEmpty(t, md)
	require.Contains(t, md, "Checkout")

	// Verify totals.
	totals, ok := p["totals"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(50), totals["feedback"])
	require.Equal(t, float64(45), totals["enriched"])
	require.Equal(t, float64(3), totals["urgent"])
	require.Equal(t, float64(5), totals["unclustered"])
}

func TestRenderPayload_Themeless(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	view := DigestView{
		TenantID: "beta",
		RunDate:  "2026-06-20",
		From:     from,
		To:       to,
		Result: Result{
			Tier:  TierThemeless,
			Stats: feedback.DigestWindowStats{Total: 3, Enriched: 3},
			Items: []feedback.DigestFeedbackRow{
				{ID: 1, Title: "slow page"},
				{ID: 2, Title: "crash on submit"},
			},
		},
	}

	b, err := RenderPayload(view)
	require.NoError(t, err)

	var p map[string]any
	require.NoError(t, json.Unmarshal(b, &p))

	require.Equal(t, "digest:beta:2026-06-20", p["idempotency_key"])

	items, ok := p["items"].([]any)
	require.True(t, ok)
	require.Len(t, items, 2)

	it0, ok := items[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(1), it0["id"])
	require.Equal(t, "slow page", it0["title"])

	// themes must still be present (possibly empty list, not null) per the
	// struct encoding; RenderPayload always writes the field.
	md, ok := p["markdown"].(string)
	require.True(t, ok)
	require.Contains(t, md, "Recent Feedback")
}

// ---------------------------------------------------------------------------
// buildClusterTheme (additional edge cases)
// ---------------------------------------------------------------------------

func TestBuildClusterTheme_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("empty examples with label", func(t *testing.T) {
		t.Parallel()
		c := embedding.DigestCluster{Count: 3, Label: "Known Label"}
		th := buildClusterTheme(c, nil)
		require.Equal(t, "Known Label", th.Title)
		require.Equal(t, 3, th.Count)
		require.Empty(t, th.ExampleIDs)
		require.Empty(t, th.ExampleTitles)
	})

	t.Run("empty label falls back to first example title", func(t *testing.T) {
		t.Parallel()
		c := embedding.DigestCluster{Count: 5, Label: ""}
		examples := []embedding.DigestExample{
			{ID: 10, Title: "Sample Title"},
			{ID: 11, Title: "Another"},
		}
		th := buildClusterTheme(c, examples)
		require.Equal(t, "Sample Title", th.Title)
		require.Equal(t, 5, th.Count)
		require.Len(t, th.ExampleIDs, 2)
	})

	t.Run("empty label and empty example titles yields untitled", func(t *testing.T) {
		t.Parallel()
		c := embedding.DigestCluster{Count: 2, Label: ""}
		examples := []embedding.DigestExample{
			{ID: 10, Title: ""},
			{ID: 11, Title: ""},
		}
		th := buildClusterTheme(c, examples)
		require.Equal(t, "Untitled theme", th.Title)
	})

	t.Run("whitespace-only label and title yields untitled", func(t *testing.T) {
		t.Parallel()
		c := embedding.DigestCluster{Count: 1, Label: "   "}
		examples := []embedding.DigestExample{
			{ID: 10, Title: "   "},
		}
		th := buildClusterTheme(c, examples)
		require.Equal(t, "Untitled theme", th.Title)
	})

	t.Run("all empty no examples", func(t *testing.T) {
		t.Parallel()
		c := embedding.DigestCluster{Count: 7, Label: ""}
		th := buildClusterTheme(c, nil)
		require.Equal(t, "Untitled theme", th.Title)
		require.Equal(t, 7, th.Count)
	})

	t.Run("example IDs and titles accumulated", func(t *testing.T) {
		t.Parallel()
		c := embedding.DigestCluster{Count: 10, Label: "Theme"}
		examples := []embedding.DigestExample{
			{ID: 1, Title: "A"},
			{ID: 2, Title: "B"},
			{ID: 3, Title: "C"},
		}
		th := buildClusterTheme(c, examples)
		require.Equal(t, []int64{1, 2, 3}, th.ExampleIDs)
		require.Equal(t, []string{"A", "B", "C"}, th.ExampleTitles)
	})
}

// ---------------------------------------------------------------------------
// NewAggregator
// ---------------------------------------------------------------------------

func TestNewAggregator_Coverage(t *testing.T) {
	t.Parallel()

	t.Run("not nil with all dependencies", func(t *testing.T) {
		t.Parallel()
		agg := NewAggregator(fakeClusters{}, fakeFeedback{}, fakeNamer{})
		require.NotNil(t, agg)
	})

	t.Run("fields are wired", func(t *testing.T) {
		t.Parallel()
		fc := fakeClusters{}
		fb := fakeFeedback{}
		fn := fakeNamer{}
		agg := NewAggregator(fc, fb, fn)
		require.NotNil(t, agg.clusters)
		require.NotNil(t, agg.feedback)
		require.NotNil(t, agg.namer)
	})

	t.Run("nil dependencies are accepted", func(t *testing.T) {
		t.Parallel()
		agg := NewAggregator(nil, nil, nil)
		require.NotNil(t, agg, "NewAggregator must return non-nil even with nil deps")
	})
}

// ---------------------------------------------------------------------------
// renderMarkdown: deep link propagation through themed and themeless paths
// ---------------------------------------------------------------------------

func TestRenderMarkdown_DeepLinks(t *testing.T) {
	t.Parallel()

	t.Run("themed view propagates deep link", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate:      "2026-06-20",
			DeepLinkBase: "https://app.test",
			Result: Result{
				Stats:  feedback.DigestWindowStats{Total: 10, Enriched: 10},
				Themes: []Theme{{Title: "Bug", Count: 5, ExampleIDs: []int64{1}}},
			},
		}
		got := renderMarkdown(view)
		require.Contains(t, got, "https://app.test/feedback?theme=Bug")
	})

	t.Run("themeless view propagates deep link", func(t *testing.T) {
		t.Parallel()
		view := DigestView{
			RunDate:      "2026-06-20",
			DeepLinkBase: "https://app.test",
			Result: Result{
				Stats: feedback.DigestWindowStats{Total: 2, Enriched: 2},
				Items: []feedback.DigestFeedbackRow{{ID: 7, Title: "oops"}},
			},
		}
		got := renderMarkdown(view)
		require.Contains(t, got, "https://app.test/feedback/7")
	})
}

// ---------------------------------------------------------------------------
// renderMarkdownThemes: long example title gets truncated
// ---------------------------------------------------------------------------

func TestRenderMarkdownThemes_LongTitle(t *testing.T) {
	t.Parallel()

	longTitle := strings.Repeat("x", 200)
	themes := []Theme{
		{Title: "Theme", Count: 3, ExampleIDs: []int64{1}, ExampleTitles: []string{longTitle}},
	}
	got := renderMarkdownThemes(themes, "")
	// truncate(s, 80) should cap at 80 chars + ellipsis
	require.NotContains(t, got, longTitle, "full 200-char title should not appear")
	require.Contains(t, got, strings.Repeat("x", 80))
}
