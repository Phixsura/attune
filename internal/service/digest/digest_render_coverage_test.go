// SPDX-License-Identifier: Apache-2.0

package digest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/repo/feedback"
)

// ---------------------------------------------------------------------------
// renderSparkline edge cases
// ---------------------------------------------------------------------------

func TestRenderSparkline_Nil(t *testing.T) {
	t.Parallel()
	if got := renderSparkline(nil); got != "" {
		t.Errorf("nil should return empty, got %q", got)
	}
}

func TestRenderSparkline_SingleValue(t *testing.T) {
	t.Parallel()
	got := renderSparkline([]int{42})
	if len([]rune(got)) != 1 {
		t.Errorf("expected 1 bar, got %d", len([]rune(got)))
	}
	if got != "█" {
		t.Errorf("single value should be full bar, got %q", got)
	}
}

func TestRenderSparkline_AllZeros(t *testing.T) {
	t.Parallel()
	got := renderSparkline([]int{0, 0, 0})
	if len([]rune(got)) != 3 {
		t.Errorf("expected 3 bars, got %d", len([]rune(got)))
	}
	for _, r := range got {
		if r != '▁' {
			t.Errorf("zero sparkline should all be lowest bar, got %c", r)
		}
	}
}

// ---------------------------------------------------------------------------
// deltaArrow edge cases
// ---------------------------------------------------------------------------

func TestDeltaArrow_UpNoChange(t *testing.T) {
	t.Parallel()
	got := deltaArrow(DeltaValue{Direction: "up", Change: 0})
	if got != "↑" {
		t.Errorf("up with 0 change = %q, want bare arrow", got)
	}
}

func TestDeltaArrow_DownNoChange(t *testing.T) {
	t.Parallel()
	got := deltaArrow(DeltaValue{Direction: "down", Change: 0})
	if got != "↓" {
		t.Errorf("down with 0 change = %q, want bare arrow", got)
	}
}

func TestDeltaArrow_Flat(t *testing.T) {
	t.Parallel()
	got := deltaArrow(DeltaValue{Direction: "flat"})
	if got != "" {
		t.Errorf("flat = %q, want empty", got)
	}
}

