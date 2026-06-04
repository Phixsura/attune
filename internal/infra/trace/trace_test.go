package trace

import (
	"context"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestFromContext_Empty(t *testing.T) {
	if id := FromContext(context.Background()); id != "" {
		t.Fatalf("empty ctx must yield empty trace_id, got %q", id)
	}
}

func TestFromContext_ExplicitWithID(t *testing.T) {
	ctx := WithID(context.Background(), "abc123")
	if id := FromContext(ctx); id != "abc123" {
		t.Fatalf("WithID round-trip: want abc123, got %q", id)
	}
}

func TestFromContext_FallsBackToChiRequestID(t *testing.T) {
	// chi middleware.GetReqID reads its own context key. Simulate by
	// setting the same key chi uses.
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "chi-id-xyz")
	if id := FromContext(ctx); id != "chi-id-xyz" {
		t.Fatalf("chi fallback: want chi-id-xyz, got %q", id)
	}
}

func TestFromContext_ExplicitBeatsChi(t *testing.T) {
	// When both are set, the explicit WithID value should win — it
	// reflects the "service set this on purpose" intent.
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "chi-id")
	ctx = WithID(ctx, "explicit-id")
	if id := FromContext(ctx); id != "explicit-id" {
		t.Fatalf("explicit beats chi: want explicit-id, got %q", id)
	}
}

func TestWithID_EmptyStringIsNoOp(t *testing.T) {
	// WithID("") should not mask a parent chi value with empty.
	ctx := context.WithValue(context.Background(), middleware.RequestIDKey, "chi-id")
	ctx = WithID(ctx, "")
	if id := FromContext(ctx); id != "chi-id" {
		t.Fatalf("empty WithID should be no-op, got %q", id)
	}
}

func TestNew_Generates32HexChars(t *testing.T) {
	id := New()
	if len(id) != 32 {
		t.Fatalf("want 32-char id, got %d (%q)", len(id), id)
	}
	for _, r := range id {
		ok := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !ok {
			t.Fatalf("non-hex char %q in id %q", r, id)
		}
	}
}

func TestNew_Unique(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := New()
		if seen[id] {
			t.Fatalf("collision on iteration %d: %q", i, id)
		}
		seen[id] = true
	}
}
