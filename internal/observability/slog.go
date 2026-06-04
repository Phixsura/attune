// slog.go — TraceIDHandler 自动从 OTel context 提取 trace_id / span_id 注入到 slog.
//
// 设计:wrap 任何 slog.Handler(JSON / Text 都行),在 Handle 时检查 ctx 是否有
// 有效的 OTel SpanContext,有的话把 trace_id 和 span_id 作为 attribute 加到 record.
//
// 业务代码用 slog.InfoContext(ctx, ...) 触发 ctx 传递,handler 才能拿到 span.
// 如果业务用 slog.InfoContext(ctx, ...) 不传 ctx → 拿不到 trace_id,字段为空。
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