func TestDeltaArrow_EmptyDirection(t *testing.T) {
	t.Parallel()
	got := deltaArrow(DeltaValue{Direction: ""})
	if got != "" {
		t.Errorf("empty direction = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// lifecycleBadge edge cases
// ---------------------------------------------------------------------------

func TestLifecycleBadge_Ongoing(t *testing.T) {
	t.Parallel()
	got := lifecycleBadge(LifecycleOngoing)
	if got != "" {
		t.Errorf("ongoing = %q, want empty", got)
	}
}

func TestLifecycleBadge_Empty(t *testing.T) {
	t.Parallel()
	got := lifecycleBadge("")
	if got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
}

func TestLifecycleBadge_New(t *testing.T) {
	t.Parallel()
	got := lifecycleBadge(LifecycleNew)
	if got != "[NEW] " {
		t.Errorf("new = %q, want [NEW] ", got)
	}
}

func TestLifecycleBadge_Regressed(t *testing.T) {
	t.Parallel()
	got := lifecycleBadge(LifecycleRegressed)
	if got != "[BACK] " {
		t.Errorf("regressed = %q, want [BACK] ", got)
	}
}

// ---------------------------------------------------------------------------
// truncate edge cases
// ---------------------------------------------------------------------------

func TestTruncate_Short(t *testing.T) {
	t.Parallel()
	got := truncate("hi", 10)
	if got != "hi" {
		t.Errorf("short string = %q", got)
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	t.Parallel()
	got := truncate("hello", 5)
	if got != "hello" {
		t.Errorf("exact length = %q", got)
	}
}

func TestTruncate_Over(t *testing.T) {
	t.Parallel()
	got := truncate("hello world", 5)
	if got != "hello…" {
		t.Errorf("over length = %q", got)
	}
}

// ---------------------------------------------------------------------------
// computeDeltas / computeDelta edge cases
// ---------------------------------------------------------------------------

func TestComputeDeltas_AllZero(t *testing.T) {
	t.Parallel()
	d := computeDeltas(feedback.DigestWindowStats{}, feedback.DigestWindowStats{})
	if d.Feedback.Direction != "flat" {
		t.Errorf("both zero = %q, want flat", d.Feedback.Direction)
	}
	if d.Enriched.Direction != "flat" {
		t.Errorf("enriched = %q, want flat", d.Enriched.Direction)
	}
	if d.Urgent.Direction != "flat" {
		t.Errorf("urgent = %q, want flat", d.Urgent.Direction)
	}
}

func TestComputeDeltas_AllUp(t *testing.T) {
	t.Parallel()
	curr := feedback.DigestWindowStats{Total: 10, Enriched: 8, Urgent: 2}
	prior := feedback.DigestWindowStats{Total: 5, Enriched: 4, Urgent: 1}
	d := computeDeltas(curr, prior)
	if d.Feedback.Direction != "up" || d.Feedback.Change != 5 {
		t.Errorf("feedback = %+v", d.Feedback)
	}
	if d.Enriched.Direction != "up" || d.Enriched.Change != 4 {
		t.Errorf("enriched = %+v", d.Enriched)
	}
	if d.Urgent.Direction != "up" || d.Urgent.Change != 1 {
		t.Errorf("urgent = %+v", d.Urgent)
	}
}

func TestComputeDeltas_AllDown(t *testing.T) {
	t.Parallel()
	curr := feedback.DigestWindowStats{Total: 5, Enriched: 4, Urgent: 1}
	prior := feedback.DigestWindowStats{Total: 10, Enriched: 8, Urgent: 3}
	d := computeDeltas(curr, prior)
	if d.Feedback.Direction != "down" || d.Feedback.Change != -5 {
		t.Errorf("feedback = %+v", d.Feedback)
	}
}

// ---------------------------------------------------------------------------
// buildSparkline
// ---------------------------------------------------------------------------

func TestBuildSparkline_EmptyCounts(t *testing.T) {
	t.Parallel()
	to := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	result := buildSparkline(nil, to, 7)
	if len(result) != 7 {
		t.Errorf("expected 7 entries, got %d", len(result))
	}
	for _, v := range result {
		if v != 0 {
			t.Errorf("no counts should produce all zeros, got %d", v)
		}
	}
}

func TestBuildSparkline_WithCounts(t *testing.T) {
	t.Parallel()
	to := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	counts := []feedback.DailyCount{
		{Date: time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC), Count: 5},
		{Date: time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC), Count: 3},
	}
	result := buildSparkline(counts, to, 7)
	if len(result) != 7 {
		t.Errorf("expected 7 entries, got %d", len(result))
	}
	// Index 5 = June 19 (to - 1), index 4 = June 18 (to - 2).
	if result[5] != 5 {
		t.Errorf("June 19 should be 5, got %d", result[5])
	}
	if result[4] != 3 {
		t.Errorf("June 18 should be 3, got %d", result[4])
	}
}

// ---------------------------------------------------------------------------
// renderMarkdownHeader edge cases
// ---------------------------------------------------------------------------

func TestRenderMarkdownHeader_WithSparkline(t *testing.T) {
	t.Parallel()
	view := DigestView{
		RunDate:   "2026-06-20",
		Sparkline: []int{1, 2, 3},
		Result:    Result{Stats: feedback.DigestWindowStats{Total: 10, Enriched: 8}},
		Deltas:    Deltas{Feedback: DeltaValue{Direction: "up", Change: 2}},
	}
	md := renderMarkdownHeader(view)
	if !strings.Contains(md, "7-day trend") {
		t.Errorf("should mention 7-day trend, got %q", md)
	}
	if !strings.Contains(md, "↑2") {
		t.Errorf("should contain up arrow with count, got %q", md)
	}
}

func TestRenderMarkdownHeader_WithUrgentAndDelta(t *testing.T) {
	t.Parallel()
	view := DigestView{
		RunDate: "2026-06-20",
		Result:  Result{Stats: feedback.DigestWindowStats{Total: 10, Enriched: 8, Urgent: 3}},
		Deltas: Deltas{
			Feedback: DeltaValue{Direction: "up", Change: 2},
			Enriched: DeltaValue{Direction: "up", Change: 1},
			Urgent:   DeltaValue{Direction: "down", Change: -1},
		},
	}
	md := renderMarkdownHeader(view)
	if !strings.Contains(md, "urgent") {
		t.Errorf("urgent>0 should mention urgent, got %q", md)
	}
	if !strings.Contains(md, "↓1") {
		t.Errorf("urgent delta should show down arrow, got %q", md)
	}
}

// ---------------------------------------------------------------------------
// renderMarkdownItems edge cases
// ---------------------------------------------------------------------------

func TestRenderMarkdownItems_WithDeepLink(t *testing.T) {
	t.Parallel()
	items := []feedback.DigestFeedbackRow{
		{ID: 42, Title: "First item"},
	}
	md := renderMarkdownItems(items, "https://app.example.com")
	if !strings.Contains(md, "[→](https://app.example.com/feedback/42)") {
		t.Errorf("should contain deep link, got %q", md)
	}
}

func TestRenderMarkdownItems_NoDeepLink(t *testing.T) {
	t.Parallel()
	items := []feedback.DigestFeedbackRow{
		{ID: 42, Title: "First item"},
	}
	md := renderMarkdownItems(items, "")
	if strings.Contains(md, "[→]") {
		t.Errorf("no deep link base should omit link, got %q", md)
	}
}

// ---------------------------------------------------------------------------
// renderMarkdown integration
// ---------------------------------------------------------------------------

func TestRenderMarkdown_WithThemesAndUnclustered(t *testing.T) {
	t.Parallel()
	view := DigestView{
		RunDate: "2026-06-20",
		Result: Result{
			Stats: feedback.DigestWindowStats{Total: 20, Enriched: 15, Unclustered: 3},
			Themes: []Theme{
				{Title: "Theme A", Count: 10, Lifecycle: LifecycleNew},
			},
		},
		Deltas: Deltas{Feedback: DeltaValue{Direction: "flat"}},
	}
	md := renderMarkdown(view)
	if !strings.Contains(md, "+3 unclustered") {
		t.Errorf("should mention unclustered, got %q", md)
	}
}

func TestRenderMarkdown_NoThemesNoItems(t *testing.T) {
	t.Parallel()
	view := DigestView{
		RunDate: "2026-06-20",
		Result:  Result{Stats: feedback.DigestWindowStats{Total: 0, Enriched: 0}},
		Deltas:  Deltas{Feedback: DeltaValue{Direction: "flat"}},
	}
	md := renderMarkdown(view)
	if !strings.Contains(md, "Daily Digest") {
		t.Errorf("should have header, got %q", md)
	}
	if strings.Contains(md, "Top Themes") || strings.Contains(md, "Recent Feedback") {
		t.Errorf("empty should not have themes or items, got %q", md)
	}
}

// ---------------------------------------------------------------------------
// RenderPayload integration
// ---------------------------------------------------------------------------

func TestRenderPayload_EmptyView(t *testing.T) {
	t.Parallel()
	view := DigestView{
		TenantID: "t1",
		RunDate:  "2026-06-20",
		From:     time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
		Result:   Result{Stats: feedback.DigestWindowStats{}},
	}
	body, err := RenderPayload(view)
	if err != nil {
		t.Fatalf("RenderPayload: %v", err)
	}
	var payload payloadOut
	if err := json.Unmarshal(body, &payload); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Version != payloadVersion {
		t.Errorf("version = %q", payload.Version)
	}
	if payload.EventType != payloadEventType {
		t.Errorf("event_type = %q", payload.EventType)
	}
	if payload.IdempotencyKey != "digest:t1:2026-06-20" {
		t.Errorf("idempotency_key = %q", payload.IdempotencyKey)
	}
}

func TestRenderPayload_WithDeepLinkBase(t *testing.T) {
	t.Parallel()
	view := DigestView{
		TenantID:     "t1",
		RunDate:      "2026-06-20",
		From:         time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC),
		To:           time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
		DeepLinkBase: "https://app.example.com",
		Result: Result{
			Stats:  feedback.DigestWindowStats{Total: 5, Enriched: 5},
			Themes: []Theme{{Title: "T1", Count: 3, ExampleIDs: []int64{1}, ExampleTitles: []string{"E1"}}},
			Items:  []feedback.DigestFeedbackRow{{ID: 42, Title: "Item"}},
		},
	}
	body, err := RenderPayload(view)
	if err != nil {
		t.Fatalf("RenderPayload: %v", err)
	}
	var payload payloadOut
	if err := json.Unmarshal(body, &payload); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.DeepLinkBase != "https://app.example.com" {
		t.Errorf("deep_link_base = %q", payload.DeepLinkBase)
	}
	if len(payload.Themes) != 1 {
		t.Errorf("themes len = %d", len(payload.Themes))
	}
	if len(payload.Items) != 1 {
		t.Errorf("items len = %d", len(payload.Items))
	}
}

// ---------------------------------------------------------------------------
// applyLifecycle
// ---------------------------------------------------------------------------

func TestApplyLifecycle_NewAndOngoing(t *testing.T) {
	t.Parallel()
	themes := []Theme{
		{Title: "Known Issue"},
		{Title: "Brand New"},
	}
	prior := map[string]bool{"known issue": true}
	applyLifecycle(themes, prior)
	if themes[0].Lifecycle != LifecycleOngoing {
		t.Errorf("known issue should be ongoing, got %q", themes[0].Lifecycle)
	}
	if themes[1].Lifecycle != LifecycleNew {
		t.Errorf("brand new should be new, got %q", themes[1].Lifecycle)
	}
}

func TestApplyLifecycle_EmptyPrior(t *testing.T) {
	t.Parallel()
	themes := []Theme{{Title: "A"}, {Title: "B"}}
	applyLifecycle(themes, nil)
	for _, th := range themes {
		if th.Lifecycle != LifecycleNew {
			t.Errorf("all should be new with nil prior, got %q", th.Lifecycle)
		}
	}
}
