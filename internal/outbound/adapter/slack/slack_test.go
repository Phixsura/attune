// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test fixtures construct Envelope literals inline.
package slack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ---------------------------------------------------------------------------
// Event rendering
// ---------------------------------------------------------------------------

func TestRenderEventBlocks_Normal(t *testing.T) {
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

	blocks := buildEventBlocks(env)

	if len(blocks) < 3 {
		t.Fatalf("expected at least 3 blocks, got %d", len(blocks))
	}
	if blocks[0].Type != "header" {
		t.Errorf("first block should be header, got %s", blocks[0].Type)
	}
	if blocks[0].Text == nil || !strings.Contains(blocks[0].Text.Text, ":speech_balloon:") {
		t.Errorf("normal event should use speech_balloon emoji")
	}
	if strings.Contains(blocks[0].Text.Text, "[Urgent]") {
		t.Errorf("normal event should not have [Urgent] prefix")
	}

	if blocks[1].Type != "section" {
		t.Errorf("second block should be section, got %s", blocks[1].Type)
	}
}

func TestRenderEventBlocks_Urgent(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":     "Payment failure",
			"content":   "All payment methods fail at checkout.",
			"is_urgent": true,
			"source":    "api",
		},
	}

	blocks := buildEventBlocks(env)

	if blocks[0].Text == nil || !strings.Contains(blocks[0].Text.Text, ":rotating_light:") {
		t.Errorf("urgent event should use rotating_light emoji")
	}
	if !strings.Contains(blocks[0].Text.Text, "[Urgent]") {
		t.Errorf("urgent event should have [Urgent] prefix")
	}
}

func TestRenderEventBlocks_WithSeverity(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":   "Slow page load",
			"content": "Dashboard takes 10s to load.",
			"source":  "widget",
			"enriched": map[string]any{
				"severity": "high",
				"category": "performance",
			},
		},
	}

	blocks := buildEventBlocks(env)

	found := false
	for _, b := range blocks {
		if b.Type == "section" && b.Text != nil &&
			strings.Contains(b.Text.Text, "Severity:") &&
			strings.Contains(b.Text.Text, "high") &&
			strings.Contains(b.Text.Text, "Category:") &&
			strings.Contains(b.Text.Text, "performance") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a section block with severity and category fields")
	}
}

func TestRenderEventBlocks_EmptyTitle(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"content": "Some feedback content.",
			"source":  "api",
		},
	}

	blocks := buildEventBlocks(env)

	if blocks[0].Text == nil || !strings.Contains(blocks[0].Text.Text, "New Feedback") {
		t.Errorf("empty title should default to 'New Feedback'")
	}
}

// ---------------------------------------------------------------------------
// checkSlack response checker
// ---------------------------------------------------------------------------

func TestCheckSlack(t *testing.T) {
	check := checkSlack("test-label")
	ctx := context.Background()

	tests := []struct {
		name      string
		status    int
		body      string
		wantNil   bool
		wantTerm  bool
		wantRetry bool
	}{
		{"200 ok", 200, "ok", true, false, false},
		{"201 ok", 201, "", true, false, false},
		{"408 retryable", 408, "timeout", false, false, true},
		{"429 retryable", 429, "rate limited", false, false, true},
		{"400 terminal", 400, "invalid_payload", false, true, false},
		{"403 terminal", 403, "token_revoked", false, true, false},
		{"500 retryable", 500, "internal error", false, false, true},
		{"503 retryable", 503, "unavailable", false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := check(ctx, tt.status, []byte(tt.body))
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

func TestRenderEvent_HttptestSend_200(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = readAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ch := ptrext.Of(channel{})
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "test",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-test",
		Feedback: map[string]any{
			"title":   "Test",
			"content": "Hello",
			"source":  "test",
		},
	}
	dst := outbound.Target{
		ID:       "target-1",
		TenantID: "tenant-test",
		URL:      srv.URL,
	}

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
	body, _ := readAll(resp.Body)

	if checkErr := rendered.Check(context.Background(), resp.StatusCode, body); checkErr != nil {
		t.Errorf("Check should return nil on 200, got %v", checkErr)
	}

	var msg slackMessage
	if err := json.Unmarshal(received, &msg); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("unmarshal received body: %v", err)
	}
	if len(msg.Blocks) == 0 {
		t.Errorf("expected non-empty blocks in sent message")
	}
}

func TestRenderEvent_HttptestSend_429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	ch := ptrext.Of(channel{})
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "test",
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
	body, _ := readAll(resp.Body)

	checkErr := rendered.Check(context.Background(), resp.StatusCode, body)
	if checkErr == nil {
		t.Fatal("expected retryable error on 429")
	}
	if errors.Is(checkErr, outbound.ErrTerminal) {
		t.Errorf("429 should be retryable, not terminal")
	}
	if !strings.Contains(checkErr.Error(), "retryable") {
		t.Errorf("error should mention retryable: %v", checkErr)
	}
}

