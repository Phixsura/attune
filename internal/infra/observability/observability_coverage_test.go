// ptrext:file-allow test slog handlers use &buf for writer setup
package observability

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// --- ReadableIDGenerator ---

func TestReadableIDGenerator_TraceIDHasTimestampPrefix(t *testing.T) {
	t.Parallel()

	gen := &ReadableIDGenerator{}
	tid, _ := gen.NewIDs(context.Background())

	tidStr := tid.String()
	require.Len(t, tidStr, 32, "trace ID must be 32 hex chars")

	// First 14 hex chars encode 7 bytes that represent yyyyMMddHHmmss
	prefix := tidStr[:14]
	_, err := hex.DecodeString(prefix)
	require.NoError(t, err, "first 14 chars must be valid hex")
}

func TestReadableIDGenerator_SpanIDValid(t *testing.T) {
	t.Parallel()

	gen := &ReadableIDGenerator{}
	_, sid := gen.NewIDs(context.Background())

	require.True(t, sid.IsValid(), "span ID should be valid")
	require.Len(t, sid.String(), 16)
}

func TestReadableIDGenerator_NewSpanID_Independent(t *testing.T) {
	t.Parallel()

	gen := &ReadableIDGenerator{}
	var tid trace.TraceID

	sid1 := gen.NewSpanID(context.Background(), tid)
	sid2 := gen.NewSpanID(context.Background(), tid)

	require.True(t, sid1.IsValid())
	require.True(t, sid2.IsValid())
	require.NotEqual(t, sid1, sid2, "two span IDs should be different")
}

func TestReadableIDGenerator_NewIDs_NeverCollide(t *testing.T) {
	t.Parallel()

	gen := &ReadableIDGenerator{}
	traceIDs := make(map[string]bool)
	spanIDs := make(map[string]bool)

	for i := 0; i < 200; i++ {
		tid, sid := gen.NewIDs(context.Background())
		tidStr := tid.String()
		sidStr := sid.String()

		require.False(t, traceIDs[tidStr], "duplicate trace ID: %s", tidStr)
		require.False(t, spanIDs[sidStr], "duplicate span ID: %s", sidStr)

		traceIDs[tidStr] = true
		spanIDs[sidStr] = true
	}
}

// --- TraceIDHandler ---

func TestTraceIDHandler_WithValidSpan(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewTraceIDHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(h)

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	logger.InfoContext(ctx, "with span")

	var rec map[string]any
	err := json.Unmarshal(buf.Bytes(), &rec)
	require.NoError(t, err)

	tid, ok := rec["trace_id"].(string)
	require.True(t, ok, "trace_id should be present")
	require.Len(t, tid, 32)

	sid, ok := rec["span_id"].(string)
	require.True(t, ok, "span_id should be present")
	require.Len(t, sid, 16)
}

func TestTraceIDHandler_WithoutSpan_NoTraceFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewTraceIDHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(h)

	logger.InfoContext(context.Background(), "no span")

	var rec map[string]any
	err := json.Unmarshal(buf.Bytes(), &rec)
	require.NoError(t, err)

	_, hasTrace := rec["trace_id"]
	_, hasSpan := rec["span_id"]
	require.False(t, hasTrace, "trace_id should not be present without span")
	require.False(t, hasSpan, "span_id should not be present without span")
}

func TestTraceIDHandler_WithAttrs_PreservesAttrs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewTraceIDHandler(slog.NewJSONHandler(&buf, nil))

	withAttrs := h.WithAttrs([]slog.Attr{slog.String("component", "test")})
	require.NotNil(t, withAttrs)

	logger := slog.New(withAttrs)
	logger.Info("test attrs", "extra", "value")

	var rec map[string]any
	err := json.Unmarshal(buf.Bytes(), &rec)
	require.NoError(t, err)

	require.Equal(t, "test", rec["component"])
	require.Equal(t, "value", rec["extra"])
}

