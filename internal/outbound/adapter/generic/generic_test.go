// SPDX-License-Identifier: Apache-2.0

package generic

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestRenderEvent_SetsDeliveryIDHeader(t *testing.T) {
	c := channel{}
	env := ptrext.Of(outbound.Envelope{
		Version:    "2",
		EventType:  "feedback.enriched",
		TenantID:   "t1",
		Feedback:   map[string]any{"id": float64(7)},
		DeliveryID: "42",
	})
	rendered, err := c.RenderEvent(env, outbound.Target{URL: "https://example.com/hook", Secret: "shhh"})
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := req.Header.Get("X-Attune-Delivery-Id"); got != "42" {
		t.Fatalf("X-Attune-Delivery-Id = %q, want %q", got, "42")
	}
	if req.Header.Get("X-Attune-Signature") == "" {
		t.Fatal("expected X-Attune-Signature to still be set")
	}
}

func TestRedactURL_StripsSecrets(t *testing.T) {
	cases := map[string]string{
		"https://u:p@hooks.example.com/services/T0/B0/SECRET?tok=abc#frag": "https://hooks.example.com",
		"https://hooks.example.com/path/with/SECRET":                       "https://hooks.example.com",
		"https://host:8443/x": "https://host:8443",
		"::::not a url":       "<redacted-url>",
	}
	for in, want := range cases {
		if got := redactURL(in); got != want {
			t.Errorf("redactURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderEvent_OmitsDeliveryIDHeaderWhenUnset(t *testing.T) {
	c := channel{}
	env := ptrext.Of(outbound.Envelope{Version: "2", TenantID: "t1", Feedback: map[string]any{}})
	rendered, err := c.RenderEvent(env, outbound.Target{URL: "https://example.com/hook", Secret: "shhh"})
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, ok := req.Header["X-Attune-Delivery-Id"]; ok {
		t.Fatal("X-Attune-Delivery-Id should be absent when DeliveryID is empty")
	}
}

func TestChannel_ID(t *testing.T) {
	c := channel{}
	if got := c.ID(); got != "raw-webhook" {
		t.Fatalf("ID() = %q, want %q", got, "raw-webhook")
	}
}

func TestRenderEvent_ContentHashSignature(t *testing.T) {
	c := channel{}
	env := ptrext.Of(outbound.Envelope{
		Version:    "2",
		EventType:  "feedback.enriched",
		TenantID:   "t1",
		Feedback:   map[string]any{"id": float64(42)},
		DeliveryID: "abc",
	})
	target := outbound.Target{
		URL:              "https://example.com/hook",
		Secret:           "shhh",
		SignatureVersion: outbound.SignatureVersionContentHash,
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
		t.Fatal("expected X-Attune-Signature")
	}
}

func TestRenderEvent_SetsContentType(t *testing.T) {
	c := channel{}
	env := ptrext.Of(outbound.Envelope{Version: "2", TenantID: "t1", Feedback: map[string]any{}})
	rendered, err := c.RenderEvent(env, outbound.Target{URL: "https://example.com/hook", Secret: "shhh"})
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want application/json; charset=utf-8", got)
	}
}

func TestRenderEvent_SetsUserAgent(t *testing.T) {
	c := channel{}
	env := ptrext.Of(outbound.Envelope{Version: "2", TenantID: "t1", Feedback: map[string]any{}})
	rendered, err := c.RenderEvent(env, outbound.Target{URL: "https://example.com/hook", Secret: "shhh"})
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := req.Header.Get("User-Agent"); got != "attune/1.0" {
		t.Fatalf("User-Agent = %q, want attune/1.0", got)
	}
}

func TestRenderDigest(t *testing.T) {
	c := channel{}
	view := map[string]any{
		"tenant_id": "t1",
		"run_date":  "2026-06-15",
		"from":      "2026-06-14T00:00:00Z",
		"to":        "2026-06-15T00:00:00Z",
		"result": map[string]any{
			"Stats": map[string]any{
				"Total":       float64(10),
				"Enriched":    float64(8),
				"Urgent":      float64(2),
				"Unclustered": float64(1),
			},
			"Themes": []any{
				map[string]any{"Title": "Theme 1", "Count": float64(5)},
			},
		},
	}
	rendered, err := c.RenderDigest(view, outbound.Target{
		URL:      "https://example.com/digest",
		Secret:   "digest-secret",
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
		t.Fatal("expected X-Attune-Signature")
	}
	if req.Header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatal("expected Content-Type")
	}
}

func TestRenderDigest_NoThemes_ShowsItems(t *testing.T) {
	c := channel{}
	view := map[string]any{
		"tenant_id": "t1",
		"run_date":  "2026-06-15",
		"from":      "2026-06-14T00:00:00Z",
		"to":        "2026-06-15T00:00:00Z",
		"result": map[string]any{
			"Stats": map[string]any{
				"Total":    float64(3),
				"Enriched": float64(3),
				"Urgent":   float64(0),
			},
			"Themes": []any{},
			"Items": []any{
				map[string]any{"ID": float64(1), "Title": "Item 1"},
				map[string]any{"ID": float64(2), "Title": "Item 2"},
			},
		},
	}
	rendered, err := c.RenderDigest(view, outbound.Target{
		URL:      "https://example.com/digest",
		Secret:   "digest-secret",
		TenantID: "t1",
	})
	if err != nil {
		t.Fatalf("RenderDigest: %v", err)
	}
	_, err = rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
}
