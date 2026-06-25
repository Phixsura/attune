// idgen.go — ReadableIDGenerator emits trace_ids whose 14-char prefix
// is a human-readable UTC timestamp.
//
// trace_id format (W3C-compatible, 32 lowercase hex chars):
//
//	20260515193045a1b2c3d40005f3e7a8
//	└──yyyyMMddHHmmss──┘└────18-char random suffix────┘
//	 timestamp (UTC, 14 hex chars = 7 bytes)
//
// Support staff can read the timestamp off a trace_id by eye, then
// pivot to the trace backend by exact id or time window.
//
// Entropy: 72 random bits in the suffix. 10M traces in a day collide
// with probability ~10^-6 — sufficient. The lexicographic order
// matches time order, so trace_id ASC in any UI is also time ASC.
//
// The FE is the real source of truth for trace_id (the browser OTel
// SDK uses an identical ReadableIdGenerator and passes the trace
// through traceparent to BE / gateway, which inherits without
// regenerating). This generator covers inbound paths without a
// traceparent — curl, cron, third-party webhooks.
//
// Sampling: currently AlwaysSample(). Switching to TraceIDRatioBased
// later would bias every trace from the same second toward the same
// sample decision; move the timestamp to the suffix if that matters.
package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"go.opentelemetry.io/otel/trace"
)

type ReadableIDGenerator struct{}

func (g *ReadableIDGenerator) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	var tid trace.TraceID
	var sid trace.SpanID

	// First 7 bytes (14 hex chars) carry yyyyMMddHHmmss as ASCII —
	// every digit 0-9 is itself a valid hex char so the timestamp
	// substring is already lowercase hex without further encoding.
	ts := time.Now().UTC().Format("20060102150405")
	tsBytes, err := hex.DecodeString(ts)
	if err == nil && len(tsBytes) == 7 {
		copy(tid[:7], tsBytes)
	}
	// trailing 9 bytes are random — crypto/rand.Read only fails on
	// catastrophic system issues (entropy pool exhaustion), so we proceed
	// with whatever we got rather than failing trace generation entirely.
	_, _ = rand.Read(tid[7:])
	_, _ = rand.Read(sid[:])
	return tid, sid
}

func (g *ReadableIDGenerator) NewSpanID(ctx context.Context, _ trace.TraceID) trace.SpanID {
	var sid trace.SpanID
	_, _ = rand.Read(sid[:])
	return sid
}
