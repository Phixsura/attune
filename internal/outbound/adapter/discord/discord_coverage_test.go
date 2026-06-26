// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test-fixtures

package discord

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

func TestToDigestView_RunDateOnly(t *testing.T) {
	t.Parallel()
	m := map[string]any{"run_date": "2026-06-20"}
	got, ok := toDigestView(m)
	if !ok {
		t.Fatal("map with run_date should succeed")
	}
	if got.RunDate != "2026-06-20" {
		t.Errorf("RunDate = %q", got.RunDate)
	}
}

// ---------------------------------------------------------------------------
// buildDigestEmbed edge cases
// ---------------------------------------------------------------------------

func TestBuildDigestEmbed_EmptyThemesAndItems(t *testing.T) {
	t.Parallel()
	view := digestView{
		TenantID: "t1",
		RunDate:  "2026-06-20",
		Result:   digestResult{Stats: digestStats{Total: 0, Enriched: 0}},
	}
	embed := buildDigestEmbed(view)
	if !strings.Contains(embed.Title, "Digest") {
		t.Errorf("title should contain Digest, got %q", embed.Title)
	}
	// No themes/items => description still has the totals line.
	if !strings.Contains(embed.Description, "0") {
		t.Errorf("empty digest should mention 0 feedback, got %q", embed.Description)
	}
}

func TestBuildDigestEmbed_NoUrgent(t *testing.T) {
	t.Parallel()
	view := digestView{
		TenantID: "t1",
		RunDate:  "2026-06-20",
		Result:   digestResult{Stats: digestStats{Total: 5, Enriched: 5, Urgent: 0}},
	}
	embed := buildDigestEmbed(view)
	if strings.Contains(embed.Description, "urgent") {
		t.Errorf("0 urgent should not mention urgent, got %q", embed.Description)
	}
}

// ---------------------------------------------------------------------------
// buildEventEmbed edge cases
// ---------------------------------------------------------------------------

func TestBuildEventEmbed_NilFeedbackMap(t *testing.T) {
	t.Parallel()
	env := ptrext.Of(outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback:  nil,
	})
	embed := buildEventEmbed(env)
	if !strings.Contains(embed.Title, "New Feedback") {
		t.Errorf("nil feedback title should default to 'New Feedback', got %q", embed.Title)
	}
}

func TestBuildEventEmbed_EmptyContent(t *testing.T) {
	t.Parallel()
	env := ptrext.Of(outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback:  map[string]any{"title": "Title", "source": "web"},
	})
	embed := buildEventEmbed(env)
	if embed.Description != "" {
		t.Errorf("empty content should produce empty description, got %q", embed.Description)
	}
}

func TestBuildEventEmbed_EnrichedNestedUrgent(t *testing.T) {
	t.Parallel()
	env := ptrext.Of(outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"content": "Body",
			"source":  "api",
			"enriched": map[string]any{
				"title":     "Nested Title",
				"is_urgent": true,
			},
		},
	})
	embed := buildEventEmbed(env)
	if !strings.Contains(embed.Title, "[Urgent]") {
		t.Error("should pick up is_urgent from enriched map")
	}
	if !strings.Contains(embed.Title, "Nested Title") {
		t.Error("should pick up title from enriched map")
	}
	if embed.Color != colorRed {
		t.Errorf("urgent should be red, got %d", embed.Color)
	}
}

func TestBuildEventEmbed_NoSourceNoFields(t *testing.T) {
	t.Parallel()
	env := ptrext.Of(outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback:  map[string]any{"title": "Title", "content": "Content"},
	})
	embed := buildEventEmbed(env)
	if len(embed.Fields) != 0 {
		t.Errorf("no source/severity/category should produce 0 fields, got %d", len(embed.Fields))
	}
}

func TestBuildEventEmbed_CriticalSeverityColor(t *testing.T) {
	t.Parallel()
	env := ptrext.Of(outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title": "X", "content": "Y", "source": "api",
			"enriched": map[string]any{
				"attrs": map[string]any{"severity": "critical"},
			},
		},
	})
	embed := buildEventEmbed(env)
	if embed.Color != colorRed {
		t.Errorf("critical severity should map to red, got %d", embed.Color)
	}
}

// ---------------------------------------------------------------------------
// render closure
// ---------------------------------------------------------------------------

func TestRender_DigestBuildSetsHeaders(t *testing.T) {
	t.Parallel()
	ch := ptrext.Of(channel{})
	view := digestView{
		TenantID: "t1",
		RunDate:  "2026-06-20",
		Result:   digestResult{Stats: digestStats{Total: 1}},
	}
	dst := outbound.Target{ID: "t1", TenantID: "t1", URL: "https://discord.com/api/webhooks/x/y"}

	rendered, err := ch.RenderDigest(view, dst)
	if err != nil {
		t.Fatalf("RenderDigest: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := req.Header.Get("User-Agent"); got != "attune/1.0" {
		t.Errorf("User-Agent = %q", got)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
}

// ---------------------------------------------------------------------------
// descriptionBudget edge cases
// ---------------------------------------------------------------------------

func TestDescriptionBudget_WithFields(t *testing.T) {
	t.Parallel()
	e := discordEmbed{
		Title:  "Title",
		Fields: []discordField{{Name: "F1", Value: strings.Repeat("x", 500)}},
		Footer: ptrext.Of(discordFooter{Text: "Footer"}),
	}
	budget := descriptionBudget(e)
	if budget <= 0 {
		t.Fatalf("should have positive budget, got %d", budget)
	}
	if budget > embedDescriptionMax {
		t.Fatalf("budget should not exceed description max, got %d", budget)
	}
}

// ---------------------------------------------------------------------------
// validTimestamp edge cases
// ---------------------------------------------------------------------------

func TestValidTimestamp_RFC3339WithOffset(t *testing.T) {
	t.Parallel()
	ts := "2026-06-20T12:00:00+08:00"
	if got := validTimestamp(ts); got != ts {
		t.Errorf("valid RFC3339 with offset should be kept, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Channel ID
// ---------------------------------------------------------------------------

func TestDiscordChannelID(t *testing.T) {
	t.Parallel()
	ch := ptrext.Of(channel{})
	if ch.ID() != "discord" {
		t.Errorf("ID() = %q, want discord", ch.ID())
	}
}
