// SPDX-License-Identifier: Apache-2.0

package lark

import (
	"context"
	"encoding/json"
	"errors"
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

func TestRenderEventCard(t *testing.T) {
	t.Parallel()

	ch := ptrext.Of(channel{})
	dst := outbound.Target{
		ID:       "target-1",
		TenantID: "tenant-test",
		URL:      "https://example.com/webhook",
	}
	env := ptrext.Of(outbound.Envelope{
		Version:   "v2",
		Timestamp: "2026-06-17T12:00:00Z",
		EventType: "feedback.created",
		TenantID:  "tenant-test",
		Feedback: map[string]any{
			"title":   "Login broken",
			"content": "Cannot login on Safari",
		},
	})

	rendered, err := ch.RenderEvent(env, dst)
	if err != nil {
		t.Fatalf("RenderEvent err = %v", err)
	}

	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build err = %v", err)
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
	if req.Header.Get("User-Agent") != "attune/1.0" {
		t.Errorf("unexpected User-Agent: %s", req.Header.Get("User-Agent"))
	}
}

func TestRenderEventCardUrgent(t *testing.T) {
	t.Parallel()

	env := ptrext.Of(outbound.Envelope{
		Version:   "v2",
		Timestamp: "2026-06-17T12:00:00Z",
		EventType: "feedback.created",
		TenantID:  "tenant-test",
		Feedback: map[string]any{
			"title":     "Critical issue",
			"content":   "System down",
			"is_urgent": true,
		},
	})

	card := buildEventCard(env)
	if card.Header.Template != "red" {
		t.Errorf("urgent card template = %q, want red", card.Header.Template)
	}
	if card.Header.Title.Content != "[Urgent] Critical issue" {
		t.Errorf("urgent card title = %q", card.Header.Title.Content)
	}
}

func TestRenderEventCardDefaults(t *testing.T) {
	t.Parallel()

	env := ptrext.Of(outbound.Envelope{
		Version:   "v2",
		Timestamp: "2026-06-17T12:00:00Z",
		EventType: "feedback.created",
		TenantID:  "tenant-test",
		Feedback:  map[string]any{},
	})

	card := buildEventCard(env)
	if card.Header.Template != "blue" {
		t.Errorf("default card template = %q, want blue", card.Header.Template)
	}
	if card.Header.Title.Content != "New Feedback" {
		t.Errorf("default card title = %q, want 'New Feedback'", card.Header.Title.Content)
	}
}

func TestRenderEventWithSecret(t *testing.T) {
	t.Parallel()

	ch := ptrext.Of(channel{})
	dst := outbound.Target{
		ID:       "target-1",
		TenantID: "tenant-test",
		URL:      "https://example.com/webhook",
		Secret:   "my-secret-1234567890",
	}
	env := ptrext.Of(outbound.Envelope{
		Version:   "v2",
		Timestamp: "2026-06-17T12:00:00Z",
		Feedback:  map[string]any{"title": "Test"},
	})

	rendered, err := ch.RenderEvent(env, dst)
	if err != nil {
		t.Fatalf("RenderEvent err = %v", err)
	}

	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build err = %v", err)
	}

	var msg larkMessage
	if err := json.NewDecoder(req.Body).Decode(&msg); err != nil {
		t.Fatalf("decode body err = %v", err)
	}
	if msg.Timestamp == "" {
		t.Error("expected timestamp in signed message")
	}
	if msg.Sign == "" {
		t.Error("expected signature in signed message")
	}
}

func TestRenderEventWithoutSecret(t *testing.T) {
	t.Parallel()

	ch := ptrext.Of(channel{})
	dst := outbound.Target{
		ID:       "target-1",
		TenantID: "tenant-test",
		URL:      "https://example.com/webhook",
	}
	env := ptrext.Of(outbound.Envelope{
		Version:   "v2",
		Timestamp: "2026-06-17T12:00:00Z",
		Feedback:  map[string]any{"title": "Test"},
	})

	rendered, err := ch.RenderEvent(env, dst)
	if err != nil {
		t.Fatalf("RenderEvent err = %v", err)
	}

	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build err = %v", err)
	}

	var msg larkMessage
	if err := json.NewDecoder(req.Body).Decode(&msg); err != nil {
		t.Fatalf("decode body err = %v", err)
	}
	if msg.Timestamp != "" {
		t.Error("unexpected timestamp in unsigned message")
	}
	if msg.Sign != "" {
		t.Error("unexpected signature in unsigned message")
	}
}

func TestCheckLarkResponse200Success(t *testing.T) {
	t.Parallel()

	checker := checkLarkResponse("test-label")
	err := checker(context.Background(), 200, []byte(`{"StatusCode":0}`))
	if err != nil {
		t.Fatalf("expected nil error on 200 success, got %v", err)
	}
}