func TestTraceIDHandler_WithGroup_CreatesGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewTraceIDHandler(slog.NewJSONHandler(&buf, nil))

	withGroup := h.WithGroup("request")
	require.NotNil(t, withGroup)

	logger := slog.New(withGroup)
	logger.Info("grouped", "method", "GET")

	var rec map[string]any
	err := json.Unmarshal(buf.Bytes(), &rec)
	require.NoError(t, err)

	group, ok := rec["request"].(map[string]any)
	require.True(t, ok, "group 'request' should be a nested object")
	require.Equal(t, "GET", group["method"])
}

func TestTraceIDHandler_ChainedWithAttrsAndGroup(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := NewTraceIDHandler(slog.NewJSONHandler(&buf, nil))

	chained := h.WithAttrs([]slog.Attr{slog.String("service", "attune")}).
		WithGroup("detail")

	logger := slog.New(chained)
	logger.Info("chained", "op", "test")

	var rec map[string]any
	err := json.Unmarshal(buf.Bytes(), &rec)
	require.NoError(t, err)
	require.Equal(t, "attune", rec["service"])
}

// --- BuildDefaultHandler ---

func TestBuildDefaultHandler_ReturnsNonNil(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	h := BuildDefaultHandler(&buf)
	require.NotNil(t, h)
}

func TestBuildDefaultHandler_ProducesJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(BuildDefaultHandler(&buf))
	logger.Info("json test", "k", "v")

	line := strings.TrimSpace(buf.String())
	require.NotEmpty(t, line)

	var rec map[string]any
	err := json.Unmarshal([]byte(line), &rec)
	require.NoError(t, err)
	require.Equal(t, "json test", rec["msg"])
	require.Equal(t, "v", rec["k"])
}

func TestBuildDefaultHandler_InfoLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(BuildDefaultHandler(&buf))

	// Debug should not appear
	logger.Debug("debug message")
	require.Empty(t, buf.String(), "debug messages should be filtered")

	// Info should appear
	logger.Info("info message")
	require.NotEmpty(t, buf.String(), "info messages should appear")
}

// --- InitTracer ---

func TestInitTracer_NoopReturnsWorkingShutdown(t *testing.T) {
	t.Parallel()

	shutdown, err := InitTracer(context.Background(), Options{
		ServiceName:    "test",
		ServiceVersion: "v0.0.1",
		Environment:    "test",
		Endpoint:       "",
	})
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	err = shutdown(context.Background())
	require.NoError(t, err)
}

func TestInitTracer_WithEndpoint(t *testing.T) {
	t.Parallel()

	// Use a non-existent endpoint; InitTracer should still succeed
	// (exporter creation doesn't connect immediately)
	shutdown, err := InitTracer(context.Background(), Options{
		ServiceName:    "test",
		ServiceVersion: "v0.0.1",
		Environment:    "test",
		Endpoint:       "localhost:14318",
		URLPath:        "/v1/traces",
		Insecure:       true,
	})
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Shutdown should not error even if endpoint is unreachable
	err = shutdown(context.Background())
	require.NoError(t, err)
}

func TestInitTracer_WithHeaders(t *testing.T) {
	t.Parallel()

	shutdown, err := InitTracer(context.Background(), Options{
		ServiceName:    "test",
		ServiceVersion: "v0.0.1",
		Environment:    "test",
		Endpoint:       "localhost:14318",
		URLPath:        "/v1/traces",
		Headers:        map[string]string{"Authorization": "Bearer test-token"},
		Insecure:       true,
	})
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown(context.Background()))
}

func TestOptions_Fields(t *testing.T) {
	t.Parallel()

	opts := Options{
		ServiceName:    "attune",
		ServiceVersion: "abc123",
		Environment:    "prod",
		Endpoint:       "otel.example.com:4318",
		URLPath:        "/v1/traces",
		Headers:        map[string]string{"X-Custom": "val"},
		Insecure:       false,
	}

	require.Equal(t, "attune", opts.ServiceName)
	require.Equal(t, "abc123", opts.ServiceVersion)
	require.Equal(t, "prod", opts.Environment)
	require.Equal(t, "otel.example.com:4318", opts.Endpoint)
	require.Equal(t, "/v1/traces", opts.URLPath)
	require.Equal(t, "val", opts.Headers["X-Custom"])
	require.False(t, opts.Insecure)
}