// truncate/mapStr/severity-category moved to internal/outbound/render and are
// tested there; the Block Kit behavior is still covered above.

// ---------------------------------------------------------------------------
// Digest rendering (existing)
// ---------------------------------------------------------------------------

func TestRenderDigestBlocks(t *testing.T) {
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

	blocks := buildDigestBlocks(view)

	if len(blocks) < 4 {
		t.Errorf("expected at least 4 blocks, got %d", len(blocks))
	}
	if blocks[0].Type != "header" {
		t.Errorf("expected header block, got %s", blocks[0].Type)
	}

	b, _ := json.MarshalIndent(blocks, "", "  ")
	t.Logf("Slack blocks:\n%s", b)
}

func TestRenderDigestBlocks_Integration(t *testing.T) {
	ch := ptrext.Of(channel{})
	dst := outbound.Target{
		ID:       "target-1",
		TenantID: "tenant-test",
		URL:      "https://hooks.slack.com/services/xxx",
		Secret:   "",
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
	if req.Header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("unexpected Content-Type: %s", req.Header.Get("Content-Type"))
	}
}

// ---------------------------------------------------------------------------
// Block Kit limit enforcement
// ---------------------------------------------------------------------------

func TestRenderEventBlocks_LongTitleTruncatedToHeaderLimit(t *testing.T) {
	longTitle := strings.Repeat("A", 200)
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":   longTitle,
			"content": "body",
			"source":  "web",
		},
	}

	blocks := buildEventBlocks(env)

	headerText := blocks[0].Text.Text
	if len([]rune(headerText)) > headerMaxChars+3 {
		t.Errorf("header %d runes exceeds limit %d", len([]rune(headerText)), headerMaxChars)
	}
	if !strings.HasSuffix(headerText, "...") {
		t.Errorf("truncated header should end with '...', got %q", headerText[len(headerText)-10:])
	}
}

func TestRenderEventBlocks_UrgentLongTitleTruncated(t *testing.T) {
	longTitle := strings.Repeat("B", 200)
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":     longTitle,
			"content":   "body",
			"is_urgent": true,
			"source":    "api",
		},
	}

	blocks := buildEventBlocks(env)

	headerText := blocks[0].Text.Text
	if len([]rune(headerText)) > headerMaxChars+3 {
		t.Errorf("urgent header %d runes exceeds limit %d", len([]rune(headerText)), headerMaxChars)
	}
	if !strings.Contains(headerText, ":rotating_light:") {
		t.Errorf("urgent header should have rotating_light emoji")
	}
	if !strings.Contains(headerText, "[Urgent]") {
		t.Errorf("urgent header should have [Urgent] prefix")
	}
}

