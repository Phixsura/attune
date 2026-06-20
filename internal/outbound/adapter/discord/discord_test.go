// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test fixtures construct Envelope literals inline.
package discord

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
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ---------------------------------------------------------------------------
// Severity → embed color mapping
// ---------------------------------------------------------------------------

func TestSeverityColor(t *testing.T) {
	tests := []struct {
		name     string
		severity string
		urgent   bool
		want     int
	}{
		{"critical -> red", "critical", false, colorRed},
		{"major -> orange", "major", false, colorOrange},
		{"minor -> yellow", "minor", false, colorYellow},
		{"unknown -> gray", "blocker", false, colorGray},
		{"empty -> gray", "", false, colorGray},
		{"case-insensitive Critical -> red", "Critical", false, colorRed},
		{"whitespace trimmed", "  major  ", false, colorOrange},
		{"urgent overrides minor -> red", "minor", true, colorRed},
		{"urgent with empty severity -> red", "", true, colorRed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := severityColor(tt.severity, tt.urgent); got != tt.want {
				t.Errorf("severityColor(%q, %v) = %d, want %d", tt.severity, tt.urgent, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Event embed rendering
// ---------------------------------------------------------------------------

func TestBuildEventEmbed_Normal(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":     "Login button broken",
			"content":   "Clicking the login button does nothing on Safari.",
			"is_urgent": false,
			"source":    "widget",
		},
	}

	embed := buildEventEmbed(env)

	if !strings.Contains(embed.Title, "Login button broken") {
		t.Errorf("title should contain feedback title, got %q", embed.Title)
	}
	if strings.Contains(embed.Title, "[Urgent]") {
		t.Errorf("normal event should not have [Urgent] prefix, got %q", embed.Title)
	}
	if embed.Color != colorGray {
		t.Errorf("normal event with no severity should be gray, got %d", embed.Color)
	}
	if !strings.Contains(embed.Description, "Safari") {
		t.Errorf("description should contain content, got %q", embed.Description)
	}
}

func TestBuildEventEmbed_Urgent(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":     "Payment failure",
			"content":   "All payment methods fail at checkout.",
			"is_urgent": true,
			"source":    "api",
		},
	}

	embed := buildEventEmbed(env)

	if !strings.Contains(embed.Title, "[Urgent]") {
		t.Errorf("urgent event should have [Urgent] prefix, got %q", embed.Title)
	}
	if embed.Color != colorRed {
		t.Errorf("urgent event should be red, got %d", embed.Color)
	}
}

func TestBuildEventEmbed_WithSeverity_NestedAttrs(t *testing.T) {
	// The outbox envelope nests severity/category inside enriched.attrs.
	env := &outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":   "Slow page load",
			"content": "Dashboard takes 10s to load.",
			"source":  "widget",
			"enriched": map[string]any{
				"attrs": map[string]any{
					"severity": "major",
					"category": "performance",
				},
			},
		},
	}

	embed := buildEventEmbed(env)

	if embed.Color != colorOrange {
		t.Errorf("major severity should map to orange, got %d", embed.Color)
	}
	if !hasField(embed, "Severity", "major") {
		t.Errorf("expected a Severity field with value 'major', fields=%+v", embed.Fields)
	}
	if !hasField(embed, "Category", "performance") {
		t.Errorf("expected a Category field with value 'performance', fields=%+v", embed.Fields)
	}
}

func TestBuildEventEmbed_EmptyTitle(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"content": "Some feedback content.",
			"source":  "api",
		},
	}

	embed := buildEventEmbed(env)

	if !strings.Contains(embed.Title, "New Feedback") {
		t.Errorf("empty title should default to 'New Feedback', got %q", embed.Title)
	}
}

// ---------------------------------------------------------------------------
// allowed_mentions guard — embeds plus an explicit empty parse list guarantee
// no @everyone / @here pings even if content ever leaks into the payload.
// ---------------------------------------------------------------------------

