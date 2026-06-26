// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test-fixtures

package lark

import (
	"context"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ---------------------------------------------------------------------------
// buildDigestCard: empty themes + items path
// ---------------------------------------------------------------------------

func TestBuildDigestCard_EmptyThemesAndItems(t *testing.T) {
	t.Parallel()
	view := digestView{
		RunDate: "2026-06-20",
		Result: digestResult{
			Stats: digestStats{Total: 0, Enriched: 0},
		},
		Deltas: digestDeltas{Feedback: deltaValue{Direction: "flat"}},
	}
	card := buildDigestCard(view)
	if card.Header.Template != "purple" {
		t.Errorf("template = %q, want purple", card.Header.Template)
	}
	// summary + hr + hr + note = at least 4 elements.
	if len(card.Elements) < 3 {
		t.Errorf("expected at least 3 elements, got %d", len(card.Elements))
	}
}

func TestBuildDigestCard_DownDelta(t *testing.T) {
	t.Parallel()
	view := digestView{
		RunDate: "2026-06-20",
		Result: digestResult{
			Stats: digestStats{Total: 5, Enriched: 5},
		},
		Deltas: digestDeltas{
			Feedback: deltaValue{Current: 5, Prior: 10, Change: -5, Direction: "down"},
		},
	}
	card := buildDigestCard(view)
	found := false
	for _, e := range card.Elements {
		if e.Text != nil && strings.Contains(e.Text.Content, "↓") {
			found = true
		}
	}
	if !found {
		t.Error("down delta should render a down arrow")
	}
}

func TestBuildDigestCard_NoSparkline(t *testing.T) {
	t.Parallel()
	view := digestView{
		RunDate: "2026-06-20",
		Result: digestResult{
			Stats: digestStats{Total: 1, Enriched: 1},
		},
		Deltas:    digestDeltas{Feedback: deltaValue{Direction: "flat"}},
		Sparkline: nil,
	}
	card := buildDigestCard(view)
	for _, e := range card.Elements {
		if e.Text != nil && strings.Contains(e.Text.Content, "📈") {
			t.Error("nil sparkline should not render the sparkline emoji")
		}
	}
}

func TestBuildDigestCard_UrgentMentioned(t *testing.T) {
	t.Parallel()
	view := digestView{
		RunDate: "2026-06-20",
		Result: digestResult{
			Stats: digestStats{Total: 10, Enriched: 8, Urgent: 3},
		},
		Deltas: digestDeltas{Feedback: deltaValue{Direction: "flat"}},
	}
	card := buildDigestCard(view)
	found := false
	for _, e := range card.Elements {
		if e.Text != nil && strings.Contains(e.Text.Content, "urgent") {
			found = true
		}
	}
	if !found {
		t.Error("urgent > 0 should mention urgent in summary")
	}
}

func TestBuildDigestCard_SingleThemeNoPlural(t *testing.T) {
	t.Parallel()
	view := digestView{
		RunDate: "2026-06-20",
		Result: digestResult{
			Stats: digestStats{Total: 1, Enriched: 1},
			Themes: []digestTheme{
				{Title: "Single", Count: 1, Lifecycle: "new"},
			},
		},
		Deltas: digestDeltas{Feedback: deltaValue{Direction: "flat"}},
	}
	card := buildDigestCard(view)
	for _, e := range card.Elements {
		if e.Text != nil && strings.Contains(e.Text.Content, "1 reports") {
			t.Error("count=1 should say 'report' not 'reports'")
		}
	}
}

// ---------------------------------------------------------------------------
// buildEventCard edge cases
// ---------------------------------------------------------------------------

func TestBuildEventCard_NilFeedback(t *testing.T) {
	t.Parallel()
	env := ptrext.Of(outbound.Envelope{
		Version:   "v2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback:  nil,
	})
	card := buildEventCard(env)
	if card.Header.Title.Content != "New Feedback" {
		t.Errorf("nil feedback title = %q, want 'New Feedback'", card.Header.Title.Content)
	}
	if card.Header.Template != "blue" {
		t.Errorf("nil feedback template = %q, want blue", card.Header.Template)
	}
}

func TestBuildEventCard_LongContentTruncated(t *testing.T) {
	t.Parallel()
	longContent := strings.Repeat("x", 1000)
	env := ptrext.Of(outbound.Envelope{
		Version:   "v2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback:  map[string]any{"title": "Title", "content": longContent},
	})
	card := buildEventCard(env)
	// Content is truncated to 500 in the div element.
	for _, e := range card.Elements {
		if e.Tag == "div" && e.Text != nil && len(e.Text.Content) > 510 {
			t.Errorf("content should be truncated to ~500, got %d", len(e.Text.Content))
		}
	}
}

// ---------------------------------------------------------------------------
// render: signing
// ---------------------------------------------------------------------------

func TestRender_NoSecretOmitsSignature(t *testing.T) {
	t.Parallel()
	ch := ptrext.Of(channel{})
	dst := outbound.Target{
		ID:       "t1",
		TenantID: "tenant-1",
		URL:      "https://example.com/webhook",
		Secret:   "",
	}
	env := ptrext.Of(outbound.Envelope{
		Version:   "v2",
		Timestamp: "2026-06-20T12:00:00Z",
		Feedback:  map[string]any{"title": "T"},
	})
	rendered, err := ch.RenderEvent(env, dst)
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	_, err = rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
}

// ---------------------------------------------------------------------------
// renderSparkline edge cases
// ---------------------------------------------------------------------------

func TestRenderSparkline_Single(t *testing.T) {
	t.Parallel()
	result := renderSparkline([]int{42})
	if len([]rune(result)) != 1 {
		t.Errorf("expected 1 bar, got %d", len([]rune(result)))
	}
	if result != "█" {
		t.Errorf("single value should be full bar, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// deltaArrow: additional coverage
// ---------------------------------------------------------------------------

func TestDeltaArrow_UpWithZeroChange(t *testing.T) {
	t.Parallel()
	got := deltaArrow(deltaValue{Direction: "up", Change: 0})
	if got != "↑" {
		t.Errorf("up with 0 change should be bare arrow, got %q", got)
	}
}

func TestDeltaArrow_DownWithZeroChange(t *testing.T) {
	t.Parallel()
	got := deltaArrow(deltaValue{Direction: "down", Change: 0})
	if got != "↓" {
		t.Errorf("down with 0 change should be bare arrow, got %q", got)
	}
}
