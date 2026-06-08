// slog.go — TraceIDHandler decorates any slog.Handler so trace_id and
// span_id from the OTel SpanContext on the call's ctx land as
// attributes on every record.
//
// The wrapper is transparent to JSON / Text handlers — Handle inspects
// ctx, attaches the IDs when a valid SpanContext is present, and
// delegates to the inner handler unchanged.
//
// Business code must call slog.InfoContext(ctx, ...) (not slog.Info)
// for the ctx to be threaded through; otherwise the IDs are absent.
package observability

import (
	"context"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// BuildDefaultHandler returns the attune-standard slog handler chain: a JSON
// handler (or TextHandler when ENV=dev) wrapped in TraceIDHandler so every
// record carries trace_id/span_id from the active OTel span on ctx.
//
// Pure — no global state. Tests construct a handler over a bytes.Buffer here
// without touching slog.Default. Production wiring is InstallDefaultLogger.
func BuildDefaultHandler(w io.Writer) slog.Handler {
	opts := ptrext.Of(slog.HandlerOptions{Level: slog.LevelInfo})
	var inner slog.Handler
	if os.Getenv("ENV") == "dev" {
		inner = slog.NewTextHandler(w, opts)
	} else {
		inner = slog.NewJSONHandler(w, opts)
	}
	return NewTraceIDHandler(inner)
}

// InstallDefaultLogger builds the default handler over os.Stdout and installs
// it as slog's package-level default. Call once at process start, before any
// logging. CLAUDE.md §7 routes all log calls through internal/pkg/logext,
// which forwards to this default.
func InstallDefaultLogger() {
	slog.SetDefault(slog.New(BuildDefaultHandler(os.Stdout)))
}

// TraceIDHandler wraps an underlying slog.Handler and adds trace_id/span_id
// attributes from the active OTel span (if any).
type TraceIDHandler struct {
	slog.Handler
}

// NewTraceIDHandler wraps an existing slog.Handler.
func NewTraceIDHandler(h slog.Handler) *TraceIDHandler {
	return ptrext.Of(TraceIDHandler{Handler: h})
}

func (h *TraceIDHandler) Handle(ctx context.Context, r slog.Record) error {
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		sc := span.SpanContext()
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

func (h *TraceIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return ptrext.Of(TraceIDHandler{Handler: h.Handler.WithAttrs(attrs)})
}

func (h *TraceIDHandler) WithGroup(name string) slog.Handler {
	return ptrext.Of(TraceIDHandler{Handler: h.Handler.WithGroup(name)})
}
