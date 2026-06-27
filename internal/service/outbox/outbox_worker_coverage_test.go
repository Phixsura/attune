// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package outbox

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/outbound"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
)

// ---------------------------------------------------------------------------
// outboxBackoff — edge cases beyond the existing table
// ---------------------------------------------------------------------------

func TestCoverage_OutboxBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{"attempt 0 defaults to first", 0, 30 * time.Second},
		{"attempt 1 is first step", 1, 30 * time.Second},
		{"attempt 2 is second step", 2, 2 * time.Minute},
		{"attempt 3 is third step", 3, 10 * time.Minute},
		{"attempt 4 is fourth step", 4, 1 * time.Hour},
		{"attempt 5 caps at last", 5, 1 * time.Hour},
		{"attempt 1000 caps at last", 1000, 1 * time.Hour},
		{"negative attempt defaults to first", -1, 30 * time.Second},
		{"large negative defaults to first", -999, 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := outboxBackoff(tt.attempt)
			if got != tt.want {
				t.Errorf("outboxBackoff(%d) = %v, want %v", tt.attempt, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// truncateStr — multi-byte, boundary, and tiny-n edge cases
// ---------------------------------------------------------------------------

func TestCoverage_TruncateStr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    string
		n    int
		want string
	}{
		{"emoji truncation", "\U0001F600\U0001F601\U0001F602", 4, "\U0001F600"},
		{"exactly at boundary minus 1", "abcde", 4, "abcd"},
		{"n equals 1", "hello", 1, "h"},
		{"n larger than string length", "hi", 100, "hi"},
		{"n equals string length", "exact", 5, "exact"},
		{"single byte string n=1", "x", 1, "x"},
		{"empty string n=0", "", 0, ""},
		{"multi-byte mid-rune cut", "\U0001F600", 2, "\xf0\x9f"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncateStr(tt.s, tt.n)
			if got != tt.want {
				t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// unmarshalEnvelope — additional edge cases
// ---------------------------------------------------------------------------

func TestCoverage_UnmarshalEnvelope(t *testing.T) {
	t.Parallel()

	t.Run("invalid JSON returns error", func(t *testing.T) {
		t.Parallel()
		_, err := unmarshalEnvelope([]byte(`{not json`))
		if err == nil {
			t.Fatal("expected error for invalid JSON, got nil")
		}
	})

	t.Run("empty JSON object succeeds with zero fields", func(t *testing.T) {
		t.Parallel()
		env, err := unmarshalEnvelope([]byte(`{}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if env.Version != "" {
			t.Errorf("Version = %q, want empty", env.Version)
		}
		if env.Timestamp != "" {
			t.Errorf("Timestamp = %q, want empty", env.Timestamp)
		}
		if env.TenantID != "" {
			t.Errorf("TenantID = %q, want empty", env.TenantID)
		}
	})

	t.Run("timestamp field used directly", func(t *testing.T) {
		t.Parallel()
		payload, _ := json.Marshal(map[string]any{
			"timestamp": "2026-01-01T00:00:00Z",
		})
		env, err := unmarshalEnvelope(payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if env.Timestamp != "2026-01-01T00:00:00Z" {
			t.Errorf("Timestamp = %q, want %q", env.Timestamp, "2026-01-01T00:00:00Z")
		}
	})

	t.Run("tenant_id at top level used directly", func(t *testing.T) {
		t.Parallel()
		payload, _ := json.Marshal(map[string]any{
			"tenant_id": "top-tenant",
		})
		env, err := unmarshalEnvelope(payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if env.TenantID != "top-tenant" {
			t.Errorf("TenantID = %q, want %q", env.TenantID, "top-tenant")
		}
	})

	t.Run("nested feedback.tenant_id extracted when top-level missing", func(t *testing.T) {
		t.Parallel()
		payload, _ := json.Marshal(map[string]any{
			"feedback": map[string]any{
				"tenant_id": "nested-tenant",
			},
		})
		env, err := unmarshalEnvelope(payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if env.TenantID != "nested-tenant" {
			t.Errorf("TenantID = %q, want %q", env.TenantID, "nested-tenant")
		}
	})

	t.Run("top-level tenant_id takes precedence over nested", func(t *testing.T) {
		t.Parallel()
		payload, _ := json.Marshal(map[string]any{
			"tenant_id": "top-wins",
			"feedback": map[string]any{
				"tenant_id": "nested-loses",
			},
		})
		env, err := unmarshalEnvelope(payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if env.TenantID != "top-wins" {
			t.Errorf("TenantID = %q, want %q", env.TenantID, "top-wins")
		}
	})

	t.Run("delivered_at falls back to timestamp", func(t *testing.T) {
		t.Parallel()
		payload, _ := json.Marshal(map[string]any{
			"delivered_at": "2026-06-20T12:00:00Z",
		})
		env, err := unmarshalEnvelope(payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if env.Timestamp != "2026-06-20T12:00:00Z" {
			t.Errorf("Timestamp = %q, want delivered_at value %q",
				env.Timestamp, "2026-06-20T12:00:00Z")
		}
	})

	t.Run("timestamp takes precedence over delivered_at", func(t *testing.T) {
		t.Parallel()
		payload, _ := json.Marshal(map[string]any{
			"timestamp":    "2026-01-01T00:00:00Z",
			"delivered_at": "2026-06-20T12:00:00Z",
		})
		env, err := unmarshalEnvelope(payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if env.Timestamp != "2026-01-01T00:00:00Z" {
			t.Errorf("Timestamp = %q, want timestamp value %q",
				env.Timestamp, "2026-01-01T00:00:00Z")
		}
	})

	t.Run("minimal valid envelope", func(t *testing.T) {
		t.Parallel()
		payload, _ := json.Marshal(map[string]any{
			"version":    "2",
			"event_type": "feedback.enriched",
			"timestamp":  "2026-01-01T00:00:00Z",
			"tenant_id":  "t1",
			"feedback":   map[string]any{},
		})
		env, err := unmarshalEnvelope(payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if env.Version != "2" {
			t.Errorf("Version = %q, want %q", env.Version, "2")
		}
		if env.EventType != "feedback.enriched" {
			t.Errorf("EventType = %q, want %q", env.EventType, "feedback.enriched")
		}
		if env.Timestamp != "2026-01-01T00:00:00Z" {
			t.Errorf("Timestamp = %q, want %q", env.Timestamp, "2026-01-01T00:00:00Z")
		}
		if env.TenantID != "t1" {
			t.Errorf("TenantID = %q, want %q", env.TenantID, "t1")
		}
	})

	t.Run("feedback with non-string tenant_id is ignored", func(t *testing.T) {
		t.Parallel()
		payload, _ := json.Marshal(map[string]any{
			"feedback": map[string]any{
				"tenant_id": 12345,
			},
		})
		env, err := unmarshalEnvelope(payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if env.TenantID != "" {
			t.Errorf("TenantID = %q, want empty (non-string nested tenant_id)", env.TenantID)
		}
	})
}

// ---------------------------------------------------------------------------
// toOutboundTarget
// ---------------------------------------------------------------------------

func TestCoverage_ToOutboundTarget(t *testing.T) {
	t.Parallel()

	t.Run("all fields set", func(t *testing.T) {
		t.Parallel()
		id := uuid.New()
		nt := ptrext.Of(notifytarget.NotifyTarget{
			ID:               id,
			TenantID:         "t1",
			DestinationType:  "raw_webhook",
			URL:              "https://example.com/hook",
			Secret:           "s3cr3t-long-enough",
			SignatureVersion: outbound.SignatureVersionBytes,
		})

		got := toOutboundTarget(nt)

		if got.ID != id.String() {
			t.Errorf("ID = %q, want %q", got.ID, id.String())
		}
		if got.TenantID != "t1" {
			t.Errorf("TenantID = %q, want %q", got.TenantID, "t1")
		}
		if got.URL != "https://example.com/hook" {
			t.Errorf("URL = %q, want %q", got.URL, "https://example.com/hook")
		}
		if got.Secret != "s3cr3t-long-enough" {
			t.Errorf("Secret = %q, want %q", got.Secret, "s3cr3t-long-enough")
		}
		if got.SignatureVersion != outbound.SignatureVersionBytes {
			t.Errorf("SignatureVersion = %q, want %q", got.SignatureVersion, outbound.SignatureVersionBytes)
		}
		if got.DestinationType != "raw_webhook" {
			t.Errorf("DestinationType = %q, want %q", got.DestinationType, "raw_webhook")
		}
	})

	t.Run("empty signature version defaults to content hash", func(t *testing.T) {
		t.Parallel()
		nt := ptrext.Of(notifytarget.NotifyTarget{
			ID:               uuid.New(),
			TenantID:         "t2",
			URL:              "https://example.com/hook",
			Secret:           "another-secret-val",
			SignatureVersion: "",
		})

		got := toOutboundTarget(nt)

		if got.SignatureVersion != outbound.SignatureVersionContentHash {
			t.Errorf("SignatureVersion = %q, want default %q",
				got.SignatureVersion, outbound.SignatureVersionContentHash)
		}
	})

	t.Run("custom signature version preserved", func(t *testing.T) {
		t.Parallel()
		nt := ptrext.Of(notifytarget.NotifyTarget{
			ID:               uuid.New(),
			TenantID:         "t3",
			URL:              "https://example.com/hook",
			Secret:           "custom-secret-value",
			SignatureVersion: "v3-custom",
		})

		got := toOutboundTarget(nt)

		if got.SignatureVersion != "v3-custom" {
			t.Errorf("SignatureVersion = %q, want %q", got.SignatureVersion, "v3-custom")
		}
	})
}

// ---------------------------------------------------------------------------
// Configure — zero/negative after custom values
// ---------------------------------------------------------------------------

func TestCoverage_Configure(t *testing.T) {
	t.Parallel()

	t.Run("zero values after custom preserve customs", func(t *testing.T) {
		t.Parallel()
		w := NewOutboxWorker(nil, nil, nil)
		w.Configure(20*time.Second, 50, 8)

		w.Configure(0, 0, 0)

		if w.pollInterval != 20*time.Second {
			t.Errorf("pollInterval = %v, want 20s", w.pollInterval)
		}
		if w.batchSize != 50 {
			t.Errorf("batchSize = %d, want 50", w.batchSize)
		}
		if w.maxAttempts != 8 {
			t.Errorf("maxAttempts = %d, want 8", w.maxAttempts)
		}
	})

	t.Run("negative values after custom preserve customs", func(t *testing.T) {
		t.Parallel()
		w := NewOutboxWorker(nil, nil, nil)
		w.Configure(20*time.Second, 50, 8)

		w.Configure(-5*time.Second, -10, -3)

		if w.pollInterval != 20*time.Second {
			t.Errorf("pollInterval = %v, want 20s", w.pollInterval)
		}
		if w.batchSize != 50 {
			t.Errorf("batchSize = %d, want 50", w.batchSize)
		}
		if w.maxAttempts != 8 {
			t.Errorf("maxAttempts = %d, want 8", w.maxAttempts)
		}
	})
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

func TestCoverage_Constants(t *testing.T) {
	t.Parallel()

	t.Run("drainTimeout is 30s", func(t *testing.T) {
		t.Parallel()
		if drainTimeout != 30*time.Second {
			t.Errorf("drainTimeout = %v, want 30s", drainTimeout)
		}
	})

	t.Run("claimHeartbeatInterval is 3m", func(t *testing.T) {
		t.Parallel()
		if claimHeartbeatInterval != 3*time.Minute {
			t.Errorf("claimHeartbeatInterval = %v, want 3m", claimHeartbeatInterval)
		}
	})
}

// ---------------------------------------------------------------------------
// NewOutboxWorker — nil dependencies and defaults
// ---------------------------------------------------------------------------

func TestCoverage_NewOutboxWorker(t *testing.T) {
	t.Parallel()

	t.Run("nil dependencies produce non-nil worker", func(t *testing.T) {
		t.Parallel()
		w := NewOutboxWorker(nil, nil, nil)
		if w == nil {
			t.Fatal("NewOutboxWorker returned nil")
		}
		if w.outbox != nil {
			t.Error("outbox should be nil when constructed with nil")
		}
		if w.targets != nil {
			t.Error("targets should be nil when constructed with nil")
		}
		if w.transport != nil {
			t.Error("transport should be nil when constructed with nil")
		}
	})

	t.Run("defaults match design doc", func(t *testing.T) {
		t.Parallel()
		w := NewOutboxWorker(nil, nil, nil)
		if w.pollInterval != 5*time.Second {
			t.Errorf("pollInterval = %v, want 5s", w.pollInterval)
		}
		if w.batchSize != 10 {
			t.Errorf("batchSize = %d, want 10", w.batchSize)
		}
		if w.maxAttempts != 5 {
			t.Errorf("maxAttempts = %d, want 5", w.maxAttempts)
		}
	})

	t.Run("owner has correct prefix", func(t *testing.T) {
		t.Parallel()
		w := NewOutboxWorker(nil, nil, nil)
		if !strings.HasPrefix(w.owner, "outbox-") {
			t.Errorf("owner = %q, want prefix 'outbox-'", w.owner)
		}
		// Verify it contains a valid UUID after the prefix.
		uuidPart := strings.TrimPrefix(w.owner, "outbox-")
		if _, err := uuid.Parse(uuidPart); err != nil {
			t.Errorf("owner UUID part %q is not a valid UUID: %v", uuidPart, err)
		}
	})

	t.Run("drain is non-nil", func(t *testing.T) {
		t.Parallel()
		w := NewOutboxWorker(nil, nil, nil)
		if w.drain == nil {
			t.Error("drain should be non-nil")
		}
	})
}
