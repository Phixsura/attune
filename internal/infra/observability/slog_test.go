// slog_test.go — verifies the bootstrap (BuildDefaultHandler) + the
// TraceIDHandler decorator preserve trace_id/span_id from the active OTel
// span on ctx. Acceptance #3 of issue #48: facade migration must not drop
// the handler-injected trace fields.
package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace"
)

// withTestSpan returns a ctx carrying a recording span produced by the
// stdlib tracer provider, so the SpanContext is valid (TraceIDHandler skips
// invalid contexts). The provider has no exporter — nothing ships.
func withTestSpan(t *testing.T) context.Context {
	t.Helper()
	tp := trace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("attune-test").Start(context.Background(), "t")
	t.Cleanup(func() { span.End() })
	return ctx
}

func TestBuildDefaultHandlerInjectsTraceID(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	logger := slog.New(BuildDefaultHandler(buf))
	ctx := withTestSpan(t)

	logger.InfoContext(ctx, "hello")

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("expected a log line, got empty buffer")
	}
	// Default output is JSON; trace_id/span_id are top-level string fields.
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("expected JSON output, parse failed: %v · line=%q", err, line)
	}
	for _, k := range []string{"trace_id", "span_id"} {
		v, ok := rec[k].(string)
		if !ok || v == "" {
			t.Errorf("expected %q present and non-empty in record, got %v", k, rec[k])
		}
	}
}

func TestBuildDefaultHandlerNoSpanOmitsTraceID(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	logger := slog.New(BuildDefaultHandler(buf))

	logger.InfoContext(context.Background(), "hello")

	line := strings.TrimSpace(buf.String())
	var rec map[string]any
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		t.Fatalf("expected JSON output, parse failed: %v · line=%q", err, line)
	}
	if _, ok := rec["trace_id"]; ok {
		t.Errorf("trace_id should be absent without an active span, got %v", rec["trace_id"])
	}
}
