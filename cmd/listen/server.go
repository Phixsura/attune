// server.go holds the `listen server` bootstrap: it loads config, wires up
// OpenTelemetry, the pgx pool, the LLM client, repos/services, the outbox +
// digest background workers, the chi router (lark + ingest + console mounts)
// and runs the HTTP server until SIGINT/SIGTERM. The small OTel header helpers
// and the signal-driven context live here too since they are only used by the
// server path. Subcommand dispatch and CLI plumbing stay in main.go.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/riandyrn/otelchi"
	"go.opentelemetry.io/otel/trace"

	"github.com/Phixsura/listen/internal/handlers"
	"github.com/Phixsura/listen/internal/infra/apikey"
	"github.com/Phixsura/listen/internal/infra/config"
	"github.com/Phixsura/listen/internal/infra/database"
	"github.com/Phixsura/listen/internal/infra/metrics"
	"github.com/Phixsura/listen/internal/logext"
	"github.com/Phixsura/listen/internal/notify"
	"github.com/Phixsura/listen/internal/repo"
	"github.com/Phixsura/listen/internal/service"
	"github.com/Phixsura/listen/internal/observability"
)

// ── server ────────────────────────────────────────────────────────────────

func runServer() error {
	const where = "main.runServer"
	cfg, err := config.Load()
	if err != nil {
		logext.Errorf(context.Background(), "[%s] config.Load failed,err:%+v", where, err.Error())
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	logext.Infof(ctx, "[%s] start,port:%d", where, cfg.Port)

	// OpenTelemetry tracer。endpoint 空 = noop(本地能跑),prod 部署后通过 .env
	// 配 OTEL_EXPORTER_OTLP_ENDPOINT 才真上报 SLS Trace。详见
	// docs/observability-trace-design.md
	//
	// Listen 是对外服务(私有化部署 / SaaS),OTel 完全非侵入:
	//   - 客户端可选传 W3C traceparent,不传也工作
	//   - 响应头 X-Trace-Id 是 optional debug 字段,API 契约不变
	//   - 内部业务日志带 trace_id 仅供运维 / SLS 用,客户不感知
	otelShutdown, err := observability.InitTracer(ctx, observability.Options{
		ServiceName:    "casceneai-listen",
		ServiceVersion: envOrDefault("APP_VERSION", "dev"),
		Environment:    envOrDefault("ENV", "dev"),
		Endpoint:       os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		URLPath:        envOrDefault("OTEL_EXPORTER_OTLP_TRACES_PATH", "/opentelemetry/v1/traces"),
		Headers:        parseOTelHeaders(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")),
		Insecure:       os.Getenv("OTEL_EXPORTER_OTLP_INSECURE") == "true",
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if err := otelShutdown(shutdownCtx); err != nil {
			slog.WarnContext(shutdownCtx, "otel shutdown failed", "err", err)
		}
	}()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("pgxpool: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("pg ping: %w", err)
	}
	slog.InfoContext(ctx, "postgres connected")

	if err := database.RunMigrations(ctx, pool); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	llm, err := buildLLMClient(cfg)
	if err != nil {
		return err
	}
	defer llm.Close()
	slog.InfoContext(ctx, "llm backend ready", "endpoint", cfg.LLMOpenAIBaseURL)

	feedbackRepo := repo.NewFeedback(pool)
	apikeyRepo := repo.NewAPIKey(pool)
	tenantRepo := repo.NewTenant(pool)
	notifyTargetRepo := repo.NewNotifyTarget(pool)
	outboxRepo := repo.NewOutbox(pool)
	enricher := service.NewEnricher(feedbackRepo, llm)
	ingestor := service.NewIngestor(feedbackRepo, enricher)
	apiKeys := service.NewAPIKeys(apikeyRepo)

	if err := syncCustomWebhooks(ctx, cfg.CustomWebhooks, tenantRepo, notifyTargetRepo); err != nil {
		return fmt.Errorf("sync custom webhooks: %w", err)
	}
	notifier, err := buildNotifier(ctx, cfg, notifyTargetRepo)
	if err != nil {
		return fmt.Errorf("build notifier: %w", err)
	}
	if notifier != nil {
		enricher.SetNotifier(notifier)
	}
	// Outbox wiring: enricher writes raw-webhook rows in same tx as
	// MarkDone (at-least-once); a background worker drains them.
	enricher.SetOutbox(outboxRepo, notifyTargetRepo)
	outboxWorker := service.NewOutboxWorker(
		outboxRepo, notifyTargetRepo,
		notify.NewTransport(nil, notify.DefaultRetry()),
	)
	go outboxWorker.Run(ctx)
	// listen_outbox_lag_seconds is refreshed on a 30s ticker rather than
	// on every Prometheus scrape — avoids hammering the DB.
	go runOutboxLagRefresher(ctx, outboxRepo)

	// Phase 5 M6 weekly digest scheduler. Ticks every 30 min; scans
	// tenants whose last_digest_sent_at < now-6d AND has at least one
	// active lark-bot; composes 7-day summary + sends via SendAlert.
	go service.NewDigestService(tenantRepo, feedbackRepo, notifyTargetRepo).Run(ctx)

	larkHandler, err := handlers.NewLarkHandler(ctx, tenantRepo, ingestor,
		cfg.LarkSigningSecret, cfg.LarkVerificationToken, cfg.LarkDefaultTenantSlug)
	if err != nil {
		return err
	}
	if cfg.LarkEnabled() {
		slog.InfoContext(ctx, "lark webhook enabled", "tenant_slug", cfg.LarkDefaultTenantSlug)
	} else {
		slog.InfoContext(ctx, "lark webhook disabled (no signing secret)")
	}
	ingestHandler := handlers.NewIngestHandler(ingestor)

	r := chi.NewRouter()
	// otelchi 入口产 root span(从客户端 traceparent 继承 or 兜底生成可读 trace_id)。
	// 必须最先,过滤 /health 避免心跳塞满 trace。
	r.Use(otelchi.Middleware("casceneai-listen", otelchi.WithFilter(func(r *http.Request) bool {
		return !strings.HasPrefix(r.URL.Path, "/health") && !strings.HasPrefix(r.URL.Path, "/metrics")
	})))
	// X-Trace-Id 响应头(对客户 optional debug,API 契约不强制)
	r.Use(traceIDResponseHeader)
	r.Use(middleware.RequestID) // chi 自己的 X-Request-ID(向后兼容,跟 X-Trace-Id 并存)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
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
			return fmt.Errorf("build console: %w", err)
		}
		r.Route("/fb/v1/console", func(r chi.Router) {
			r.Mount("/", consoleRouter)
		})
		slog.InfoContext(ctx, "console enabled", "base_url", cfg.ConsoleBaseURL)
	} else {
		slog.InfoContext(ctx, "console disabled (no CONSOLE_SESSION_KEY)")
	}

	go enricher.RunBackground(ctx, cfg.EnricherInterval, cfg.EnricherBatch)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.InfoContext(ctx, "listen server listening", "addr", srv.Addr)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case <-ctx.Done():
		slog.InfoContext(ctx, "shutting down")
		shutdownCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()
	return ctx, cancel
}

// ── OTel helpers ──

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseOTelHeaders(raw string) map[string]string {
	out := map[string]string{}
	if raw == "" {
		return out
	}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		idx := strings.Index(pair, "=")
		if idx <= 0 {
			continue
		}
		out[strings.TrimSpace(pair[:idx])] = strings.TrimSpace(pair[idx+1:])
	}
	return out
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