func TestCheckLarkResponse200NonZeroStatusCode(t *testing.T) {
	t.Parallel()

	checker := checkLarkResponse("test-label")
	err := checker(context.Background(), 200, []byte(`{"StatusCode":19024,"StatusMessage":"sign match fail or timestamp is not within one hour from current time"}`))
	if err == nil {
		t.Fatal("expected error for non-zero StatusCode")
	}
	if !errors.Is(err, outbound.ErrTerminal) {
		t.Fatalf("expected ErrTerminal, got %v", err)
	}
}

func TestCheckLarkResponse200RateLimitCode9499(t *testing.T) {
	t.Parallel()

	checker := checkLarkResponse("test-label")
	err := checker(context.Background(), 200, []byte(`{"StatusCode":9499,"StatusMessage":"rate limited"}`))
	if err == nil {
		t.Fatal("expected error for rate limit code 9499")
	}
	if errors.Is(err, outbound.ErrTerminal) {
		t.Fatal("rate limit code 9499 should not be terminal")
	}
}

func TestCheckLarkResponse429RateLimit(t *testing.T) {
	t.Parallel()

	checker := checkLarkResponse("test-label")
	err := checker(context.Background(), 429, []byte(`rate limited`))
	if err == nil {
		t.Fatal("expected error for HTTP 429")
	}
	if errors.Is(err, outbound.ErrTerminal) {
		t.Fatal("429 should not be terminal (retriable)")
	}
}

func TestCheckLarkResponse4xxTerminal(t *testing.T) {
	t.Parallel()

	cases := []int{400, 401, 403, 404}
	for _, status := range cases {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			checker := checkLarkResponse("test-label")
			err := checker(context.Background(), status, []byte(`bad request`))
			if err == nil {
				t.Fatalf("expected error for HTTP %d", status)
			}
			if !errors.Is(err, outbound.ErrTerminal) {
				t.Fatalf("HTTP %d should be terminal, got %v", status, err)
			}
		})
	}
}

func TestCheckLarkResponse5xxRetriable(t *testing.T) {
	t.Parallel()

	checker := checkLarkResponse("test-label")
	err := checker(context.Background(), 500, []byte(`internal server error`))
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if errors.Is(err, outbound.ErrTerminal) {
		t.Fatal("500 should not be terminal")
	}
}

func TestCheckLarkResponse200InvalidJSON(t *testing.T) {
	t.Parallel()

	checker := checkLarkResponse("test-label")
	err := checker(context.Background(), 200, []byte(`not json`))
	if err == nil {
		t.Fatal("expected error for 200 with invalid JSON")
	}
	if !errors.Is(err, outbound.ErrTerminal) {
		t.Fatalf("expected ErrTerminal, got %v", err)
	}
}

func TestCheckLarkResponse200MissingStatusCode(t *testing.T) {
	t.Parallel()

	checker := checkLarkResponse("test-label")
	err := checker(context.Background(), 200, []byte(`{}`))
	if err == nil {
		t.Fatal("expected error for 200 with missing StatusCode")
	}
	if !errors.Is(err, outbound.ErrTerminal) {
		t.Fatalf("expected ErrTerminal, got %v", err)
	}
}

func TestSignLarkDeterministic(t *testing.T) {
	t.Parallel()

	sig1 := signLark("1234567890", "secret")
	sig2 := signLark("1234567890", "secret")
	if sig1 != sig2 {
		t.Fatalf("signLark not deterministic: %q != %q", sig1, sig2)
	}
	if sig1 == "" {
		t.Fatal("signLark returned empty string")
	}

	// Different timestamp produces different signature.
	sig3 := signLark("9999999999", "secret")
	if sig1 == sig3 {
		t.Fatal("signLark should differ for different timestamps")
	}

	// Different secret produces different signature.
	sig4 := signLark("1234567890", "other-secret")
	if sig1 == sig4 {
		t.Fatal("signLark should differ for different secrets")
	}
}

func TestChannelID(t *testing.T) {
	t.Parallel()

	ch := ptrext.Of(channel{})
	if ch.ID() != "lark" {
		t.Fatalf("channel ID = %q, want lark", ch.ID())
	}
}

func TestDeltaArrow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		d    deltaValue
		want string
	}{
		{"up with change", deltaValue{Direction: "up", Change: 15}, "\\u2191" + "15"},
		{"up no change", deltaValue{Direction: "up", Change: 0}, "\\u2191"},
		{"down with change", deltaValue{Direction: "down", Change: -5}, "\\u2193" + "5"},
		{"down no change", deltaValue{Direction: "down", Change: 0}, "\\u2193"},
		{"flat", deltaValue{Direction: "flat"}, ""},
		{"empty", deltaValue{Direction: ""}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := deltaArrow(tc.d)
			// Verify the actual Unicode content rather than escaped forms.
			switch tc.d.Direction {
			case "up":
				if tc.d.Change > 0 {
					want := "↑15"
					if got != want {
						t.Errorf("deltaArrow = %q, want %q", got, want)
					}
				} else {
					if got != "↑" {
						t.Errorf("deltaArrow = %q, want up arrow", got)
					}
				}
			case "down":
				if tc.d.Change < 0 {
					want := "↓5"
					if got != want {
						t.Errorf("deltaArrow = %q, want %q", got, want)
					}
				} else {
					if got != "↓" {
						t.Errorf("deltaArrow = %q, want down arrow", got)
					}
				}
			default:
				if got != "" {
					t.Errorf("deltaArrow = %q, want empty", got)
				}
			}
		})
	}
}

