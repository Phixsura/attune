// Package logext —— 日志辅助:printf 风格的 slog 封装 + 结构体 JSON 序列化。
//
// 为什么:slog 原生 API `slog.InfoContext(ctx, "msg", "k1", v1, "k2", v2)` 是
// 结构化 key-value 风格,可搜索但读起来啰嗦,跟错时一行 log 看不全字段值。
// 这层封装支持 printf 风格,人眼跟错的可读性优先。
//
// 这是 attune 这边的本地副本(从 backend/internal/logext 复制改的) — 因为
// attune 是独立 go.mod 不跟 backend 互 import。 唯一改动:序列化用
// encoding/json 替 sonic,避免给 attune 引新 dep(attune Day-1 纪律是"不引
// 新 dep" — 见 CLAUDE.md / code_no_new_deps memory)。 性能差异在日志路径
// 可忽略(序列化不是 hot path)。
//
// 使用:
//
//	const where = "notify.PushToTarget"
//	logext.Warnf(ctx, "[%s] push failed,target_id:%s,err:%+v",
//	    where, target.ID, err.Error())
//
// 看结构体用 AsLogParam:
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

// Debugf —— printf 风格的 slog.DebugContext 封装。
func Debugf(ctx context.Context, format string, args ...any) {
	slog.DebugContext(ctx, fmt.Sprintf(format, args...))
}

// Infof —— printf 风格的 slog.InfoContext 封装。
func Infof(ctx context.Context, format string, args ...any) {
	slog.InfoContext(ctx, fmt.Sprintf(format, args...))
}

// Warnf —— printf 风格的 slog.WarnContext 封装。
func Warnf(ctx context.Context, format string, args ...any) {
	slog.WarnContext(ctx, fmt.Sprintf(format, args...))
}

// Errorf —— printf 风格的 slog.ErrorContext 封装。
func Errorf(ctx context.Context, format string, args ...any) {
	slog.ErrorContext(ctx, fmt.Sprintf(format, args...))
}

// AsLogParam —— 结构体 → JSON 字符串,塞进日志 format 用 %s 占位。
//
// attune 用 stdlib encoding/json(vs backend 用 bytedance/sonic) — 日志序列化
// 不是 hot path,stdlib 够用,且 attune 严守"不引新 dep"纪律。
// 序列化失败返 "<marshal-err: ...>"(不抛错,日志容错优先)。
//
// 跟 fmt 的 %+v 区别:
//   - %+v:Go 默认 reflect dump,unexported 字段也出
//   - AsLogParam:JSON,只出 exported,proto 类型遵循 json 标签更干净
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