func TestRenderEventBlocks_NilFeedbackMap(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback:  nil,
	}

	blocks := buildEventBlocks(env)

	if len(blocks) < 3 {
		t.Fatalf("expected at least 3 blocks from nil feedback, got %d", len(blocks))
	}
	if !strings.Contains(blocks[0].Text.Text, "New Feedback") {
		t.Errorf("nil feedback title should default to 'New Feedback'")
	}
}

func TestRenderEventBlocks_EmptyContent(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":  "Title",
			"source": "web",
		},
	}

	blocks := buildEventBlocks(env)

	if blocks[1].Type != "section" {
		t.Fatalf("expected section, got %s", blocks[1].Type)
	}
	if blocks[1].Text.Text != "" {
		t.Errorf("empty content should produce empty section text, got %q", blocks[1].Text.Text)
	}
}

func TestRenderEventBlocks_StructuralOrder(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":   "Test",
			"content": "Body",
			"source":  "web",
			"enriched": map[string]any{
				"severity": "high",
				"category": "ux",
			},
		},
	}

	blocks := buildEventBlocks(env)

	expected := []string{"header", "section", "section", "divider", "context"}
	if len(blocks) != len(expected) {
		t.Fatalf("expected %d blocks, got %d", len(expected), len(blocks))
	}
	for i, want := range expected {
		if blocks[i].Type != want {
			t.Errorf("block[%d] type = %s, want %s", i, blocks[i].Type, want)
		}
	}
	if len(blocks[4].Elements) != 1 {
		t.Errorf("context block should have exactly 1 element, got %d", len(blocks[4].Elements))
	}
}

func TestRenderEvent_HttptestSend_ContentType(t *testing.T) {
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := ptrext.Of(channel{})
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "test",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-test",
		Feedback:  map[string]any{"title": "T", "content": "C", "source": "s"},
	}
	dst := outbound.Target{ID: "t1", TenantID: "tenant-test", URL: srv.URL}

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
	resp.Body.Close()

	if gotContentType != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", gotContentType)
	}
}

func TestRenderEvent_HttptestSend_408Retryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestTimeout)
	}))
	defer srv.Close()

	ch := ptrext.Of(channel{})
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "test",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-test",
		Feedback:  map[string]any{"title": "T", "content": "C", "source": "s"},
	}
	dst := outbound.Target{ID: "t1", TenantID: "tenant-test", URL: srv.URL}

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
	body, _ := readAll(resp.Body)

	checkErr := rendered.Check(context.Background(), resp.StatusCode, body)
	if checkErr == nil {
		t.Fatal("expected error on 408")
	}
	if errors.Is(checkErr, outbound.ErrTerminal) {
		t.Errorf("408 should be retryable, not terminal")
	}
}

func TestRenderDigestBlocks_ManyThemesTruncated(t *testing.T) {
	themes := make([]digestTheme, 50)
	for i := range themes {
		themes[i] = digestTheme{
			Title:         strings.Repeat("Theme title with enough length to fill space ", 3),
			Count:         i + 1,
			ExampleTitles: []string{strings.Repeat("Example feedback title ", 5)},
			Lifecycle:     "ongoing",
		}
	}

	view := digestView{
		TenantID: "tenant-test",
		RunDate:  "2026-06-20",
		Result:   digestResult{Stats: digestStats{Total: 100, Enriched: 90}, Themes: themes},
		Deltas:   digestDeltas{Feedback: deltaValue{Direction: "flat"}},
	}

	blocks := buildDigestBlocks(view)

	for _, b := range blocks {
		if b.Type == "section" && b.Text != nil {
			runes := []rune(b.Text.Text)
			if len(runes) > sectionMaxChars+3 {
				t.Errorf("section text %d runes exceeds limit %d", len(runes), sectionMaxChars)
			}
		}
	}
}

