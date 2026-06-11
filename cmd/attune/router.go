// router.go builds the HTTP router for `attune server`: the OTel root span +
// X-Trace-Id middleware, the standard chi middleware chain, health/metrics, the
// /v1 API surface, and the optional Console mount. Split out of
// server.go to keep each file under the 300-line cap (CLAUDE.md §1).
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riandyrn/otelchi"
	"go.opentelemetry.io/otel/trace"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/handlers"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/repo/admin"
	inboundsourcerepo "github.com/Phixsura/attune/internal/repo/inboundsource"
	apikeysvc "github.com/Phixsura/attune/internal/service/apikey"
)

// buildRouter wires the chi router: OTel root span + X-Trace-Id, the standard
// middleware chain, /healthz, /metrics, the /v1 API (api-key /
// rate-limited feedback ingest + inbound adapter mux), and — when
// CONSOLE_SESSION_KEY is set — the Console under /fb/v1/console.
func buildRouter(
	ctx context.Context,
	cfg *config.Config,
	ingestHandler *handlers.IngestHandler,
	apiKeys *apikeysvc.APIKeys,
	pool *pgxpool.Pool,
	inboundMux chi.Router,
	inboundSecrets *secretstore.TinkStore,
	inboundSources *inboundsourcerepo.Repo,
	adminRepo *admin.Repo,
) (chi.Router, error) {
	const where = "main.buildRouter"
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
	r.Use(middleware.Timeout(305 * time.Second))
	mountHealth(r)
	// Prometheus scrape endpoint. Restrict to internal CIDR via nginx
	// in production — no auth at the Go level.
	r.Handle("/metrics", metrics.Handler())
	rateLimiter := buildRateLimiter(cfg)

	r.Route("/v1", func(r chi.Router) {
		// Inbound adapter mux. Adapters have already registered their
		// routes onto inboundMux during Manager.StartAll(ctx). Mounting
		// it here exposes them under /v1/inbound/<channel>/...
		if inboundMux != nil {
			r.Mount("/inbound", inboundMux)
		}
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
		consoleRouter, err := buildConsoleRouter(cfg, pool, inboundSecrets, inboundSources, adminRepo)
		if err != nil {
			return nil, fmt.Errorf("build console: %w", err)
		}
		r.Route("/fb/v1/console", func(r chi.Router) {
			r.Mount("/", consoleRouter)
		})
		mountConsoleStatic(ctx, r)
		logext.Infof(ctx, "[%s] console enabled,base_url:%s", where, cfg.ConsoleBaseURL)
	} else {
		logext.Infof(ctx, "[%s] console disabled (no CONSOLE_SESSION_KEY)", where)
	}
	return r, nil
}

func mountConsoleStatic(ctx context.Context, r chi.Router) {
	const where = "main.mountConsoleStatic"
	dir, ok := consoleStaticDir()
	if !ok {
		logext.Warnf(ctx, "[%s] skipped: console dist not found", where)
		return
	}
	handler := consoleSPAHandler(dir)
	r.Get("/console", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/console/", http.StatusMovedPermanently)
	})
	r.Handle("/console/*", handler)
	logext.Infof(ctx, "[%s] mounted,dir:%s", where, dir)
}

func consoleStaticDir() (string, bool) {
	for _, dir := range []string{"/app/console", "console/dist"} {
		info, err := os.Stat(dir)
		if err == nil && info.IsDir() {
			return dir, true
		}
	}
	return "", false
}

func consoleSPAHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rel := strings.TrimPrefix(req.URL.Path, "/console/")
		clean := filepath.Clean(rel)
		if clean == "." ||
			filepath.IsAbs(clean) ||
			strings.HasPrefix(clean, "..") ||
			strings.Contains(clean, string(os.PathSeparator)+".."+string(os.PathSeparator)) ||
			strings.HasSuffix(clean, string(os.PathSeparator)+"..") ||
			strings.Contains(clean, "\\") {
			serveConsoleIndex(w, req, dir)
			return
		}

		baseAbs, err := filepath.Abs(dir)
		if err != nil {
			serveConsoleIndex(w, req, dir)
			return
		}
		targetAbs, err := filepath.Abs(filepath.Join(dir, clean))
		if err != nil {
			serveConsoleIndex(w, req, dir)
			return
		}
		if targetAbs != baseAbs && !strings.HasPrefix(targetAbs, baseAbs+string(os.PathSeparator)) {
			serveConsoleIndex(w, req, dir)
			return
		}

		if info, err := os.Stat(targetAbs); err == nil && !info.IsDir() {
			http.StripPrefix("/console/", files).ServeHTTP(w, req)
			return
		}
		serveConsoleIndex(w, req, dir)
	})
}

func serveConsoleIndex(w http.ResponseWriter, req *http.Request, dir string) {
	http.ServeFile(w, req, filepath.Join(dir, "index.html"))
}

// mountHealth registers the liveness probe at /healthz — the Google/Kubernetes
// convention, where the trailing "z" keeps a health route from colliding with a
// real application path. (The pre-0.2 /health route was removed; see CHANGELOG.)
// The otelchi /health-prefix filter in buildRouter keeps /healthz out of traces.
func mountHealth(r chi.Router) {
	r.Get("/healthz", dispatcher.HealthzHandler())
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
