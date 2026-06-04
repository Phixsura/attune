// attrs.go — 跨服务统一的日志字段名常量。
//
// 设计原则:
//  1. 字段语义跨协议本来就不同。HTTP 跟 gRPC 是两个维度,不强行用同名。
//  2. 命名照 OTel Semantic Conventions(https://opentelemetry.io/docs/specs/semconv/)。
//     OTel SemConv 用 dot notation 如 `http.method`,SLS 把 `.` 当字段分隔会
//     自动 flatten,我们直接用 `_` 形式避免歧义。
//  3. 用 const 而非 string literal,业务代码任何字段名漂移在编译期就阻断。
//
// 业务代码用法:
//
//	slog.InfoContext(ctx, "request",
//	    observability.AttrHTTPMethod, r.Method,
//	    observability.AttrHTTPRoute,  r.URL.Path,
//	    observability.AttrHTTPStatus, sw.status,
//	    observability.AttrDurationMs, dur,
//	)
package observability

// ── 通用字段(所有服务共享)──

// AttrTraceID 跟 OTel SpanContext.TraceID 同源,由 TraceIDHandler 自动注入。
const AttrTraceID = "trace_id"

// AttrSpanID 跟 OTel SpanContext.SpanID 同源,由 TraceIDHandler 自动注入。
const AttrSpanID = "span_id"

// AttrDurationMs 单位毫秒,整数。跟 BE/Gateway/Listen 一致,SLS 可 SQL 聚合 P99。
const AttrDurationMs = "duration_ms"

// AttrService 服务名,如 "casceneai-be" / "casceneai-gateway"。InitTracer 时已注入
// Resource,业务代码一般不用手动加。
const AttrService = "service"

// AttrError 错误对象。slog 自动序列化为字符串。
const AttrError = "error"

// ── HTTP 协议字段(BE / Listen)──

// AttrHTTPMethod HTTP 请求方法 GET/POST/PUT/DELETE/...
const AttrHTTPMethod = "http_method"

// AttrHTTPRoute HTTP 路径(可含路径模板 /api/v1/projects/{id})。
const AttrHTTPRoute = "http_route"

// AttrHTTPStatus HTTP 响应状态码 200/4xx/5xx。
const AttrHTTPStatus = "http_status_code"

// AttrHTTPUserAgent HTTP User-Agent header。
const AttrHTTPUserAgent = "http_user_agent"

// AttrClientIP 客户端 IP(从 X-Forwarded-For / X-Real-IP / RemoteAddr 提取)。
const AttrClientIP = "client_ip"

// ── gRPC 协议字段 ──

// AttrRPCService gRPC service 全限定名,如 "auth.v1.Auth"。
const AttrRPCService = "rpc_service"

// AttrRPCMethod gRPC method 名,如 "ChatCompletion"。
const AttrRPCMethod = "rpc_method"

// AttrRPCGRPCStatus gRPC status code(OK/InvalidArgument/Unauthenticated/...)。
const AttrRPCGRPCStatus = "rpc_grpc_status"

// ── 业务字段(跨服务通用业务概念)──

// AttrUserID 业务用户 ID(BE 解 JWT 后注入)。
const AttrUserID = "user_id"

// AttrTenantID 业务租户 ID。
const AttrTenantID = "tenant_id"

// AttrShotID storyboard shot ID。
const AttrShotID = "shot_id"

// AttrEpisodeID storyboard episode ID。
const AttrEpisodeID = "episode_id"

// AttrTaskID gateway task ID。
const AttrTaskID = "task_id"

// AttrModelID LLM model identifier。
const AttrModelID = "model_id"
