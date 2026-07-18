// ptrext:file-allow test fixtures construct adapter targets inline.
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// TestSlackAdapterWiring_E2E verifies the full in-process chain:
//
//	adapter init() registration → outbound.LookupEvent("slack") →
//	notify.TestSend → Slack adapter renders Block Kit → HTTP POST →
//	mock server receives valid JSON with blocks.
//
// This test runs inside cmd/attune (package main), so all adapters
// registered via init() in main.go's blank-imports are live — exactly
// as they would be in the production binary.
func TestSlackAdapterWiring_E2E(t *testing.T) {
	t.Parallel()
	allowLoopbackEgressForTest(t)

	// Verify all expected adapters are registered.
	t.Run("registry_has_all_adapters", func(t *testing.T) {
		want := map[string]bool{
			"raw-webhook":  true,
			"slack":        true,
			"lark":         true,
			"discord":      true,
			"email":        true,
			"github-issue": true,
		}
		channels := outbound.Channels()
		got := map[string]bool{}
		for _, ch := range channels {
			got[ch.ID] = true
		}
		for id := range want {
			if !got[id] {
				t.Errorf("adapter %q not registered — blank-import missing in main.go?", id)
			}
		}
	})

	// Verify LookupEvent returns non-nil for slack.
	t.Run("lookup_slack", func(t *testing.T) {
		ch := outbound.LookupEvent("slack")
		if ch == nil {
			t.Fatal("outbound.LookupEvent(\"slack\") returned nil")
		}
		if ch.ID() != "slack" {
			t.Errorf("ID() = %q, want \"slack\"", ch.ID())
		}
	})

	// Full TestSend: notify.TestSend → Slack adapter → mock HTTP server.
	t.Run("test_send_slack_delivers_block_kit", func(t *testing.T) {
		var received []byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", r.Method)
			}
			ct := r.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			received, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		t.Cleanup(srv.Close)

		target := notifytarget.NotifyTarget{
			ID:              uuid.New(),
			TenantID:        "e2e-tenant",
			DestinationType: notifytarget.DestSlack,
			Audience:        notifytarget.AudienceAll,
			URL:             srv.URL,
			TimeoutSeconds:  5,
		}

		result := notify.TestSend(t.Context(), target)
		if !result.OK {
			t.Fatalf("TestSend failed: ok=%v status=%d err=%v",
				result.OK, result.StatusCode, result.Err)
		}
		if result.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", result.StatusCode)
		}
		// Latency can legitimately be 0ms on a fast loopback round-trip; only a
		// negative value would indicate a measurement bug.
		if result.LatencyMs < 0 {
			t.Errorf("latency_ms = %d, want >= 0", result.LatencyMs)
		}

		// Verify the payload is valid Slack Block Kit JSON.
		var msg struct {
			Blocks []struct {
				Type string `json:"type"`
				Text *struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"text,omitempty"`
				Elements []json.RawMessage `json:"elements,omitempty"`
			} `json:"blocks"`
		}
		if err := json.Unmarshal(received, &msg); err != nil {
			t.Fatalf("unmarshal received payload: %v\nbody: %s", err, received)
		}
		if len(msg.Blocks) < 3 {
			t.Fatalf("expected at least 3 blocks (header, section, context), got %d", len(msg.Blocks))
		}

		// First block: header with test notification title.
		hdr := msg.Blocks[0]
		if hdr.Type != "header" {
			t.Errorf("blocks[0].type = %q, want header", hdr.Type)
		}
		if hdr.Text == nil || !strings.Contains(hdr.Text.Text, "Test Notification") {
			t.Errorf("header text should contain 'Test Notification', got %v", hdr.Text)
		}

		// Body block: section with connectivity test content.
		body := msg.Blocks[1]
		if body.Type != "section" {
			t.Errorf("blocks[1].type = %q, want section", body.Type)
		}
		if body.Text == nil || !strings.Contains(body.Text.Text, "Connectivity test") {
			t.Errorf("section text should mention connectivity, got %v", body.Text)
		}

		// Must have a context block somewhere (source + timestamp).
		hasContext := false
		for _, b := range msg.Blocks {
			if b.Type == "context" {
				hasContext = true
				break
			}
		}
		if !hasContext {
			t.Error("expected a context block with source/timestamp metadata")
		}

		t.Logf("Slack Block Kit payload (%d bytes, %d blocks) delivered successfully",
			len(received), len(msg.Blocks))
	})

	// Raw-webhook TestSend still works (regression guard).
	t.Run("test_send_raw_webhook_regression", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(srv.Close)

		target := notifytarget.NotifyTarget{
			ID:              uuid.New(),
			TenantID:        "e2e-tenant",
			DestinationType: notifytarget.DestRawWebhook,
			Audience:        notifytarget.AudienceAll,
			URL:             srv.URL,
			Secret:          "test-secret-at-least-16",
			TimeoutSeconds:  5,
		}

		result := notify.TestSend(t.Context(), target)
		if !result.OK {
			t.Fatalf("raw-webhook TestSend failed: ok=%v status=%d err=%v",
				result.OK, result.StatusCode, result.Err)
		}
	})

	// Slack 429 is retryable, not terminal.
	t.Run("test_send_slack_429_is_retryable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
		}))
		t.Cleanup(srv.Close)

		target := notifytarget.NotifyTarget{
			ID:              uuid.New(),
			TenantID:        "e2e-tenant",
			DestinationType: notifytarget.DestSlack,
			URL:             srv.URL,
			TimeoutSeconds:  5,
		}

		result := notify.TestSend(t.Context(), target)
		if result.OK {
			t.Fatal("429 should not be OK")
		}
		if result.StatusCode != http.StatusTooManyRequests {
			t.Errorf("status = %d, want 429", result.StatusCode)
		}
		if result.Err == nil || !strings.Contains(result.Err.Error(), "retryable") {
			t.Errorf("error should mention retryable: %v", result.Err)
		}
	})

	// Slack 403 is terminal (bad webhook URL).
	t.Run("test_send_slack_403_is_terminal", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("invalid_token"))
		}))
		t.Cleanup(srv.Close)

		target := notifytarget.NotifyTarget{
			ID:              uuid.New(),
			TenantID:        "e2e-tenant",
			DestinationType: notifytarget.DestSlack,
			URL:             srv.URL,
			TimeoutSeconds:  5,
		}

		result := notify.TestSend(t.Context(), target)
		if result.OK {
			t.Fatal("403 should not be OK")
		}
		if result.StatusCode != http.StatusForbidden {
			t.Errorf("status = %d, want 403", result.StatusCode)
		}
	})

	// Lark adapter is also registered and functional.
	t.Run("test_send_lark_delivers", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"StatusCode":0,"StatusMessage":"success"}`))
		}))
		t.Cleanup(srv.Close)

		target := notifytarget.NotifyTarget{
			ID:              uuid.New(),
			TenantID:        "e2e-tenant",
			DestinationType: notifytarget.DestLark,
			Audience:        notifytarget.AudienceAll,
			URL:             srv.URL,
			TimeoutSeconds:  5,
		}

		result := notify.TestSend(t.Context(), target)
		if !result.OK {
			t.Fatalf("lark TestSend failed: ok=%v status=%d err=%v",
				result.OK, result.StatusCode, result.Err)
		}
	})

	// Discord delivers embeds and treats 204 No Content as success.
	t.Run("test_send_discord_delivers_embeds", func(t *testing.T) {
		var received []byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			received, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(srv.Close)

		target := notifytarget.NotifyTarget{
			ID:              uuid.New(),
			TenantID:        "e2e-tenant",
			DestinationType: notifytarget.DestDiscord,
			Audience:        notifytarget.AudienceAll,
			URL:             srv.URL,
			TimeoutSeconds:  5,
		}

		result := notify.TestSend(t.Context(), target)
		if !result.OK {
			t.Fatalf("discord TestSend failed: ok=%v status=%d err=%v",
				result.OK, result.StatusCode, result.Err)
		}

		var msg struct {
			Embeds          []map[string]any `json:"embeds"`
			AllowedMentions struct {
				Parse []string `json:"parse"`
			} `json:"allowed_mentions"`
		}
		if err := json.Unmarshal(received, &msg); err != nil {
			t.Fatalf("unmarshal received discord body: %v", err)
		}
		if len(msg.Embeds) == 0 {
			t.Errorf("expected at least one embed in discord payload")
		}
		if msg.AllowedMentions.Parse == nil || len(msg.AllowedMentions.Parse) != 0 {
			t.Errorf("allowed_mentions.parse must be present and empty, got %v", msg.AllowedMentions.Parse)
		}
	})
}
