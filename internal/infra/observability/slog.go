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
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// TraceIDHandler wraps an underlying slog.Handler and adds trace_id/span_id
// attributes from the active OTel span (if any).
type TraceIDHandler struct {
	slog.Handler
}

// NewTraceIDHandler wraps an existing slog.Handler.
func NewTraceIDHandler(h slog.Handler) *TraceIDHandler {
	return &TraceIDHandler{Handler: h}
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
	return &TraceIDHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *TraceIDHandler) WithGroup(name string) slog.Handler {
	return &TraceIDHandler{Handler: h.Handler.WithGroup(name)}
}
