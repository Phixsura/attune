// router.go builds the HTTP router for `attune server`: the OTel root span +
// X-Trace-Id middleware, the standard chi middleware chain, health/metrics, the
// /v1 API surface, and the optional Console mount. Split out of
// server.go to keep each file under the 300-line cap (CLAUDE.md §1).
package main

import (
	"context"
	"fmt"
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
	"github.com/Phixsura/attune/internal/pkg/logext"
	apikeysvc "github.com/Phixsura/attune/internal/service/apikey"
)

// buildRouter wires the chi router: OTel root span + X-Trace-Id, the standard
// middleware chain, /healthz, /metrics, the /v1 API (lark webhook + api-key /
// rate-limited feedback ingest), and — when CONSOLE_SESSION_KEY is set — the
// Console under /fb/v1/console.
func buildRouter(
	ctx context.Context,
	cfg *config.Config,
	larkHandler *handlers.LarkHandler,
	ingestHandler *handlers.IngestHandler,
	apiKeys *apikeysvc.APIKeys,
	pool *pgxpool.Pool,
) (chi.Router, error) {
	r := chi.NewRouter()
	// otelchi opens the root span — continued from a client-supplied
	// traceparent when present, generated from our readable trace_id
	// format otherwise. It must run first. The /health prefix filter
	// covers /healthz plus any leftover /health probes (now 404) so
	// liveness checks don't flood the trace backend.
	r.Use(otelchi.Middleware("attune", otelchi.WithFilter(func(r *http.Request) bool {
		return !strings.HasPrefix(r.URL.Path, "/health") && !strings.HasPrefix(r.URL.Path, "/metrics")
	})))
	// X-Trace-Id response header — optional debug aid for clients; the
	// API contract does not require it.
	r.Use(traceIDResponseHeader)
	r.Use(middleware.RequestID) // chi's own X-Request-ID — kept alongside X-Trace-Id for back-compat
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

	// Console UI. Mounted under /fb/v1/console; the reverse
	// proxy forwards external traffic here. Disabled gracefully
	// when ConsoleSessionKey is empty (single-process dev defaults).
	if cfg.ConsoleSessionKey != "" {
		consoleRouter, err := buildConsoleRouter(cfg, pool)
		if err != nil {
			return nil, fmt.Errorf("build console: %w", err)
		}
		r.Route("/fb/v1/console", func(r chi.Router) {
			r.Mount("/", consoleRouter)
		})
		logext.Infof(ctx, "[main.buildRouter] console enabled,base_url:%s", cfg.ConsoleBaseURL)
	} else {
		logext.Infof(ctx, "[main.buildRouter] console disabled (no CONSOLE_SESSION_KEY)")
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

// traceIDResponseHeader writes the active OTel trace_id into the
// X-Trace-Id response header so clients can correlate from their side.
// Must run AFTER otelchi — otherwise SpanContext is not yet populated.
func traceIDResponseHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if span.SpanContext().IsValid() {
			w.Header().Set("X-Trace-Id", span.SpanContext().TraceID().String())
		}
		next.ServeHTTP(w, r)
	})
}
