// server.go holds the `attune server` bootstrap: it loads config, wires up
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
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/handlers"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/infra/observability"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/apikey"
	"github.com/Phixsura/attune/internal/service/enrich"
	"github.com/Phixsura/attune/internal/service/ingest"
	"github.com/Phixsura/attune/internal/service/outbox"
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

	// OpenTelemetry tracer. Empty endpoint = noop (local dev works
	// without config); set OTEL_EXPORTER_OTLP_ENDPOINT in .env to ship
	// spans to a real collector. Details: docs/observability-trace-design.md.
	//
	// attune is a customer-facing service (private-deploy / SaaS), so
	// OTel stays non-invasive:
	// - clients may pass a W3C traceparent header; if absent we generate one
	// - the X-Trace-Id response header is an optional debug aid, not contract
	// - business logs carry trace_id for operators; clients don't see it
	otelShutdown, err := setupTracing(ctx)
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer shutdownTracing(otelShutdown)

	pool, err := setupDatabase(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	llm, err := buildLLMClient(cfg)
	if err != nil {
		return err
	}
	defer llm.Close()
	logext.Infof(ctx, "[%s] llm backend ready,endpoint:%s", where, cfg.LLMOpenAIBaseURL)

	feedbackRepo := feedback.NewFeedback(pool)
	apikeyRepo := apikeyrepo.NewAPIKey(pool)
	tenantRepo := tenant.NewTenant(pool)
	notifyTargetRepo := notifytarget.NewNotifyTarget(pool)
	outboxRepo := outboxrepo.NewOutbox(pool)
	enricher := enrich.NewEnricher(feedbackRepo, llm, cfg.LLMModel)
	ingestor := ingest.NewIngestor(feedbackRepo, enricher)
	apiKeys := apikey.NewAPIKeys(apikeyRepo)

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
	outboxWorker := outbox.NewOutboxWorker(
		outboxRepo, notifyTargetRepo,
		notify.NewTransport(nil, notify.DefaultRetry()),
	)
	go outboxWorker.Run(ctx)
	// attune_outbox_lag_seconds is refreshed on a 30s ticker rather than
	// on every Prometheus scrape — avoids hammering the DB.
	go runOutboxLagRefresher(ctx, outboxRepo)

	// weekly digest weekly digest scheduler. Ticks every 30 min; scans
	// tenants whose last_digest_sent_at < now-6d AND has at least one
	// active lark-bot; composes 7-day summary + sends via SendAlert.
	go outbox.NewDigestService(tenantRepo, feedbackRepo, notifyTargetRepo).Run(ctx)

	larkHandler, err := handlers.NewLarkHandler(ctx, tenantRepo, ingestor,
		cfg.LarkSigningSecret, cfg.LarkVerificationToken, cfg.LarkDefaultTenantSlug)
	if err != nil {
		return err
	}
	if cfg.LarkEnabled() {
		logext.Infof(ctx, "[%s] lark webhook enabled,tenant_slug:%s", where, cfg.LarkDefaultTenantSlug)
	} else {
		logext.Infof(ctx, "[%s] lark webhook disabled (no signing secret)", where)
	}
	ingestHandler := handlers.NewIngestHandler(ingestor)

	r, err := buildRouter(ctx, cfg, larkHandler, ingestHandler, apiKeys, pool)
	if err != nil {
		return err
	}

	go enricher.RunBackground(ctx, cfg.EnricherInterval, cfg.EnricherBatch)

	srv := ptrext.Of(http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	})
	logext.Infof(ctx, "[%s] attune server listening,addr:%s", where, srv.Addr)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case <-ctx.Done():
		logext.Infof(ctx, "[%s] shutting down", where)
		shutdownCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// setupTracing builds the OpenTelemetry tracer from env. An empty
// endpoint reduces to a no-op (so local dev runs without extra
// configuration); set OTEL_EXPORTER_OTLP_ENDPOINT in prod to ship
// spans. OTel stays non-invasive to attune's API contract — clients
// may pass W3C traceparent, the X-Trace-Id response header is an
// operator debug aid, and trace_id in internal logs serves operators
// only. Details: docs/observability-trace-design.md.
func setupTracing(ctx context.Context) (func(context.Context) error, error) {
	return observability.InitTracer(ctx, observability.Options{
		ServiceName:    "attune",
		ServiceVersion: envOrDefault("APP_VERSION", "dev"),
		Environment:    envOrDefault("ENV", "dev"),
		Endpoint:       os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		URLPath:        envOrDefault("OTEL_EXPORTER_OTLP_TRACES_PATH", "/opentelemetry/v1/traces"),
		Headers:        parseOTelHeaders(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")),
		Insecure:       os.Getenv("OTEL_EXPORTER_OTLP_INSECURE") == "true",
	})
}

// shutdownTracing flushes the tracer on exit with a bounded timeout.
func shutdownTracing(shutdown func(context.Context) error) {
	shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	if err := shutdown(shutdownCtx); err != nil {
		logext.Warnf(shutdownCtx, "[main.shutdownTracing] otel shutdown failed,err:%+v", err.Error())
	}
}

// setupDatabase opens the pgx pool, verifies connectivity, and applies
// migrations. The caller owns the returned pool (defer pool.Close()).
func setupDatabase(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("pgxpool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pg ping: %w", err)
	}
	logext.Infof(ctx, "[main.setupDatabase] postgres connected")
	if err := database.RunMigrations(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return pool, nil
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
