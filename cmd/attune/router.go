// router.go builds the HTTP router for `attune server`: the OTel root span +
// X-Trace-Id middleware, the standard chi middleware chain, health/metrics, the
// /v1 API surface, and the optional Stage B console mount. Split out of
// server.go to keep each file under the 300-line cap (CLAUDE.md §1).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riandyrn/otelchi"
	"go.opentelemetry.io/otel/trace"

	"github.com/Phixsura/attune/internal/handlers"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/service"
)

// buildRouter wires the chi router: OTel root span + X-Trace-Id, the standard
// middleware chain, /healthz, /metrics, the /v1 API (lark webhook + api-key /
// rate-limited feedback ingest), and — when CONSOLE_SESSION_KEY is set — the
// Stage B console under /fb/v1/console.
func buildRouter(
	ctx context.Context,
	cfg *config.Config,
	larkHandler *handlers.LarkHandler,
	ingestHandler *handlers.IngestHandler,
	apiKeys *service.APIKeys,
	pool *pgxpool.Pool,
) (chi.Router, error) {
	r := chi.NewRouter()
	// otelchi 入口产 root span(从客户端 traceparent 继承 or 兜底生成可读 trace_id)。
	// 必须最先。按 /health 前缀过滤,避免心跳塞满 trace —— 该前缀覆盖 /healthz,
	// 并顺带把任何残留 /health 探活(现已 404)挡在 trace 之外。
	r.Use(otelchi.Middleware("attune", otelchi.WithFilter(func(r *http.Request) bool {
		return !strings.HasPrefix(r.URL.Path, "/health") && !strings.HasPrefix(r.URL.Path, "/metrics")
	})))
	// X-Trace-Id 响应头(对客户 optional debug,API 契约不强制)
	r.Use(traceIDResponseHeader)
	r.Use(middleware.RequestID) // chi 自己的 X-Request-ID(向后兼容,跟 X-Trace-Id 并存)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	mountHealth(r)
	// Prometheus scrape endpoint. Restrict to internal CIDR via nginx
	// in production — no auth at the Go level.
	r.Handle("/metrics", metrics.Handler())
	rateLimiter := buildRateLimiter(cfg)

	r.Route("/v1", func(r chi.Router) {
		r.Mount("/lark", larkHandler.Routes())
		r.Group(func(r chi.Router) {
			r.Use(apikey.Middleware(apiKeys))
			r.Use(rateLimiter.Middleware)
			r.Mount("/feedback", ingestHandler.Routes())
		})
	})

	// Stage B 控制台 (console). Mounted under /fb/v1/console; gateway/
	// nginx reverse-proxies external traffic here. Disabled gracefully
	// when ConsoleSessionKey is empty (single-process dev defaults).
	if cfg.ConsoleSessionKey != "" {
		consoleRouter, err := buildConsoleRouter(cfg, pool)
		if err != nil {
			return nil, fmt.Errorf("build console: %w", err)
		}
		r.Route("/fb/v1/console", func(r chi.Router) {
			r.Mount("/", consoleRouter)
		})
		slog.InfoContext(ctx, "console enabled", "base_url", cfg.ConsoleBaseURL)
	} else {
		slog.InfoContext(ctx, "console disabled (no CONSOLE_SESSION_KEY)")
	}
	return r, nil
}

// mountHealth registers the liveness probe at /healthz — the Google/Kubernetes
// convention, where the trailing "z" keeps a health route from colliding with a
// real application path. (The pre-0.2 /health route was removed; see CHANGELOG.)
// The otelchi /health-prefix filter in buildRouter keeps /healthz out of traces.
func mountHealth(r chi.Router) {
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
}

// traceIDResponseHeader 把 OTel trace_id 写到 X-Trace-Id 响应头(给客户 debug 用)。
// 必须在 otelchi 之后(否则 SpanContext 不可见)。
func traceIDResponseHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if span.SpanContext().IsValid() {
			w.Header().Set("X-Trace-Id", span.SpanContext().TraceID().String())
		}
		next.ServeHTTP(w, r)
	})
}
