// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test fixtures construct Envelope literals inline.
package teams

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
)

// ---------------------------------------------------------------------------
// Event card rendering
// ---------------------------------------------------------------------------

func TestBuildEventCard_Normal(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		Timestamp: "2026-06-29T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":     "Login button broken",
			"content":   "Clicking the login button does nothing on Safari.",
			"is_urgent": false,
			"source":    "widget",
		},
	}

	card := buildEventCard(env)

	if card.Type != "AdaptiveCard" {
		t.Errorf("expected AdaptiveCard, got %s", card.Type)
	}
	if len(card.Body) < 2 {
		t.Fatalf("expected at least 2 body elements, got %d", len(card.Body))
	}
	if !strings.Contains(card.Body[0].Text, "Login button broken") {
		t.Errorf("title should contain feedback title, got %q", card.Body[0].Text)
	}
	if card.Body[0].Color == "attention" {
		t.Error("normal event should not have attention color")
	}
}

func TestBuildEventCard_Urgent(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":     "Data loss",
			"content":   "All records missing.",
			"is_urgent": true,
			"source":    "api",
		},
	}

	card := buildEventCard(env)

	if !strings.Contains(card.Body[0].Text, "[Urgent]") {
		t.Errorf("urgent event should have [Urgent] prefix, got %q", card.Body[0].Text)
	}
	if card.Body[0].Color != "attention" {
		t.Errorf("urgent event should use attention color, got %q", card.Body[0].Color)
	}
}

func TestBuildEventCard_Enriched(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"content": "Slow page loads",
			"enriched": map[string]any{
				"title":    "Performance Issue",
				"severity": "major",
				"kind":     "bug",
			},
		},
	}

	card := buildEventCard(env)
	found := cardToJSON(t, card)

	if !strings.Contains(found, "Performance Issue") {
		t.Error("card should use enriched title")
	}
	if !strings.Contains(found, "major") {
		t.Error("card should include severity")
	}
}

// ---------------------------------------------------------------------------
// Digest card rendering
// ---------------------------------------------------------------------------

func TestBuildDigestCard_Themes(t *testing.T) {
	view := digestView{
		TenantID: "t1",
		RunDate:  "2026-06-29",
		Result: digestResult{
			Stats:  digestStats{Total: 50, Enriched: 40, Urgent: 3},
			Themes: []digestTheme{{Title: "Login", Count: 10}, {Title: "Perf", Count: 8}},
		},
	}

	card := buildDigestCard(view)
	found := cardToJSON(t, card)

	if !strings.Contains(found, "50") {
		t.Error("digest should include total count")
	}
	if !strings.Contains(found, "Login") {
		t.Error("digest should include theme title")
	}
	if !strings.Contains(found, "3 urgent") {
		t.Error("digest should mention urgent count")
	}
}

func TestBuildDigestCard_Items(t *testing.T) {
	view := digestView{
		TenantID: "t1",
		RunDate:  "2026-06-29",
		Result: digestResult{
			Stats: digestStats{Total: 5, Enriched: 5},
			Items: []digestItem{{ID: 42, Title: "Bug report"}},
		},
	}

	card := buildDigestCard(view)
	found := cardToJSON(t, card)

	if !strings.Contains(found, "Bug report") {
		t.Error("digest should include item title")
	}
}

func TestBuildDigestCard_FallbackJSON(t *testing.T) {
	card := buildDigestCard("not a digest view")
	if len(card.Body) < 2 {
		t.Fatal("fallback card should have body elements")
	}
}

// ---------------------------------------------------------------------------
// Round-trip: render → HTTP → check
// ---------------------------------------------------------------------------

func TestRenderEvent_RoundTrip(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := &channel{}
	env := &outbound.Envelope{
		Version:  "2",
		TenantID: "t1",
		Feedback: map[string]any{"content": "test feedback", "title": "Test"},
	}
	dst := outbound.Target{TenantID: "t1", URL: srv.URL}

	rendered, err := ch.RenderEvent(env, dst)
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}

	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if err := rendered.Check(context.Background(), resp.StatusCode, nil); err != nil {
		t.Fatalf("Check: %v", err)
	}

	var msg teamsMessage
	if err := json.Unmarshal(gotBody, &msg); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("unmarshal body: %v", err)
	}
	if msg.Type != "message" {
		t.Errorf("message type = %q, want %q", msg.Type, "message")
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(msg.Attachments))
	}
	if msg.Attachments[0].Content.Type != "AdaptiveCard" {
		t.Errorf("attachment content type = %q", msg.Attachments[0].Content.Type)
	}
}

// ---------------------------------------------------------------------------
// Response checker
// ---------------------------------------------------------------------------

func TestCheckTeams(t *testing.T) {
	check := checkTeams("test")
	ctx := context.Background()

	if err := check(ctx, 200, nil); err != nil {
		t.Errorf("200 should succeed: %v", err)
	}
	if err := check(ctx, 204, nil); err != nil {
		t.Errorf("204 should succeed: %v", err)
	}
	if err := check(ctx, 429, nil); err == nil {
		t.Error("429 should be retryable error")
	} else if errors.Is(err, outbound.ErrTerminal) {
		t.Error("429 should NOT be terminal")
	}
	if err := check(ctx, 400, []byte("bad request")); err == nil {
		t.Error("400 should be terminal")
	} else if !errors.Is(err, outbound.ErrTerminal) {
		t.Error("400 should be ErrTerminal")
	}
}

// helpers
func cardToJSON(t *testing.T, card adaptiveCard) string {
	t.Helper()
	b, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal card: %v", err)
	}
	return string(b)
}