func TestRenderDigestBlocks_EmptyThemesAndItems(t *testing.T) {
	view := digestView{
		TenantID: "tenant-test",
		RunDate:  "2026-06-20",
		Result:   digestResult{Stats: digestStats{Total: 0, Enriched: 0}},
		Deltas:   digestDeltas{Feedback: deltaValue{Direction: "flat"}},
	}

	blocks := buildDigestBlocks(view)

	expected := []string{"header", "section", "divider", "divider", "context"}
	if len(blocks) != len(expected) {
		t.Fatalf("expected %d blocks for empty digest, got %d", len(expected), len(blocks))
	}
	for i, want := range expected {
		if blocks[i].Type != want {
			t.Errorf("block[%d] type = %s, want %s", i, blocks[i].Type, want)
		}
	}
}

func TestRenderEventBlocks_MrkdwnSpecialCharsPreserved(t *testing.T) {
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":   `User says *bold* & <html> "quotes"`,
			"content": "Line 1\nLine 2\n*emphasis* _italic_ ~strike~ `code` <@U123>",
			"source":  "api",
		},
	}

	blocks := buildEventBlocks(env)

	payload, err := json.Marshal(slackMessage{Blocks: blocks})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !json.Valid(payload) {
		t.Fatal("payload must be valid JSON after special chars")
	}
	if strings.Contains(blocks[1].Text.Text, "<@U123>") {
		t.Error("raw <@U123> must be escaped to prevent unwanted mentions")
	}
	if !strings.Contains(blocks[1].Text.Text, "&lt;@U123&gt;") {
		t.Error("user mention should be escaped in mrkdwn content")
	}
	if !strings.Contains(blocks[1].Text.Text, "Line 1\nLine 2") {
		t.Error("newlines should be preserved in content")
	}
	if !strings.Contains(blocks[1].Text.Text, "*emphasis*") {
		t.Error("mrkdwn formatting (bold/italic) should pass through")
	}
}

func TestRenderDigestBlocks_AllZeroSparkline(t *testing.T) {
	result := renderSparkline([]int{0, 0, 0, 0, 0, 0, 0})
	if len([]rune(result)) != 7 {
		t.Errorf("expected 7 bars, got %d", len([]rune(result)))
	}
	for _, r := range result {
		if r != '▁' {
			t.Errorf("zero sparkline should all be ▁, got %c", r)
		}
	}
}

// ---------------------------------------------------------------------------
// mrkdwn injection
// ---------------------------------------------------------------------------

func TestEscapeMrkdwn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"hello", "hello"},
		{"<script>", "&lt;script&gt;"},
		{"<!channel>", "&lt;!channel&gt;"},
		{"a & b < c > d", "a &amp; b &lt; c &gt; d"},
		{"&amp;", "&amp;amp;"},
	}
	for _, tc := range cases {
		got := escapeMrkdwn(tc.in)
		if got != tc.want {
			t.Errorf("escapeMrkdwn(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRenderEvent_MrkdwnInjectionEscaped(t *testing.T) {
	t.Parallel()
	blocks := buildInjectionTestBlocks(t)

	t.Run("header_preserves_raw", func(t *testing.T) {
		hdr := blocks[0]
		if hdr.Type != "header" || hdr.Text == nil {
			t.Fatalf("block[0] not a header: %v", hdr)
		}
		if !strings.Contains(hdr.Text.Text, "<!channel>") {
			t.Error("header plain_text should preserve raw <!channel>")
		}
	})

	t.Run("content_escaped", func(t *testing.T) {
		sec := blocks[1]
		if sec.Text == nil {
			t.Fatal("block[1] has no text")
		}
		if strings.Contains(sec.Text.Text, "<!here>") {
			t.Error("content mrkdwn must not contain raw <!here>")
		}
		if !strings.Contains(sec.Text.Text, "&lt;!here&gt;") {
			t.Error("content should contain escaped &lt;!here&gt;")
		}
		if !strings.Contains(sec.Text.Text, "&amp;") {
			t.Error("& in content should be escaped to &amp;")
		}
	})

	t.Run("enriched_escaped", func(t *testing.T) {
		enrichSec := blocks[2]
		if enrichSec.Text == nil {
			t.Fatal("block[2] has no text")
		}
		if !strings.Contains(enrichSec.Text.Text, "&lt;high&gt;") {
			t.Errorf("severity should be escaped, got: %s", enrichSec.Text.Text)
		}
		if !strings.Contains(enrichSec.Text.Text, "A &amp; B") {
			t.Errorf("category & should be escaped, got: %s", enrichSec.Text.Text)
		}
	})

	t.Run("context_source_escaped", func(t *testing.T) {
		ctxText := findContextText(t, blocks)
		if strings.Contains(ctxText, "<!everyone>") {
			t.Error("context mrkdwn must not contain raw <!everyone>")
		}
		if !strings.Contains(ctxText, "&lt;!everyone&gt;") {
			t.Errorf("source should be escaped in context, got: %s", ctxText)
		}
	})
}

func buildInjectionTestBlocks(t *testing.T) []slackBlock {
	t.Helper()
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":   "<!channel> alert",
			"content": "Use <!here> for <http://evil.com|click me> injection & more",
			"source":  "<!everyone>",
			"enriched": map[string]any{
				"severity": "<high>",
				"category": "A & B",
			},
		},
	}
	return buildEventBlocks(env)
}

