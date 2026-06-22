// ptrext:file-allow test slog handlers use &buf for writer setup
package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestBuildDefaultHandler(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := BuildDefaultHandler(&buf)
	if h == nil {
		t.Fatal("BuildDefaultHandler returned nil")
	}

	logger := slog.New(h)
	logger.Info("test message", "key", "value")

	output := buf.String()
	if output == "" {
		t.Fatal("BuildDefaultHandler produced no output")
	}

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if record["msg"] != "test message" {
		t.Errorf("msg = %v, want 'test message'", record["msg"])
	}
	if record["key"] != "value" {
		t.Errorf("key = %v, want 'value'", record["key"])
	}
}

func TestTraceIDHandler_Handle(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	h := NewTraceIDHandler(inner)

	logger := slog.New(h)
	logger.InfoContext(context.Background(), "test without span")

	output := buf.String()
	if output == "" {
		t.Fatal("TraceIDHandler produced no output")
	}

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if record["msg"] != "test without span" {
		t.Errorf("msg = %v, want 'test without span'", record["msg"])
	}
}

func TestTraceIDHandler_WithAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	h := NewTraceIDHandler(inner)

	h2 := h.WithAttrs([]slog.Attr{slog.String("extra", "attr")})
	if h2 == nil {
		t.Fatal("WithAttrs returned nil")
	}

	logger := slog.New(h2)
	logger.Info("test with attrs")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if record["extra"] != "attr" {
		t.Errorf("extra = %v, want 'attr'", record["extra"])
	}
}

func TestTraceIDHandler_WithGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	h := NewTraceIDHandler(inner)

	h2 := h.WithGroup("mygroup")
	if h2 == nil {
		t.Fatal("WithGroup returned nil")
	}

	logger := slog.New(h2)
	logger.Info("test with group", "field", "value")

	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if record["mygroup"] == nil {
		t.Error("expected group 'mygroup' in output")
	}
}

func TestReadableIDGenerator_NewIDs(t *testing.T) {
	t.Parallel()

	gen := &ReadableIDGenerator{}
	tid, sid := gen.NewIDs(context.Background())

	if tid.IsValid() == false {
		t.Error("NewIDs returned invalid TraceID")
	}
	if sid.IsValid() == false {
		t.Error("NewIDs returned invalid SpanID")
	}

	tidStr := tid.String()
	if len(tidStr) != 32 {
		t.Errorf("TraceID string length = %d, want 32", len(tidStr))
	}
}

func TestReadableIDGenerator_NewSpanID(t *testing.T) {
	t.Parallel()

	gen := &ReadableIDGenerator{}
	var tid trace.TraceID
	sid := gen.NewSpanID(context.Background(), tid)

	if sid.IsValid() == false {
		t.Error("NewSpanID returned invalid SpanID")
	}

	sidStr := sid.String()
	if len(sidStr) != 16 {
		t.Errorf("SpanID string length = %d, want 16", len(sidStr))
	}
}

func TestReadableIDGenerator_UniqueIDs(t *testing.T) {
	t.Parallel()

	gen := &ReadableIDGenerator{}
	seen := make(map[string]bool)

	for i := 0; i < 100; i++ {
		tid, _ := gen.NewIDs(context.Background())
		tidStr := tid.String()
		if seen[tidStr] {
			t.Errorf("duplicate TraceID: %s", tidStr)
		}
		seen[tidStr] = true
	}
}

func TestInitTracer_NoopWithEmptyEndpoint(t *testing.T) {
	t.Parallel()

	shutdown, err := InitTracer(context.Background(), Options{
		ServiceName:    "test-service",
		ServiceVersion: "v1.0.0",
		Environment:    "test",
		Endpoint:       "",
	})
	if err != nil {
		t.Fatalf("InitTracer error = %v", err)
	}
	if shutdown == nil {
		t.Fatal("InitTracer returned nil shutdown func")
	}

	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown error = %v", err)
	}
}
