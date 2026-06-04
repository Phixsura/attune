// idgen.go — ReadableIDGenerator 产生人类可读时间戳前缀的 trace_id。
//
// trace_id 格式(W3C 兼容,32 lowercase hex chars):
//
//	20260515193045a1b2c3d40005f3e7a8
//	└──yyyyMMddHHmmss──┘└────18 字符随机────┘
//	  时间戳 (UTC, 14 hex chars = 7 bytes)
//
// 客服看到 trace_id → 肉眼读出时间 → 在 SLS 用 trace_id 全量查或时间窗筛选。
//
// Entropy: 72-bit 随机后缀。一天 10M trace 撞概率 ~ 10^-6,够用。
// 字典序 = 时间序,SLS Trace UI 默认按 trace_id 升序就是时间升序。
//
// FE 是 trace_id 真正源头(浏览器 OTel SDK 用 ReadableIdGenerator 同格式生成,
// traceparent 透传到 BE/Gateway,服务端继承不重新生成)。本 generator 给
// curl / cron / 第三方 webhook 等无 traceparent 入站场景兜底生成。
//
// ⚠ 采样:当前 AlwaysSample()。未来切 TraceIDRatioBased 时,同秒 trace 会被
// 同 sampling 决策。需要 ratio 采样时,把时间戳挪到 TraceID 尾部。
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

	// 前 7 字节(14 hex chars)= yyyyMMddHHmmss 的 ASCII hex 编码
	// 注:"20260515" 这种全是 0-9 字符,本身就是合法 hex
	ts := time.Now().UTC().Format("20060102150405")
	tsBytes, err := hex.DecodeString(ts)
	if err == nil && len(tsBytes) == 7 {
		copy(tid[:7], tsBytes)
	}
	// 后 9 字节随机
	_, _ = rand.Read(tid[7:])

	_, _ = rand.Read(sid[:])
	return tid, sid
}

func (g *ReadableIDGenerator) NewSpanID(ctx context.Context, _ trace.TraceID) trace.SpanID {
	var sid trace.SpanID
	_, _ = rand.Read(sid[:])
	return sid
}
