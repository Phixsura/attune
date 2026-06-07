// Package trace owns the request-scoped trace_id contract for attune.
//
// chi's middleware.RequestID populates request_id on every HTTP
// request. This package lifts that into a typed ctx value so non-HTTP
// code (enricher running on a poller, outbox worker, etc.) reads the
// same key without importing chi.
//
// The trace_id flows in three directions:
//
// 1. inbound HTTP request → chi RequestID → ctx
// 2. ctx → slog fields (every log line gets "trace_id=...")
// 3. ctx → outbox row.TraceID → envelope.trace_id + X-Trace-ID header
// → customer's downstream system
//
// On a background poll (no incoming request), trace_id is generated
// fresh per row so observability stays unbroken.
package trace

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/go-chi/chi/v5/middleware"
)

type ctxKey int

const (
	traceIDKey ctxKey = iota
)

// FromContext returns the trace_id stamped onto ctx, falling back to
// chi's RequestID middleware value, or "" when neither is set.
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(traceIDKey).(string); ok && v != "" {
		return v
	}
	if id := middleware.GetReqID(ctx); id != "" {
		return id
	}
	return ""
}

// WithID returns a derived ctx carrying traceID. Used by the ingestor's
// fireEnrich to thread the HTTP trace_id into the async goroutine's
// fresh background ctx.
func WithID(ctx context.Context, traceID string) context.Context {
	if traceID == "" {
		return ctx
	}
	return context.WithValue(ctx, traceIDKey, traceID)
}

// New generates a fresh trace_id for background work that has no
// inbound HTTP request to inherit from (e.g. enricher poller catching
// up on stale rows). 16 random bytes hex-encoded = 32-char string,
// compatible with chi's default RequestID format.
func New() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// Crypto/rand is documented to never fail on Linux/macOS in
		// practice; on the impossible failure we fall back to an
		// empty trace rather than crashing.
		return ""
	}
	return hex.EncodeToString(buf)
}
