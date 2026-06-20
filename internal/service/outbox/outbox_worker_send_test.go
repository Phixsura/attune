package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/outbound"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
)

func TestWrapCheck_TranslatesOutboundTerminalToNotifyTerminal(t *testing.T) {
	t.Parallel()
	adapterCheck := func(_ context.Context, status int, _ []byte) error {
		return fmt.Errorf("%w: slack status=403 body=invalid_token", outbound.ErrTerminal)
	}
	row := outboxrepo.OutboxRow{ID: 42, TenantID: "t1", TraceID: "trace-1"}
	wrapped := wrapCheck(adapterCheck, row)

	err := wrapped(context.Background(), 403, []byte("invalid_token"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, notify.ErrTerminal) {
		t.Errorf("error should wrap notify.ErrTerminal, got: %v", err)
	}
}

func TestWrapCheck_NonTerminalPassesThrough(t *testing.T) {
	t.Parallel()
	adapterCheck := func(_ context.Context, _ int, _ []byte) error {
		return fmt.Errorf("slack retryable status=429")
	}
	row := outboxrepo.OutboxRow{ID: 43, TenantID: "t1"}
	wrapped := wrapCheck(adapterCheck, row)

	err := wrapped(context.Background(), 429, []byte("rate limited"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, notify.ErrTerminal) {
		t.Error("retryable error should not wrap notify.ErrTerminal")
	}
	if errors.Is(err, outbound.ErrTerminal) {
		t.Error("retryable error should not wrap outbound.ErrTerminal")
	}
}

func TestWrapCheck_SuccessReturnsNil(t *testing.T) {
	t.Parallel()
	adapterCheck := func(_ context.Context, _ int, _ []byte) error { return nil }
	row := outboxrepo.OutboxRow{ID: 44, TenantID: "t1", TraceID: "trace-2"}
	wrapped := wrapCheck(adapterCheck, row)

	if err := wrapped(context.Background(), 200, []byte("ok")); err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}

// TestUnmarshalEnvelope_OutboxShape proves that the real outbox JSON
// (produced by enricher_outbox.go buildOutboxEnvelope) round-trips
// correctly through unmarshalEnvelope — timestamp, tenant_id, and
// nested enriched fields all land where the adapter expects them.
func TestUnmarshalEnvelope_OutboxShape(t *testing.T) {
	t.Parallel()

	// Exact shape from enricher_outbox.go:388-413.
	outboxJSON, _ := json.Marshal(map[string]any{
		"version":      "2",
		"event_type":   "feedback.enriched",
		"delivered_at": "2026-06-20T12:00:00Z",
		"trace_id":     "abc-123",
		"feedback": map[string]any{
			"id":           42,
			"tenant_id":    "tenant-x",
			"content":      "Payment failed on checkout",
			"source":       "api",
			"user_id":      "u-1",
			"submitted_at": "2026-06-20T11:55:00Z",
			"enriched": map[string]any{
				"title":       "Checkout Payment Failure",
				"is_urgent":   true,
				"rationale":   "Payment flow broken",
				"enriched_at": "2026-06-20T12:00:00Z",
				"attrs": map[string]any{
					"severity": "high",
					"category": "payments",
				},
			},
		},
	})

	env, err := unmarshalEnvelope(outboxJSON)
	if err != nil {
		t.Fatalf("unmarshalEnvelope: %v", err)
	}

	if env.Version != "2" {
		t.Errorf("Version = %q, want %q", env.Version, "2")
	}
	if env.Timestamp != "2026-06-20T12:00:00Z" {
		t.Errorf("Timestamp = %q, want delivered_at value", env.Timestamp)
	}
	if env.TenantID != "tenant-x" {
		t.Errorf("TenantID = %q, want %q", env.TenantID, "tenant-x")
	}

	// Verify the adapter can read nested fields the same way buildEventBlocks does.
	fb := env.Feedback
	enriched, _ := fb["enriched"].(map[string]any)
	if enriched == nil {
		t.Fatal("feedback.enriched is nil")
	}

	title, _ := enriched["title"].(string)
	if title != "Checkout Payment Failure" {
		t.Errorf("enriched.title = %q, want %q", title, "Checkout Payment Failure")
	}
	isUrgent, _ := enriched["is_urgent"].(bool)
	if !isUrgent {
		t.Error("enriched.is_urgent should be true")
	}

	attrs, _ := enriched["attrs"].(map[string]any)
	if attrs == nil {
		t.Fatal("enriched.attrs is nil")
	}
	if sev, _ := attrs["severity"].(string); sev != "high" {
		t.Errorf("attrs.severity = %q, want %q", sev, "high")
	}
	if cat, _ := attrs["category"].(string); cat != "payments" {
		t.Errorf("attrs.category = %q, want %q", cat, "payments")
	}
}
