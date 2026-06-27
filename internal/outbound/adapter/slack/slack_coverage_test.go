// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test-fixtures

package slack

import (
	"context"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ---------------------------------------------------------------------------
// toDigestView edge cases
// ---------------------------------------------------------------------------

func TestToDigestView_Nil(t *testing.T) {
	t.Parallel()
	_, ok := toDigestView(nil)
	if ok {
		t.Fatal("nil input should return false")
	}
}

func TestToDigestView_EmptyMap(t *testing.T) {
	t.Parallel()
	_, ok := toDigestView(map[string]any{})
	if ok {
		t.Fatal("empty map (no tenant_id/run_date) should return false")
	}
}

func TestToDigestView_Unmarshalable(t *testing.T) {
	t.Parallel()
	_, ok := toDigestView(make(chan int))
	if ok {
		t.Fatal("channel value should fail json.Marshal")
	}
}

func TestToDigestView_DirectType(t *testing.T) {
	t.Parallel()
	dv := digestView{TenantID: "t1", RunDate: "2026-06-15"}
	got, ok := toDigestView(dv)
	if !ok {
		t.Fatal("direct type assertion should succeed")
	}
	if got.TenantID != "t1" {
		t.Errorf("TenantID = %q, want t1", got.TenantID)
	}
}

func TestToDigestView_MapWithRunDateOnly(t *testing.T) {
	t.Parallel()
	m := map[string]any{"run_date": "2026-06-20"}
	got, ok := toDigestView(m)
	if !ok {
		t.Fatal("map with run_date should succeed")
	}
	if got.RunDate != "2026-06-20" {
		t.Errorf("RunDate = %q, want 2026-06-20", got.RunDate)
	}
}

// ---------------------------------------------------------------------------
// buildDigestBlocks edge cases
// ---------------------------------------------------------------------------

func TestBuildDigestBlocks_FallbackForBadView(t *testing.T) {
	t.Parallel()
	// Non-digestView and non-map triggers fallback path.
	blocks := buildDigestBlocks("not a real view")
	if len(blocks) != 2 {
		t.Fatalf("expected 2 fallback blocks, got %d", len(blocks))
	}
	if blocks[0].Type != "header" {
		t.Errorf("fallback block[0] = %s, want header", blocks[0].Type)
	}
	if blocks[1].Type != "section" {
		t.Errorf("fallback block[1] = %s, want section", blocks[1].Type)
	}
}

func TestBuildDigestBlocks_ItemsNoThemes(t *testing.T) {
	t.Parallel()
	view := digestView{
		TenantID: "t1",
		RunDate:  "2026-06-20",
		Result: digestResult{
			Stats: digestStats{Total: 3, Enriched: 3},
			Items: []digestItem{
				{ID: 42, Title: "First item"},
				{ID: 43, Title: "Second item"},
			},
		},
		Deltas: digestDeltas{Feedback: deltaValue{Direction: "flat"}},
	}
	blocks := buildDigestBlocks(view)
	found := false
	for _, b := range blocks {
		if b.Type == "section" && b.Text != nil && strings.Contains(b.Text.Text, "#42") {
			found = true
		}
	}
	if !found {
		t.Error("items path should render #42")
	}
}

func TestBuildDigestBlocks_DownDeltaShown(t *testing.T) {
	t.Parallel()
	view := digestView{
		TenantID: "t1",
		RunDate:  "2026-06-20",
		Result:   digestResult{Stats: digestStats{Total: 5, Enriched: 5}},
		Deltas: digestDeltas{
			Feedback: deltaValue{Current: 5, Prior: 10, Change: -5, Direction: "down"},
		},
	}
	blocks := buildDigestBlocks(view)
	found := false
	for _, b := range blocks {
		if b.Type == "section" && b.Text != nil && strings.Contains(b.Text.Text, "↓") {
			found = true
		}
	}
	if !found {
		t.Error("down delta should render a down arrow")
	}
}

func TestBuildDigestBlocks_SingleCountNoPlural(t *testing.T) {
	t.Parallel()
	view := digestView{
		TenantID: "t1",
		RunDate:  "2026-06-20",
		Result: digestResult{
			Stats:  digestStats{Total: 1, Enriched: 1},
			Themes: []digestTheme{{Title: "Theme", Count: 1}},
		},
		Deltas: digestDeltas{Feedback: deltaValue{Direction: "flat"}},
	}
	blocks := buildDigestBlocks(view)
	for _, b := range blocks {
		if b.Type == "section" && b.Text != nil && strings.Contains(b.Text.Text, "1 reports") {
			t.Error("count=1 should not be pluralized")
		}
	}
}

// ---------------------------------------------------------------------------
// render closure edge cases
// ---------------------------------------------------------------------------

func TestRender_BuildSetsUserAgent(t *testing.T) {
	t.Parallel()
	ch := ptrext.Of(channel{})
	env := ptrext.Of(outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback:  map[string]any{"title": "T", "content": "C", "source": "s"},
	})
	dst := outbound.Target{ID: "t1", TenantID: "tenant-1", URL: "https://hooks.slack.com/services/x"}

	rendered, err := ch.RenderEvent(env, dst)
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := req.Header.Get("User-Agent"); got != "attune/1.0" {
		t.Errorf("User-Agent = %q, want attune/1.0", got)
	}
}