func findContextText(t *testing.T, blocks []slackBlock) string {
	t.Helper()
	for _, b := range blocks {
		if b.Type == "context" && len(b.Elements) > 0 {
			return b.Elements[0].Text
		}
	}
	t.Fatal("no context block found")
	return ""
}

// TestBuildEventBlocks_OutboxEnvelopeShape feeds the exact JSON shape that
// buildOutboxEnvelope produces (title/is_urgent nested in feedback.enriched,
// severity/category in enriched.attrs) through buildEventBlocks and asserts
// every field lands in the rendered Block Kit message.
func TestBuildEventBlocks_OutboxEnvelopeShape(t *testing.T) {
	t.Parallel()
	env := &outbound.Envelope{
		Version:   "2",
		EventType: "feedback.enriched",
		Timestamp: "2026-06-20T12:00:00Z",
		TenantID:  "tenant-x",
		Feedback: map[string]any{
			"id":        float64(42),
			"tenant_id": "tenant-x",
			"content":   "Payment failed on checkout",
			"source":    "api",
			"enriched": map[string]any{
				"title":     "Checkout Payment Failure",
				"is_urgent": true,
				"attrs": map[string]any{
					"severity": "high",
					"category": "payments",
				},
			},
		},
	}
	blocks := buildEventBlocks(env)

	hdr := blocks[0]
	if !strings.Contains(hdr.Text.Text, "[Urgent]") {
		t.Errorf("header should contain [Urgent], got %q", hdr.Text.Text)
	}
	if !strings.Contains(hdr.Text.Text, "Checkout Payment Failure") {
		t.Errorf("header should contain title from enriched, got %q", hdr.Text.Text)
	}

	content := blocks[1]
	if !strings.Contains(content.Text.Text, "Payment failed") {
		t.Errorf("content section missing, got %q", content.Text.Text)
	}

	enrichSec := blocks[2]
	if !strings.Contains(enrichSec.Text.Text, "high") {
		t.Errorf("severity from enriched.attrs missing, got %q", enrichSec.Text.Text)
	}
	if !strings.Contains(enrichSec.Text.Text, "payments") {
		t.Errorf("category from enriched.attrs missing, got %q", enrichSec.Text.Text)
	}

	ctxText := findContextText(t, blocks)
	if !strings.Contains(ctxText, "api") {
		t.Errorf("context should contain source, got %q", ctxText)
	}
	if !strings.Contains(ctxText, "2026-06-20T12:00:00Z") {
		t.Errorf("context should contain timestamp, got %q", ctxText)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf, nil
}
