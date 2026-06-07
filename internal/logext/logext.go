// Package logext — printf-style wrappers over slog plus a small
// JSON-AsLogParam helper.
//
// Why: the slog API “slog.InfoContext(ctx, "msg", "k1", v1, "k2", v2)“
// is structured key/value, great for log aggregators but verbose when
// a human is reading one line in `docker logs`. These wrappers accept
// printf format + args so single-line debug output stays readable.
//
// JSON marshalling uses stdlib encoding/json — log paths are not hot
// enough to justify a faster serializer, and attune's "no new deps"
// discipline trumps the marginal performance gain.
//
// Usage:
//
//	const where = "notify.PushToTarget"
//	logext.Warnf(ctx, "[%s] push failed,target_id:%s,err:%+v",
//	    where, target.ID, err.Error())
//
// For struct payloads in a log line, use AsLogParam:
//
//	logext.Infof(ctx, "[%s] req:%s,resp:%s", where,
//	    logext.AsLogParam(req), logext.AsLogParam(resp))
package logext

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// Debugf is a printf-style slog.DebugContext wrapper.
func Debugf(ctx context.Context, format string, args ...any) {
	slog.DebugContext(ctx, fmt.Sprintf(format, args...))
}

// Infof is a printf-style slog.InfoContext wrapper.
func Infof(ctx context.Context, format string, args ...any) {
	slog.InfoContext(ctx, fmt.Sprintf(format, args...))
}

// Warnf is a printf-style slog.WarnContext wrapper.
func Warnf(ctx context.Context, format string, args ...any) {
	slog.WarnContext(ctx, fmt.Sprintf(format, args...))
}

// Errorf is a printf-style slog.ErrorContext wrapper.
func Errorf(ctx context.Context, format string, args ...any) {
	slog.ErrorContext(ctx, fmt.Sprintf(format, args...))
}

// AsLogParam marshals v to a compact JSON string suitable for embedding
// in a logext.{Info,Warn,Error}f format with %s. On marshal failure it
// returns "<marshal-err: ...>" rather than panicking — fault tolerance
// in the log path outweighs strict error propagation.
//
// Versus fmt's %+v:
//   - %+v: Go reflect dump; emits unexported fields and is verbose.
//   - AsLogParam: JSON, exported fields only, honours `json:` tags
//     (proto-generated types render cleanly).
func AsLogParam(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<marshal-err: %v>", err)
	}
	return string(b)
}
