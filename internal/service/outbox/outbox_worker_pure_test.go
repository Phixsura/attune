// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package outbox

import (
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// unmarshalEnvelope — edge cases beyond TestUnmarshalEnvelope_OutboxShape
// ---------------------------------------------------------------------------

func TestUnmarshalEnvelope_NativeTimestamp(t *testing.T) {
	t.Parallel()
	// When the payload already has "timestamp" set, it should not overwrite
	// from "delivered_at".
	payload, _ := json.Marshal(map[string]any{
		"version":    "2",
		"event_type": "feedback.enriched",
		"timestamp":  "2026-06-20T10:00:00Z",
		"tenant_id":  "t-1",
		"feedback":   map[string]any{"id": 1},
	})
	env, err := unmarshalEnvelope(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Timestamp != "2026-06-20T10:00:00Z" {
		t.Errorf("Timestamp = %q, want native value", env.Timestamp)
	}
}

func TestUnmarshalEnvelope_FallbackDeliveredAt(t *testing.T) {
	t.Parallel()
	// When "timestamp" is absent but "delivered_at" exists, the fixup should
	// copy it into Timestamp.
	payload, _ := json.Marshal(map[string]any{
		"version":      "2",
		"event_type":   "feedback.enriched",
		"delivered_at": "2026-06-20T11:00:00Z",
		"tenant_id":    "t-2",
		"feedback":     map[string]any{"id": 2},
	})
	env, err := unmarshalEnvelope(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Timestamp != "2026-06-20T11:00:00Z" {
		t.Errorf("Timestamp = %q, want delivered_at fallback", env.Timestamp)
	}
}

func TestUnmarshalEnvelope_TenantIDFromFeedback(t *testing.T) {
	t.Parallel()
	// When top-level tenant_id is absent, it should be extracted from
	// feedback.tenant_id.
	payload, _ := json.Marshal(map[string]any{
		"version":    "2",
		"event_type": "feedback.enriched",
		"timestamp":  "2026-06-20T10:00:00Z",
		"feedback":   map[string]any{"id": 3, "tenant_id": "t-nested"},
	})
	env, err := unmarshalEnvelope(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.TenantID != "t-nested" {
		t.Errorf("TenantID = %q, want %q", env.TenantID, "t-nested")
	}
}

func TestUnmarshalEnvelope_TopLevelTenantIDTakesPrecedence(t *testing.T) {
	t.Parallel()
	// When both top-level and nested tenant_id exist, top-level wins
	// (the fixup only runs when TenantID == "").
	payload, _ := json.Marshal(map[string]any{
		"version":    "2",
		"event_type": "feedback.enriched",
		"timestamp":  "2026-06-20T10:00:00Z",
		"tenant_id":  "t-top",
		"feedback":   map[string]any{"id": 4, "tenant_id": "t-nested"},
	})
	env, err := unmarshalEnvelope(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.TenantID != "t-top" {
		t.Errorf("TenantID = %q, want top-level %q", env.TenantID, "t-top")
	}
}