func TestRenderEvent_NoMentions(t *testing.T) {
	ch := ptrext.Of(channel{})
	env := &outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":   "@everyone please look",
			"content": "<!channel> urgent",
			"source":  "api",
		},
	}
	dst := outbound.Target{ID: "t1", TenantID: "tenant-1", URL: "https://discord.com/api/webhooks/x/y"}

	rendered, err := ch.RenderEvent(env, dst)
	if err != nil {
		t.Fatalf("RenderEvent: %v", err)
	}
	req, err := rendered.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	var msg discordMessage
	decodeBody(t, req, &msg)

	if msg.AllowedMentions.Parse == nil {
		t.Fatalf("allowed_mentions.parse must be present (empty slice), got nil")
	}
	if len(msg.AllowedMentions.Parse) != 0 {
		t.Errorf("allowed_mentions.parse must be empty to suppress pings, got %v", msg.AllowedMentions.Parse)
	}
}

// ---------------------------------------------------------------------------
// checkDiscord response checker — 204 (Discord success), 429, 4xx
// ---------------------------------------------------------------------------

func TestCheckDiscord(t *testing.T) {
	check := checkDiscord("test-label")
	ctx := context.Background()

	tests := []struct {
		name      string
		status    int
		wantNil   bool
		wantTerm  bool
		wantRetry bool
	}{
		{"204 no content ok", 204, true, false, false},
		{"200 ok", 200, true, false, false},
		{"408 retryable", 408, false, false, true},
		{"429 retryable", 429, false, false, true},
		{"400 terminal", 400, false, true, false},
		{"403 terminal", 403, false, true, false},
		{"404 terminal (deleted webhook)", 404, false, true, false},
		{"500 retryable", 500, false, false, true},
		{"503 retryable", 503, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := check(ctx, tt.status, []byte("body"))
			if tt.wantNil && err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
			if !tt.wantNil && err == nil {
				t.Errorf("expected error, got nil")
			}
			if tt.wantTerm && !errors.Is(err, outbound.ErrTerminal) {
				t.Errorf("expected terminal error, got %v", err)
			}
			if tt.wantRetry && errors.Is(err, outbound.ErrTerminal) {
				t.Errorf("expected retryable error, got terminal: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// httptest send integration
// ---------------------------------------------------------------------------

func TestRenderEvent_HttptestSend_204(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = readBody(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	ch := ptrext.Of(channel{})
	env := &outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-test",
		Feedback:  map[string]any{"title": "Test", "content": "Hello", "source": "test"},
	}
	dst := outbound.Target{ID: "target-1", TenantID: "tenant-test", URL: srv.URL}

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

	if checkErr := rendered.Check(context.Background(), resp.StatusCode, nil); checkErr != nil {
		t.Errorf("Check should return nil on 204, got %v", checkErr)
	}

	var msg discordMessage
	if err := json.Unmarshal(received, &msg); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("unmarshal received body: %v", err)
	}
	if len(msg.Embeds) == 0 {
		t.Errorf("expected non-empty embeds in sent message")
	}
}

func TestRenderEvent_HttptestSend_429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"retry_after":5.0,"global":false}`))
	}))
	defer srv.Close()

	ch := ptrext.Of(channel{})
	env := &outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-test",
		Feedback:  map[string]any{"title": "Test", "content": "Hello", "source": "test"},
	}
	dst := outbound.Target{ID: "target-1", TenantID: "tenant-test", URL: srv.URL}

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
	body, _ := io.ReadAll(resp.Body)

	checkErr := rendered.Check(context.Background(), resp.StatusCode, body)
	if checkErr == nil {
		t.Fatal("expected retryable error on 429")
	}
	if errors.Is(checkErr, outbound.ErrTerminal) {
		t.Errorf("429 should be retryable, not terminal")
	}
}

// ---------------------------------------------------------------------------
// Digest embed rendering
// ---------------------------------------------------------------------------

func TestBuildDigestEmbed_WithThemes(t *testing.T) {
	view := map[string]any{
		"run_date":  "2026-06-20",
		"tenant_id": "tenant-1",
		"result": map[string]any{
			"Stats":  map[string]any{"Total": 12, "Enriched": 10, "Urgent": 2},
			"Themes": []any{map[string]any{"Title": "Checkout fails", "Count": 5}},
		},
	}

	embed := buildDigestEmbed(view)

	if !strings.Contains(embed.Title, "Digest") {
		t.Errorf("digest title should mention Digest, got %q", embed.Title)
	}
	combined := embed.Description + fieldsText(embed)
	if !strings.Contains(combined, "Checkout fails") {
		t.Errorf("digest should surface theme title, got %q", combined)
	}
}

func TestBuildDigestEmbed_WithItemsNoThemes(t *testing.T) {
	// Live production branch (digest/sender.go → LookupDigest): a digest with
	// recent items but no clustered themes renders the items list.
	view := map[string]any{
		"run_date":  "2026-06-20",
		"tenant_id": "tenant-1",
		"result": map[string]any{
			"Stats": map[string]any{"Total": 3, "Enriched": 3, "Urgent": 1},
			"Items": []any{
				map[string]any{"ID": 42, "Title": "Export button broken"},
				map[string]any{"ID": 43, "Title": "Slow dashboard"},
			},
		},
	}

	embed := buildDigestEmbed(view)

	if !strings.Contains(embed.Description, "#42") || !strings.Contains(embed.Description, "Export button broken") {
		t.Errorf("items branch should render '#42 Export button broken', got %q", embed.Description)
	}
	if !strings.Contains(embed.Description, "urgent") {
		t.Errorf("Urgent>0 should surface an urgent line, got %q", embed.Description)
	}
}

func TestBuildDigestEmbed_FallbackUnknownView(t *testing.T) {
	embed := buildDigestEmbed(map[string]any{"unexpected": "shape"})
	if embed.Title == "" {
		t.Errorf("fallback digest must still produce a title")
	}
	// Strengthened: the fallback must actually fence the JSON and surface the
	// unknown content, with the gray color — not just any non-empty title.
	if !strings.Contains(embed.Description, "```") || !strings.Contains(embed.Description, "unexpected") {
		t.Errorf("fallback should code-fence the unknown view JSON, got %q", embed.Description)
	}
	if embed.Color != colorGray {
		t.Errorf("fallback color should be gray, got %d", embed.Color)
	}
}

// Digest path goes through the same render()/Build closure as events — assert
// it ships allowed_mentions:{parse:[]} and is sent as JSON.
func TestRenderDigest_HttptestSend_204(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = readBody(r)
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	ch := ptrext.Of(channel{})
	view := map[string]any{
		"run_date":  "2026-06-20",
		"tenant_id": "tenant-1",
		"result": map[string]any{
			"Stats":  map[string]any{"Total": 5, "Enriched": 5, "Urgent": 2},
			"Themes": []any{map[string]any{"Title": "Checkout fails", "Count": 4}},
		},
	}
	dst := outbound.Target{ID: "t1", TenantID: "tenant-1", URL: srv.URL}

	rendered, err := ch.RenderDigest(view, dst)
	if err != nil {
		t.Fatalf("RenderDigest: %v", err)
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
	if checkErr := rendered.Check(context.Background(), resp.StatusCode, nil); checkErr != nil {
		t.Errorf("Check should be nil on 204, got %v", checkErr)
	}

	var msg discordMessage
	if err := json.Unmarshal(received, &msg); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("unmarshal digest body: %v", err)
	}
	if len(msg.Embeds) == 0 {
		t.Errorf("digest must ship at least one embed")
	}
	if msg.AllowedMentions.Parse == nil || len(msg.AllowedMentions.Parse) != 0 {
		t.Errorf("digest must pin allowed_mentions.parse to empty, got %v", msg.AllowedMentions.Parse)
	}
	if !strings.Contains(msg.Embeds[0].Description, "urgent") {
		t.Errorf("digest with Urgent>0 should render urgent line, got %q", msg.Embeds[0].Description)
	}
}

// Discord rejects a non-empty but malformed embed timestamp with a 400, which
// would burn outbox retries. A malformed upstream timestamp must be dropped.
func TestBuildEventEmbed_MalformedTimestampDropped(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		Timestamp: "not-a-timestamp",
		TenantID:  "tenant-1",
		Feedback:  map[string]any{"title": "X", "content": "Y", "source": "api"},
	}
	embed := buildEventEmbed(env)
	if embed.Timestamp != "" {
		t.Errorf("malformed timestamp must be dropped, got %q", embed.Timestamp)
	}
}