func TestRenderDigest_BuildSetsContentType(t *testing.T) {
	t.Parallel()
	ch := ptrext.Of(channel{})
	view := digestView{
		TenantID: "t1",
		RunDate:  "2026-06-20",
		Result:   digestResult{Stats: digestStats{Total: 1}},
		Deltas:   digestDeltas{Feedback: deltaValue{Direction: "flat"}},
	}
	dst := outbound.Target{ID: "t1", TenantID: "t1", URL: "https://hooks.slack.com/services/x"}

	rendered, err := ch.RenderDigest(view, dst)
	if err != nil {
		t.Fatalf("RenderDigest: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
}

// ---------------------------------------------------------------------------
// renderSparkline edge cases
// ---------------------------------------------------------------------------

func TestRenderSparkline_Empty(t *testing.T) {
	t.Parallel()
	if got := renderSparkline(nil); got != "" {
		t.Errorf("nil counts should return empty, got %q", got)
	}
}

func TestRenderSparkline_SingleMax(t *testing.T) {
	t.Parallel()
	result := renderSparkline([]int{100})
	if len([]rune(result)) != 1 {
		t.Errorf("expected 1 bar, got %d", len([]rune(result)))
	}
	if result != "█" {
		t.Errorf("single max value should be full bar, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// buildEventBlocks: enriched nested path (outbox shape)
// ---------------------------------------------------------------------------

func TestBuildEventBlocks_EnrichedNestedTitle(t *testing.T) {
	t.Parallel()
	env := ptrext.Of(outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"content": "Body text",
			"source":  "api",
			"enriched": map[string]any{
				"title":     "Enriched Title From Nested",
				"is_urgent": true,
			},
		},
	})
	blocks := buildEventBlocks(env)
	if !strings.Contains(blocks[0].Text.Text, "[Urgent]") {
		t.Error("should pick up is_urgent from enriched map")
	}
	if !strings.Contains(blocks[0].Text.Text, "Enriched Title From Nested") {
		t.Error("should pick up title from enriched map when top-level title is empty")
	}
}

// ---------------------------------------------------------------------------
// buildEventBlocks: severity only / category only
// ---------------------------------------------------------------------------

func TestBuildEventBlocks_SeverityOnly(t *testing.T) {
	t.Parallel()
	env := ptrext.Of(outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title": "Test", "content": "Body", "source": "web",
			"enriched": map[string]any{"severity": "critical"},
		},
	})
	blocks := buildEventBlocks(env)
	found := false
	for _, b := range blocks {
		if b.Type == "section" && b.Text != nil && strings.Contains(b.Text.Text, "Severity:") {
			found = true
			if strings.Contains(b.Text.Text, "Category:") {
				t.Error("should not contain Category when only severity is set")
			}
		}
	}
	if !found {
		t.Error("severity-only should still produce a severity section")
	}
}

func TestBuildEventBlocks_CategoryOnly(t *testing.T) {
	t.Parallel()
	env := ptrext.Of(outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title": "Test", "content": "Body", "source": "web",
			"enriched": map[string]any{"category": "ux"},
		},
	})
	blocks := buildEventBlocks(env)
	found := false
	for _, b := range blocks {
		if b.Type == "section" && b.Text != nil && strings.Contains(b.Text.Text, "Category:") {
			found = true
			if strings.Contains(b.Text.Text, "Severity:") {
				t.Error("should not contain Severity when only category is set")
			}
		}
	}
	if !found {
		t.Error("category-only should still produce a category section")
	}
}

// ---------------------------------------------------------------------------
// Channel ID
// ---------------------------------------------------------------------------

func TestSlackChannelID(t *testing.T) {
	t.Parallel()
	ch := ptrext.Of(channel{})
	if ch.ID() != "slack" {
		t.Errorf("ID() = %q, want slack", ch.ID())
	}
}
