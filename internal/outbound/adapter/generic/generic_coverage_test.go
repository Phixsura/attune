// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ---------------------------------------------------------------------------
// RenderDigest edge cases
// ---------------------------------------------------------------------------

func TestRenderDigest_EmptyResult(t *testing.T) {
	t.Parallel()
	c := channel{}
	view := map[string]any{
		"tenant_id": "t1",
		"run_date":  "2026-06-20",
		"result": map[string]any{
			"Stats": map[string]any{
				"Total":    float64(0),
				"Enriched": float64(0),
				"Urgent":   float64(0),
			},
		},
	}
	rendered, err := c.RenderDigest(view, outbound.Target{
		URL:      "https://example.com/digest",
		Secret:   "secret",
		TenantID: "t1",
	})
	if err != nil {
		t.Fatalf("RenderDigest: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if req.Header.Get("X-Attune-Signature") == "" {
		t.Error("expected X-Attune-Signature")
	}
}

func TestRenderDigest_WithUrgentMarkdown(t *testing.T) {
	t.Parallel()
	c := channel{}
	view := map[string]any{
		"tenant_id": "t1",
		"run_date":  "2026-06-20",
		"result": map[string]any{
			"Stats": map[string]any{
				"Total":    float64(10),
				"Enriched": float64(8),
				"Urgent":   float64(3),
			},
			"Themes": []any{
				map[string]any{"Title": "Theme A", "Count": float64(5)},
				map[string]any{"Title": "Theme B", "Count": float64(1)},
			},
		},
		"deep_link_base": "https://app.example.com",
	}
	rendered, err := c.RenderDigest(view, outbound.Target{
		URL:      "https://example.com/digest",
		Secret:   "secret",
		TenantID: "t1",
	})
	if err != nil {
		t.Fatalf("RenderDigest: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if req.Header.Get("User-Agent") != "attune/1.0" {
		t.Errorf("User-Agent = %q", req.Header.Get("User-Agent"))
	}
}

// ---------------------------------------------------------------------------
// renderDigestPayload
// ---------------------------------------------------------------------------

func TestRenderDigestPayload_Structure(t *testing.T) {
	t.Parallel()
	view := map[string]any{
		"tenant_id":      "t1",
		"run_date":       "2026-06-20",
		"from":           "2026-06-19T00:00:00Z",
		"to":             "2026-06-20T00:00:00Z",
		"deep_link_base": "https://app.example.com",
		"result": map[string]any{
			"Stats": map[string]any{
				"Total":       float64(10),
				"Enriched":    float64(8),
				"Urgent":      float64(3),
				"Unclustered": float64(2),
			},
			"Themes": []any{
				map[string]any{"Title": "Theme A", "Count": float64(5)},
			},
		},
		"deltas": map[string]any{
			"feedback": map[string]any{"current": float64(10), "prior": float64(5)},
		},
		"sparkline": []any{float64(1), float64(2), float64(3)},
	}
	body, err := renderDigestPayload(view)
	if err != nil {
		t.Fatalf("renderDigestPayload: %v", err)
	}
	var payload digestPayload
	if err := json.Unmarshal(body, &payload); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Version != "1" {
		t.Errorf("version = %q, want 1", payload.Version)
	}
	if payload.EventType != "feedback.digest" {
		t.Errorf("event_type = %q", payload.EventType)
	}
	if payload.TenantID != "t1" {
		t.Errorf("tenant_id = %q", payload.TenantID)
	}
	if payload.IdempotencyKey != "digest:t1:2026-06-20" {
		t.Errorf("idempotency_key = %q", payload.IdempotencyKey)
	}
	if payload.DeepLinkBase != "https://app.example.com" {
		t.Errorf("deep_link_base = %q", payload.DeepLinkBase)
	}
	if payload.Markdown == "" {
		t.Error("markdown should be non-empty")
	}
}

// ---------------------------------------------------------------------------
// renderDigestMarkdown
// ---------------------------------------------------------------------------

func TestRenderDigestMarkdown_ThemesPath(t *testing.T) {
	t.Parallel()
	dv := map[string]any{
		"run_date": "2026-06-20",
		"result": map[string]any{
			"Stats": map[string]any{
				"Total":    float64(10),
				"Enriched": float64(8),
				"Urgent":   float64(2),
			},
			"Themes": []any{
				map[string]any{"Title": "Theme A", "Count": float64(5)},
				map[string]any{"Title": "Theme B", "Count": float64(1)},
			},
		},
	}
	md := renderDigestMarkdown(dv)
	if !strings.Contains(md, "**Daily Digest") {
		t.Errorf("missing header, got %q", md)
	}
	if !strings.Contains(md, "urgent") {
		t.Errorf("urgent>0 should mention urgent, got %q", md)
	}
	if !strings.Contains(md, "Theme A") {
		t.Errorf("should mention themes, got %q", md)
	}
	// Count=1 should not have trailing 's'.
	if strings.Contains(md, "1 reports") {
		t.Error("count=1 should say 'report' not 'reports'")
	}
}

func TestRenderDigestMarkdown_ItemsPath(t *testing.T) {
	t.Parallel()
	dv := map[string]any{
		"run_date": "2026-06-20",
		"result": map[string]any{
			"Stats": map[string]any{
				"Total":    float64(3),
				"Enriched": float64(3),
				"Urgent":   float64(0),
			},
			"Items": []any{
				map[string]any{"ID": float64(42), "Title": "Item A"},
			},
		},
	}
	md := renderDigestMarkdown(dv)
	if !strings.Contains(md, "#42") {
		t.Errorf("items path should render #42, got %q", md)
	}
	if strings.Contains(md, "urgent") {
		t.Errorf("urgent=0 should not mention urgent, got %q", md)
	}
}

func TestRenderDigestMarkdown_NoThemesNoItems(t *testing.T) {
	t.Parallel()
	dv := map[string]any{
		"run_date": "2026-06-20",
		"result": map[string]any{
			"Stats": map[string]any{
				"Total":    float64(0),
				"Enriched": float64(0),
				"Urgent":   float64(0),
			},
		},
	}
	md := renderDigestMarkdown(dv)
	if !strings.Contains(md, "Daily Digest") {
		t.Errorf("should still have header, got %q", md)
	}
}

// ---------------------------------------------------------------------------
// RenderEvent: default (bytes) signature version
// ---------------------------------------------------------------------------

func TestRenderEvent_DefaultBytesSignature(t *testing.T) {
	t.Parallel()
	c := channel{}
	env := ptrext.Of(outbound.Envelope{
		Version:   "2",
		TenantID:  "t1",
		Feedback:  map[string]any{"id": float64(1)},
		EventType: "feedback.enriched",
	})
	target := outbound.Target{
		URL:              "https://example.com/hook",
		Secret:           "secret",
		SignatureVersion: "", // default => bytes
		TenantID:         "t1",
	}
	rendered, err := c.RenderEvent(env, target)
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sig := req.Header.Get("X-Attune-Signature")
	if sig == "" {
		t.Fatal("expected X-Attune-Signature for bytes mode")
	}
}
