// SPDX-License-Identifier: Apache-2.0

package lark

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestRenderDigestCard(t *testing.T) {
	view := digestView{
		TenantID: "tenant-test",
		RunDate:  "2026-06-15",
		Result: digestResult{
			Stats: digestStats{
				Total:    47,
				Enriched: 45,
				Urgent:   3,
			},
			Themes: []digestTheme{
				{Title: "Safari Checkout Issues", Count: 12, ExampleTitles: []string{"Checkout button does nothing on Safari"}, Lifecycle: "new"},
				{Title: "Export Timeout Problems", Count: 8, ExampleTitles: []string{"CSV export times out"}, Lifecycle: "ongoing"},
			},
		},
		Deltas: digestDeltas{
			Feedback: deltaValue{Current: 47, Prior: 32, Change: 15, Direction: "up"},
			Enriched: deltaValue{Current: 45, Prior: 30, Change: 15, Direction: "up"},
			Urgent:   deltaValue{Current: 3, Prior: 5, Change: -2, Direction: "down"},
		},
		Sparkline: []int{12, 8, 15, 22, 18, 25, 47},
	}

	card := buildDigestCard(view)

	if card.Header.Title.Content != "📊 Daily Digest — 2026-06-15" {
		t.Errorf("unexpected header: %q", card.Header.Title.Content)
	}
	if card.Header.Template != "purple" {
		t.Errorf("unexpected template: %q", card.Header.Template)
	}
	if len(card.Elements) < 3 {
		t.Errorf("expected at least 3 elements, got %d", len(card.Elements))
	}

	b, _ := json.MarshalIndent(card, "", "  ")
	t.Logf("Lark card:\n%s", b)
}

func TestRenderDigestCard_Integration(t *testing.T) {
	ch := ptrext.Of(channel{})
	dst := outbound.Target{
		ID:       "target-1",
		TenantID: "tenant-test",
		URL:      "https://example.com/webhook",
		Secret:   "test-secret-1234567890",
	}

	view := map[string]any{
		"tenant_id": "tenant-test",
		"run_date":  "2026-06-15",
		"result": map[string]any{
			"Stats": map[string]any{
				"Total":    float64(47),
				"Enriched": float64(45),
				"Urgent":   float64(3),
			},
			"Themes": []any{
				map[string]any{"Title": "Safari Issues", "Count": float64(12), "ExampleTitles": []any{"Checkout fails"}, "Lifecycle": "new"},
			},
			"Items": nil,
		},
		"deltas": map[string]any{
			"feedback": map[string]any{"current": float64(47), "prior": float64(32), "change": float64(15), "direction": "up"},
			"enriched": map[string]any{"current": float64(45), "prior": float64(30), "change": float64(15), "direction": "up"},
			"urgent":   map[string]any{"current": float64(3), "prior": float64(5), "change": float64(-2), "direction": "down"},
		},
		"sparkline": []any{float64(12), float64(8), float64(15), float64(22), float64(18), float64(25), float64(47)},
	}

	rendered, err := ch.RenderDigest(view, dst)
	if err != nil {
		t.Fatalf("RenderDigest failed: %v", err)
	}

	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if req.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", req.Method)
	}
	if req.URL.String() != "https://example.com/webhook" {
		t.Errorf("unexpected URL: %s", req.URL)
	}
	if req.Header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("unexpected Content-Type: %s", req.Header.Get("Content-Type"))
	}

	t.Log("Lark RenderDigest integration test passed")
}