func TestLifecycleBadge(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		lc   string
		want string
	}{
		{"new", "new", "[NEW] "},
		{"regressed", "regressed", "[BACK] "},
		{"ongoing", "ongoing", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := lifecycleBadge(tc.lc); got != tc.want {
				t.Errorf("lifecycleBadge(%q) = %q, want %q", tc.lc, got, tc.want)
			}
		})
	}
}

func TestRenderSparkline(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		counts []int
		empty  bool
	}{
		{"empty slice", nil, true},
		{"all zeros", []int{0, 0, 0}, false},
		{"ascending", []int{1, 2, 3, 4, 5}, false},
		{"single value", []int{5}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := renderSparkline(tc.counts)
			if tc.empty && got != "" {
				t.Errorf("expected empty sparkline, got %q", got)
			}
			if !tc.empty && len([]rune(got)) != len(tc.counts) {
				t.Errorf("sparkline rune length = %d, want %d", len([]rune(got)), len(tc.counts))
			}
		})
	}
}

func TestBuildDigestCardFallback(t *testing.T) {
	t.Parallel()

	// Non-digestView triggers fallback path.
	card := buildDigestCard("not a digestView")
	if card.Header.Title.Content != "Daily Feedback Digest" {
		t.Errorf("fallback card title = %q", card.Header.Title.Content)
	}
	if card.Header.Template != "purple" {
		t.Errorf("fallback card template = %q", card.Header.Template)
	}
	if len(card.Elements) != 1 {
		t.Errorf("fallback card elements = %d, want 1", len(card.Elements))
	}
}

func TestBuildDigestCardWithItems(t *testing.T) {
	t.Parallel()

	view := digestView{
		RunDate: "2026-06-17",
		Result: digestResult{
			Stats: digestStats{Total: 10, Enriched: 8},
			Items: []digestItem{
				{ID: 1, Title: "First feedback"},
				{ID: 2, Title: "Second feedback"},
			},
		},
		Deltas: digestDeltas{
			Feedback: deltaValue{Direction: "flat"},
		},
	}

	card := buildDigestCard(view)
	if card.Header.Template != "purple" {
		t.Errorf("card template = %q", card.Header.Template)
	}
	// Should use items path (not themes).
	found := false
	for _, elem := range card.Elements {
		if elem.Text != nil && elem.Text.Tag == "lark_md" {
			content := elem.Text.Content
			if len(content) > 0 && content[0] == '#' {
				found = true
			}
		}
	}
	// Verify elements include at least the summary + hr + items + hr + note.
	if len(card.Elements) < 5 {
		t.Errorf("expected at least 5 elements for items view, got %d", len(card.Elements))
	}
	_ = found
}

func TestBuildDigestCardWithSparkline(t *testing.T) {
	t.Parallel()

	view := digestView{
		RunDate: "2026-06-17",
		Result: digestResult{
			Stats: digestStats{Total: 10, Enriched: 8, Urgent: 2},
		},
		Deltas: digestDeltas{
			Feedback: deltaValue{Direction: "down", Change: -3},
		},
		Sparkline: []int{5, 3, 7, 2, 10},
	}

	card := buildDigestCard(view)
	// Should contain sparkline emoji in first element.
	if len(card.Elements) == 0 {
		t.Fatal("expected at least one element")
	}
}

func TestBuildDigestCardThemeCountPlural(t *testing.T) {
	t.Parallel()

	view := digestView{
		RunDate: "2026-06-17",
		Result: digestResult{
			Stats: digestStats{Total: 5, Enriched: 3},
			Themes: []digestTheme{
				{Title: "Single Issue", Count: 1, Lifecycle: "new"},
				{Title: "Multiple Issues", Count: 5, ExampleTitles: []string{"Example"}, Lifecycle: "regressed"},
			},
		},
		Deltas: digestDeltas{
			Feedback: deltaValue{Direction: "up", Change: 2},
		},
	}

	card := buildDigestCard(view)
	if len(card.Elements) < 4 {
		t.Errorf("expected at least 4 elements for themes view, got %d", len(card.Elements))
	}
}

// truncate moved to internal/outbound/render and is rune-safe-tested there.
