// attrs.go — shared constants for log field names.
//
// Rules:
//  1. HTTP and gRPC carry semantically different fields; don't force
//     them onto a single name just because they look similar.
//  2. Names follow OTel Semantic Conventions
//     (https://opentelemetry.io/docs/specs/semconv/). OTel uses dot
//     notation like `http.method`; some log backends flatten `.`,
//     so we substitute `_` to avoid downstream ambiguity.
//  3. Use these constants instead of string literals — any drift in
//     field naming is then caught at compile time.
//
// Usage from business code:
//
//	slog.InfoContext(ctx, "request",
//	    observability.AttrHTTPMethod, r.Method,
//	    observability.AttrHTTPRoute,  r.URL.Path,
//	    observability.AttrHTTPStatus, sw.status,
//	    observability.AttrDurationMs, dur,
//	)
package observability

// ── Shared fields (every service uses these) ──

// AttrTraceID mirrors OTel SpanContext.TraceID; injected by TraceIDHandler.
const AttrTraceID = "trace_id"

// AttrSpanID mirrors OTel SpanContext.SpanID; injected by TraceIDHandler.
const AttrSpanID = "span_id"

// AttrDurationMs is an integer millisecond duration. Stable across
// services so the log backend can run a single P99 query.
const AttrDurationMs = "duration_ms"

// AttrService — service name, e.g. "attune". Injected by InitTracer.
// Resource; business code rarely sets it directly.
const AttrService = "service"

// AttrError — the error value; slog stringifies it automatically.
const AttrError = "error"

// ── HTTP protocol fields ──

// AttrHTTPMethod — the HTTP method (GET / POST / PUT / DELETE / ...).
const AttrHTTPMethod = "http_method"

// AttrHTTPRoute — request path; may carry a route template like /api/v1/projects/{id}.
const AttrHTTPRoute = "http_route"

// AttrHTTPStatus — HTTP status code (200, 4xx, 5xx, ...).
const AttrHTTPStatus = "http_status_code"

// AttrHTTPUserAgent — the HTTP User-Agent header value.
const AttrHTTPUserAgent = "http_user_agent"

// AttrClientIP — client IP, derived from X-Forwarded-For / X-Real-IP / RemoteAddr.
const AttrClientIP = "client_ip"

// ── gRPC protocol fields ──

// AttrRPCService — fully-qualified gRPC service, e.g. "auth.v1.Auth".
const AttrRPCService = "rpc_service"

// AttrRPCMethod — gRPC method name, e.g. "ChatCompletion".
const AttrRPCMethod = "rpc_method"

// AttrRPCGRPCStatus — gRPC status code (OK / InvalidArgument / Unauthenticated / ...).
const AttrRPCGRPCStatus = "rpc_grpc_status"

// ── Business fields (shared identity / domain concepts) ──

// AttrUserID — application user id; injected after JWT verification.
const AttrUserID = "user_id"

// AttrTenantID — application tenant id.
const AttrTenantID = "tenant_id"

// AttrShotID — storyboard shot id (carried over from upstream services).
const AttrShotID = "shot_id"

// AttrEpisodeID — storyboard episode id (carried over from upstream services).
const AttrEpisodeID = "episode_id"

// AttrTaskID gateway task ID。
const AttrTaskID = "task_id"

// AttrModelID LLM model identifier。
const AttrModelID = "model_id"