func TestBuildEventEmbed_ValidTimestampKept(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback:  map[string]any{"title": "X", "content": "Y", "source": "api"},
	}
	embed := buildEventEmbed(env)
	if embed.Timestamp != "2026-06-20T12:00:00Z" {
		t.Errorf("valid RFC3339 timestamp must be kept, got %q", embed.Timestamp)
	}
}

func TestBuildEventEmbed_MinorSeverityYellow(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":   "Typo in tooltip",
			"content": "Minor copy issue.",
			"source":  "widget",
			"enriched": map[string]any{
				"attrs": map[string]any{"severity": "minor", "category": "ui"},
			},
		},
	}
	embed := buildEventEmbed(env)
	if embed.Color != colorYellow {
		t.Errorf("minor severity should map to yellow, got %d", embed.Color)
	}
}

// ---------------------------------------------------------------------------
// Embed limits — total assembled embed must stay within Discord's 6000-rune cap
// ---------------------------------------------------------------------------

func TestBuildEventEmbed_WithinTotalLimit(t *testing.T) {
	huge := strings.Repeat("x", 10000)
	env := &outbound.Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":   huge,
			"content": huge,
			"source":  huge,
			"enriched": map[string]any{
				"attrs": map[string]any{"severity": huge, "category": huge},
			},
		},
	}

	embed := buildEventEmbed(env)

	if total := embedTotalRunes(embed); total > embedTotalMax {
		t.Errorf("assembled embed = %d runes, exceeds Discord cap %d", total, embedTotalMax)
	}
	if titleRunes := len([]rune(embed.Title)); titleRunes > embedTitleMax {
		t.Errorf("title = %d runes, exceeds %d", titleRunes, embedTitleMax)
	}
	if descRunes := len([]rune(embed.Description)); descRunes > embedDescriptionMax {
		t.Errorf("description = %d runes, exceeds %d", descRunes, embedDescriptionMax)
	}
}

// (truncate/mapStr/severity-category moved to internal/outbound/render and are
// tested there; the embed-level behavior is still covered above.)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func hasField(e discordEmbed, name, value string) bool {
	for _, f := range e.Fields {
		if strings.Contains(f.Name, name) && strings.Contains(f.Value, value) {
			return true
		}
	}
	return false
}

func fieldsText(e discordEmbed) string {
	var b strings.Builder
	for _, f := range e.Fields {
		b.WriteString(f.Name)
		b.WriteString(f.Value)
	}
	return b.String()
}

func embedTotalRunes(e discordEmbed) int {
	total := len([]rune(e.Title)) + len([]rune(e.Description))
	for _, f := range e.Fields {
		total += len([]rune(f.Name)) + len([]rune(f.Value))
	}
	if e.Footer != nil {
		total += len([]rune(e.Footer.Text))
	}
	return total
}

func decodeBody(t *testing.T, req *http.Request, v any) {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(body, v); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("unmarshal body: %v", err)
	}
}

func readBody(r *http.Request) []byte {
	body, _ := io.ReadAll(r.Body)
	return body
}
