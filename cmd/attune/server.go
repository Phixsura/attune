// server.go holds the `attune server` bootstrap: it loads config, wires up
// OpenTelemetry, the pgx pool, the LLM client, repos/services, the outbox
// background worker, the chi router (ingest + inbound framework + console
// mounts) and runs the HTTP server until SIGINT/SIGTERM. The small OTel
// signal-driven context lives here too since it is only used by the server
// path. Subcommand dispatch and CLI plumbing stay in main.go.
package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers"
	"github.com/Phixsura/attune/internal/handlers/console"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/observability"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/admin"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	enrichmentruntimerepo "github.com/Phixsura/attune/internal/repo/enrichmentruntime"
	"github.com/Phixsura/attune/internal/repo/feedback"
	gdprrepo "github.com/Phixsura/attune/internal/repo/gdpr"
	"github.com/Phixsura/attune/internal/repo/guardpolicy"
	inboundsourcerepo "github.com/Phixsura/attune/internal/repo/inboundsource"
	llmauditrepo "github.com/Phixsura/attune/internal/repo/llmaudit"
	llmconfigrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/apikey"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	digestsvc "github.com/Phixsura/attune/internal/service/digest"
	embeddingsvc "github.com/Phixsura/attune/internal/service/embedding"
	"github.com/Phixsura/attune/internal/service/enrich"
	enrichruntimesvc "github.com/Phixsura/attune/internal/service/enrichruntime"
	gdprsvc "github.com/Phixsura/attune/internal/service/gdpr"
	"github.com/Phixsura/attune/internal/service/ingest"
	llmauditsvc "github.com/Phixsura/attune/internal/service/llmaudit"
	llmconfigsvc "github.com/Phixsura/attune/internal/service/llmconfig"
	"github.com/Phixsura/attune/internal/service/llmrouter"
	"github.com/Phixsura/attune/internal/service/outbox"
	replydraftsvc "github.com/Phixsura/attune/internal/service/replydraft"
	"github.com/Phixsura/attune/internal/worker/batchjob"

	digestrunrepo "github.com/Phixsura/attune/internal/repo/digestrun"
	digestsubrepo "github.com/Phixsura/attune/internal/repo/digestsubscription"
	embeddingrepo "github.com/Phixsura/attune/internal/repo/embedding"
	feedbackjobrepo "github.com/Phixsura/attune/internal/repo/feedbackjob"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
)

const (
	databaseConnectTimeout       = 60 * time.Second
	databaseConnectRetryInterval = 2 * time.Second
)

type runtimeServices struct {
	llm              *llmauditsvc.Client
	rawLLM           *llmrouter.Router
	rateLimitedLLM   *llmclient.MutableRateLimitedClient
	feedbackRepo     *feedback.FeedbackRepo
	apiKeys          *apikey.APIKeys
	tenantRepo       *tenant.TenantRepo
	notifyTargetRepo *notifytarget.NotifyTargetRepo
	outboxRepo       *outboxrepo.OutboxRepo
	enricher         *enrich.Enricher
	enrichRunner     *enrich.Runner
	enrichRuntime    *enrichruntimesvc.Service
	ingestor         *ingest.Ingestor
}

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

	// Install the SSRF egress policy before any outbound transport is built.
	// Default blocks loopback + private networks (always blocks cloud-metadata /
	// link-local); config relaxes loopback/private for dev / on-prem.
	egress := cfg.EgressPolicy()
	notify.SetEgressPolicy(egress)
	llmclient.SetEgressPolicy(egress)
	// Trusted-proxy hop count for client-IP resolution outside the API-key
	// middleware (audit actor IP, etc.).
	nethardening.SetTrustedProxyHops(cfg.Security.TrustedProxyHops)

	// OpenTelemetry tracer. Empty endpoint = noop; configure
	// observability.otlp_endpoint to ship spans to a real collector.
	// Details: docs/observability-trace-design.md.
	//
	// attune is a customer-facing service (private-deploy / SaaS), so
	// OTel stays non-invasive:
	// - clients may pass a W3C traceparent header; if absent we generate one
	// - the X-Trace-Id response header is an optional debug aid, not contract
	// - business logs carry trace_id for operators; clients don't see it
	otelShutdown, err := setupTracing(ctx, cfg)
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer shutdownTracing(otelShutdown)

	pool, err := setupDatabase(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := database.CheckPgvector(ctx, pool); err != nil {
		return fmt.Errorf("pgvector check: %w", err)
	}

	secrets, err := secretstore.NewTinkStoreFromJSONWithLegacy(
		cfg.Secrets.TinkKeyset,
		cfg.Secrets.LegacyInboundMasterKey,
	)
	if err != nil {
		return err
	}
	runtimeDeps, err := setupRuntimeServices(ctx, pool, cfg, secrets)
	if err != nil {
		return err
	}
	defer runtimeDeps.llm.Close()
	logext.Infof(ctx, "[%s] llm router ready,primary_secret_key:%s", where, secrets.PrimaryKeyID())

	if err := syncCustomWebhooks(ctx, cfg.CustomWebhooks, runtimeDeps.tenantRepo, runtimeDeps.notifyTargetRepo); err != nil {
		return fmt.Errorf("sync custom webhooks: %w", err)
	}
	// Outbox wiring: enricher writes raw-webhook rows in same tx as
	// MarkDone (at-least-once); a background worker drains them.
	runtimeDeps.enricher.SetOutbox(runtimeDeps.outboxRepo, runtimeDeps.notifyTargetRepo)
	outboxWorker := outbox.NewOutboxWorker(
		runtimeDeps.outboxRepo, runtimeDeps.notifyTargetRepo,
		notify.NewTransport(nil, notify.DefaultRetry()),
	)
	safego(ctx, "outbox", func() { outboxWorker.Run(ctx) })
	// attune_outbox_lag_seconds is refreshed on a 30s ticker rather than
	// on every Prometheus scrape — avoids hammering the DB.
	safego(ctx, "outbox_lag_refresher", func() { runOutboxLagRefresher(ctx, runtimeDeps.outboxRepo) })
	safego(ctx, "audit_pruner", func() {
		runAuditPruner(ctx, auditlogsvc.New(auditlogrepo.New(pool)), cfg.AuditRetention, cfg.AuditPruneInterval)
	})
	// Release stale ingest idempotency keys so the partial unique index stays
	// bounded to the recent retry window (#37).
	safego(ctx, "idempotency_key_pruner", func() {
		runIdempotencyKeyPruner(ctx, runtimeDeps.feedbackRepo, idempotencyKeyRetention, idempotencyKeyPruneInterval)
	})
	// MCP OAuth cleanup: expired codes, revoked tokens, idle sessions (#93).
	safego(ctx, "mcp_pruner", func() {
		runMCPPruner(ctx, pool, mcpPruneInterval, mcpSessionIdleLimit)
	})

	batchJobWorker := startBackgroundWorkers(ctx, pool, runtimeDeps.enricher, runtimeDeps.rawLLM, runtimeDeps.llm, runtimeDeps.feedbackRepo, cfg.ConsoleBaseURL, cfg.GDPRExportTTL)
	defer batchJobWorker.Stop()

	ingestHandler := handlers.NewIngestHandler(runtimeDeps.ingestor)

	inb, err := setupInbound(
		ctx,
		pool,
		runtimeDeps.ingestor,
		secrets,
		cfg.Console.BootstrapAdmin,
		cfg.ConsoleSessionKey != "",
	)
	if err != nil {
		return err
	}
	defer inb.shutdown()
	ready := newDrainAwareReadiness(pool)

	r, err := buildRouter(
		ctx, cfg, ingestHandler, runtimeDeps.apiKeys, pool, ready, runtimeDeps.llm,
		inb.subRouter, inb.secrets, inb.sources, inb.adminRepo, runtimeDeps.enrichRuntime,
		runtimeDeps.ingestor,
	)
	if err != nil {
		return err
	}

	srv := ptrext.Of(http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: r,
		// Hardening: ReadHeaderTimeout alone leaves the request body (slow-loris)
		// and slow response readers unbounded. ReadTimeout caps the full request;
		// WriteTimeout sits ABOVE the in-handler middleware.Timeout (305s) so the
		// handler's clean 503 wins and WriteTimeout never cuts a legitimate
		// (e.g. LLM-backed) response mid-flight; IdleTimeout bounds keep-alive
		// sockets.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      315 * time.Second,
		IdleTimeout:       120 * time.Second,
	})
	logext.Infof(ctx, "[%s] attune server listening,addr:%s", where, srv.Addr)

	if err := serveUntilStopped(ctx, srv, ready, cfg.ShutdownDrainDelay, cfg.ShutdownTimeout); err != nil {
		return err
	}
	waitCtx, cancelWait := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelWait()
	if err := runtimeDeps.enrichRunner.Wait(waitCtx); err != nil {
		logext.Warnf(waitCtx, "[%s] enrich runner shutdown timed out,err:%+v", where, err.Error())
	}
	return nil
}

func setupRuntimeServices(
	ctx context.Context,
	pool *pgxpool.Pool,
	cfg *config.Config,
	secrets *secretstore.TinkStore,
) (runtimeServices, error) {
	llmConfigRepo := llmconfigrepo.New(pool)
	llmConfig := llmconfigsvc.NewService(llmConfigRepo, secrets)
	if err := llmConfig.SyncKeyRegistry(ctx); err != nil {
		return runtimeServices{}, fmt.Errorf("sync secret key registry: %w", err)
	}

	rawLLM := llmrouter.New(llmConfigRepo, secrets)
	guardedLLM := llmguard.NewClient(rawLLM, guardpolicy.New(pool))
	rateLimitedLLM := llmclient.NewMutableRateLimitedClient(guardedLLM, llmclient.RateLimitConfig{
		Enabled: cfg.EnricherLLMMaxQPS > 0,
		QPS:     cfg.EnricherLLMMaxQPS,
		Burst:   cfg.EnricherLLMBurst,
	})
	llm := llmauditsvc.NewClient(rateLimitedLLM, llmauditrepo.New(pool))

	feedbackRepo := feedback.NewFeedback(pool)
	apikeyRepo := apikeyrepo.NewAPIKey(pool)
	tenantRepo := tenant.NewTenant(pool)
	notifyTargetRepo := notifytarget.NewNotifyTarget(pool)
	outboxRepo := outboxrepo.NewOutbox(pool)
	enricher := enrich.NewEnricher(feedbackRepo, llm, "")
	enrichRunner := enrich.NewRunner(feedbackRepo, enricher, enrich.RunnerConfig{
		QueueLen:      cfg.EnricherQueueLen,
		Workers:       cfg.EnricherWorkers,
		BatchSize:     cfg.EnricherBatch,
		BatchWindow:   cfg.EnricherBatchWindow,
		SweepInterval: cfg.EnricherInterval,
	})
	go enrichRunner.Run(ctx)

	instanceID := "attune-" + uuid.NewString()
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		instanceID = hostname
	}
	enrichRuntime := enrichruntimesvc.New(
		enrichmentruntimerepo.New(pool),
		enrichRunner,
		rateLimitedLLM,
		enrichmentRuntimeBootstrapSpec(cfg),
		enrichmentRuntimeBootstrapVersion(cfg),
		instanceID,
		uuid.NewString(),
	)
	safego(ctx, "enrich_runtime", func() { enrichRuntime.Run(ctx) })

	return runtimeServices{
		llm:              llm,
		rawLLM:           rawLLM,
		rateLimitedLLM:   rateLimitedLLM,
		feedbackRepo:     feedbackRepo,
		apiKeys:          apikey.NewAPIKeys(apikeyRepo),
		tenantRepo:       tenantRepo,
		notifyTargetRepo: notifyTargetRepo,
		outboxRepo:       outboxRepo,
		enricher:         enricher,
		enrichRunner:     enrichRunner,
		enrichRuntime:    enrichRuntime,
		ingestor:         ingest.NewIngestor(feedbackRepo, enrichRunner),
	}, nil
}

func serveUntilStopped(
	ctx context.Context,
	srv *http.Server,
	ready *drainAwareReadiness,
	drainDelay time.Duration,
	shutdownTimeout time.Duration,
) error {
	const where = "main.serveUntilStopped"
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case <-ctx.Done():
		ready.BeginDrain()
		logext.Infof(
			context.Background(),
			"[%s] shutting down,drain_delay:%s,timeout:%s",
			where,
			drainDelay,
			shutdownTimeout,
		)
		sleepBeforeShutdown(drainDelay)
		shutdownCtx, c := context.WithTimeout(context.Background(), shutdownTimeout)
		defer c()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func sleepBeforeShutdown(delay time.Duration) {
	if delay <= 0 {
		return
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	<-timer.C
}

func runAuditPruner(ctx context.Context, svc *auditlogsvc.Service, retention, interval time.Duration) {
	if svc == nil || retention <= 0 || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	pruneAuditOnce(ctx, svc, retention)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruneAuditOnce(ctx, svc, retention)
		}
	}
}

func pruneAuditOnce(ctx context.Context, svc *auditlogsvc.Service, retention time.Duration) {
	const where = "main.pruneAuditLog"
	rows, err := svc.Prune(ctx, retention)
	if err != nil {
		logext.Warnf(ctx, "[%s] failed,err:%+v", where, err.Error())
		return
	}
	logext.Infof(ctx, "[%s] OK,rows:%d,retention_hours:%d", where, rows, int(retention.Hours()))
}

const (
	// idempotencyKeyRetention is how long an ingest idempotency key stays usable
	// for dedup. A client retry happens within seconds/minutes; 48h is a generous
	// upper bound after which the key is released to keep the index small.
	idempotencyKeyRetention     = 48 * time.Hour
	idempotencyKeyPruneInterval = 6 * time.Hour
)

func runIdempotencyKeyPruner(ctx context.Context, repo *feedback.FeedbackRepo, retention, interval time.Duration) {
	if repo == nil || retention <= 0 || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	pruneIdempotencyKeysOnce(ctx, repo, retention)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruneIdempotencyKeysOnce(ctx, repo, retention)
		}
	}
}

func pruneIdempotencyKeysOnce(ctx context.Context, repo *feedback.FeedbackRepo, retention time.Duration) {
	const where = "main.pruneIdempotencyKeys"
	rows, err := repo.PurgeExpiredIdempotencyKeys(ctx, retention)
	if err != nil {
		logext.Warnf(ctx, "[%s] failed,err:%+v", where, err.Error())
		return
	}
	logext.Infof(ctx, "[%s] OK,rows:%d,retention_hours:%d", where, rows, int(retention.Hours()))
}

const (
	mcpPruneInterval    = 1 * time.Hour
	mcpSessionIdleLimit = 7 * 24 * time.Hour
)

func runMCPPruner(ctx context.Context, pool *pgxpool.Pool, interval, sessionIdleLimit time.Duration) {
	if pool == nil || interval <= 0 {
		return
	}
	codes := mcprepo.NewCodes(pool)
	tokens := mcprepo.NewTokens(pool)
	sessions := mcprepo.NewSessions(pool)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	pruneMCPOnce(ctx, codes, tokens, sessions, sessionIdleLimit)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruneMCPOnce(ctx, codes, tokens, sessions, sessionIdleLimit)
		}
	}
}

func pruneMCPOnce(ctx context.Context, codes *mcprepo.CodesRepo, tokens *mcprepo.TokensRepo, sessions *mcprepo.SessionsRepo, sessionIdleLimit time.Duration) {
	const where = "main.pruneMCP"

	codesRows, err := codes.Cleanup(ctx)
	if err != nil {
		logext.Warnf(ctx, "[%s.codes] failed,err:%+v", where, err.Error())
	} else if codesRows > 0 {
		logext.Infof(ctx, "[%s.codes] OK,rows:%d", where, codesRows)
	}

	tokensRows, err := tokens.Cleanup(ctx)
	if err != nil {
		logext.Warnf(ctx, "[%s.tokens] failed,err:%+v", where, err.Error())
	} else if tokensRows > 0 {
		logext.Infof(ctx, "[%s.tokens] OK,rows:%d", where, tokensRows)
	}

	sessionsRows, err := sessions.CleanupIdle(ctx, sessionIdleLimit)
	if err != nil {
		logext.Warnf(ctx, "[%s.sessions] failed,err:%+v", where, err.Error())
	} else if sessionsRows > 0 {
		logext.Infof(ctx, "[%s.sessions] OK,rows:%d,idle_hours:%d", where, sessionsRows, int(sessionIdleLimit.Hours()))
	}
}

// setupTracing builds the OpenTelemetry tracer from config. An empty
// endpoint reduces to a no-op (so local dev runs without extra
// configuration). OTel stays non-invasive to attune's API contract — clients
// may pass W3C traceparent, the X-Trace-Id response header is an
// operator debug aid, and trace_id in internal logs serves operators
// only. Details: docs/observability-trace-design.md.
func setupTracing(ctx context.Context, cfg *config.Config) (func(context.Context) error, error) {
	return observability.InitTracer(ctx, observability.Options{
		ServiceName:    "attune",
		ServiceVersion: cfg.Observability.ServiceVersion,
		Environment:    cfg.Observability.Environment,
		Endpoint:       cfg.Observability.OTLPEndpoint,
		URLPath:        cfg.Observability.OTLPTracesPath,
		Headers:        cfg.Observability.OTLPHeaders,
		Insecure:       cfg.Observability.OTLPInsecure,
	})
}

// shutdownTracing flushes the tracer on exit with a bounded timeout.
func shutdownTracing(shutdown func(context.Context) error) {
	const where = "main.shutdownTracing"
	shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
	defer c()
	if err := shutdown(shutdownCtx); err != nil {
		logext.Warnf(shutdownCtx, "[%s] otel shutdown failed,err:%+v", where, err.Error())
	}
}

// startEmbeddingWorker initializes and runs the embedding clustering worker.
func startEmbeddingWorker(ctx context.Context, pool *pgxpool.Pool, enricher *enrich.Enricher, rawLLM *llmrouter.Router, llm llmclient.LLMClient) {
	taskRepo := embeddingrepo.NewTaskRepo(pool)
	enricher.SetEmbeddingTask(taskRepo)
	worker := embeddingsvc.NewWorker(taskRepo, rawLLM, llm, llmauditrepo.New(pool))
	safego(ctx, "embedding", func() { worker.Run(ctx) })
	safego(ctx, "embed_queue_refresher", func() {
		runQueueDepthRefresher(ctx, "embed", taskRepo.QueueDepthByTenant, func(d map[string]int64) {
			metrics.RefreshQueueDepth(metrics.EmbedQueueDepth, d)
		})
	})
}

// startReplyDraftWorker wires the reply-draft outbox + worker. llm is the
// audit-wrapping client so each draft call lands in llm_audit
// (purpose='reply_draft').
func startReplyDraftWorker(ctx context.Context, pool *pgxpool.Pool, enricher *enrich.Enricher, llm llmclient.LLMClient) {
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	enricher.SetDraftTask(repo)
	worker := replydraftsvc.NewWorker(repo, llm)
	safego(ctx, "reply_draft", func() { worker.Run(ctx) })
	safego(ctx, "reply_draft_queue_refresher", func() {
		runQueueDepthRefresher(ctx, "reply_draft", repo.QueueDepthByTenant, func(d map[string]int64) {
			metrics.RefreshQueueDepth(metrics.ReplyDraftQueueDepth, d)
		})
	})
}

// startBackgroundWorkers starts all background workers and returns the batch
// job worker (caller must defer Stop).
func startBackgroundWorkers(
	ctx context.Context,
	pool *pgxpool.Pool,
	enricher *enrich.Enricher,
	rawLLM *llmrouter.Router,
	llm llmclient.LLMClient,
	feedbackRepo *feedback.FeedbackRepo,
	consoleBaseURL string,
	gdprExportTTL time.Duration,
) *batchjob.Worker {
	startEmbeddingWorker(ctx, pool, enricher, rawLLM, llm)
	startReplyDraftWorker(ctx, pool, enricher, llm)
	startDigestWorker(ctx, pool, llm, consoleBaseURL)
	startGDPRExportWorker(ctx, pool, gdprExportTTL)

	worker := batchjob.New(
		feedbackjobrepo.New(pool),
		feedbackRepo,
		batchjob.Config{},
	)
	worker.Start(ctx)
	return worker
}

func startGDPRExportWorker(ctx context.Context, pool *pgxpool.Pool, exportTTL time.Duration) {
	repo := gdprrepo.New(pool)
	worker := gdprsvc.NewWorker(repo, repo, auditlogsvc.New(auditlogrepo.New(pool)), gdprsvc.WithWorkerExportTTL(exportTTL))
	safego(ctx, "gdpr_export", func() { worker.Run(ctx) })
}

// startDigestWorker wires the daily digest scheduler + worker (#27). llm is the
// audit-wrapping client. When embeddings are available, the cluster namer uses
// HDBSCAN for theme extraction; otherwise falls back to naive LLM grouping.
func startDigestWorker(ctx context.Context, pool *pgxpool.Pool, llm llmclient.LLMClient, consoleBaseURL string) {
	embedRepo := embeddingrepo.NewTaskRepo(pool)
	agg := digestsvc.NewClusterAggregator(embedRepo, feedback.NewFeedback(pool), embedRepo, llm)
	worker := digestsvc.NewWorker(
		digestsubrepo.New(pool),
		digestrunrepo.New(pool),
		agg,
		notifytarget.NewNotifyTarget(pool),
		embedRepo,
		notify.NewTransport(nil, notify.DefaultRetry()),
		consoleBaseURL,
	)
	safego(ctx, "digest", func() { worker.Run(ctx) })
}

// runQueueDepthRefresher feeds a per-tenant queue-depth gauge on a 30s tick — a
// gauge is otherwise stuck at its last value (or never set at all). query
// returns the outstanding counts per tenant; apply pushes them into the gauge.
// Shared by the reply-draft and embedding outboxes.
func runQueueDepthRefresher(
	ctx context.Context,
	name string,
	query func(context.Context) (map[string]int64, error),
	apply func(map[string]int64),
) {
	refresh := func() {
		depths, err := query(ctx)
		if err != nil {
			logext.Warnf(ctx, "[main.runQueueDepthRefresher] %s failed,err:%+v", name, err.Error())
			return
		}
		apply(depths)
	}
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	refresh()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			refresh()
		}
	}
}

// setupDatabase opens the pgx pool, verifies connectivity, and applies
// migrations. The caller owns the returned pool (defer pool.Close()).
func setupDatabase(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	const where = "main.setupDatabase"
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("pgxpool: %w", err)
	}
	if err := retryDatabasePing(ctx, databaseConnectTimeout, databaseConnectRetryInterval, pool.Ping); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pg ping: %w", err)
	}
	logext.Infof(ctx, "[%s] postgres connected", where)
	// Destructive-data guard before applying 015_drop_lark.sql — see
	// docs/proposals/2026/06/2026-06-08-channel-agnostic-inbound.md
	// §Data migrations.
	if err := database.ConfirmLarkDelete(ctx, pool, cfg.Migrations.ConfirmLarkDelete); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrations preflight: %w", err)
	}
	if err := database.RunMigrations(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return pool, nil
}

func retryDatabasePing(
	ctx context.Context,
	timeout time.Duration,
	interval time.Duration,
	ping func(context.Context) error,
) error {
	if timeout <= 0 {
		return ping(ctx)
	}
	if interval <= 0 {
		interval = timeout
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	attempt := 0
	var lastErr error
	for {
		attempt++
		err := ping(deadlineCtx)
		if err == nil {
			return nil
		}
		lastErr = err
		if deadlineCtx.Err() != nil {
			return lastErr
		}
		logDatabasePingRetry(deadlineCtx, attempt, timeout, interval, lastErr)
		timer := time.NewTimer(interval)
		select {
		case <-deadlineCtx.Done():
			timer.Stop()
			return lastErr
		case <-timer.C:
		}
	}
}

func logDatabasePingRetry(ctx context.Context, attempt int, timeout, interval time.Duration, err error) {
	const where = "main.retryDatabasePing"
	if attempt != 1 && attempt%5 != 0 {
		return
	}
	maxAttempts := int(math.Ceil(float64(timeout) / float64(interval)))
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	logext.Warnf(
		ctx,
		"[%s] postgres unavailable,attempt:%d/%d,retry_in:%s,err:%+v",
		where,
		attempt,
		maxAttempts,
		interval,
		err.Error(),
	)
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

// inboundWiring bundles the inbound-framework deps runServer needs to
// thread into buildRouter + into its shutdown defer. Extracted from
// runServer in #66 Plan T24 so the boot function stays under the §1
// CCN/NLOC threshold.
type inboundWiring struct {
	subRouter *chi.Mux
	secrets   *secretstore.TinkStore
	sources   *inboundsourcerepo.Repo
	adminRepo *admin.Repo
	manager   *inbound.Manager
}

// shutdown drains in-flight inbound work with a bounded timeout. Called
// from runServer's defer; we keep a separate method so the close
// timeout + log call stay encapsulated.
func (w inboundWiring) shutdown() {
	const where = "main.inboundWiring.shutdown"
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := w.manager.ShutdownAll(shutdownCtx); err != nil {
		logext.Warnf(shutdownCtx, "[%s] inbound shutdown failed,err:%+v", where, err.Error())
	}
}

// setupInbound wires the #66 channel-agnostic inbound framework:
// validates the master key, builds the AES-GCM secret store, opens the
// inbound_sources repo, runs first-start admin bootstrap, and starts
// every registered adapter on a fresh chi sub-router. The caller mounts
// the returned sub-router at /v1/inbound and calls w.shutdown() in a
// defer.
func setupInbound(
	ctx context.Context,
	pool *pgxpool.Pool,
	ingestor *ingest.Ingestor,
	secrets *secretstore.TinkStore,
	bootstrap config.BootstrapAdminConfig,
	consoleEnabled bool,
) (inboundWiring, error) {
	const where = "main.setupInbound"
	sources := inboundsourcerepo.NewRepo(pool)
	adminRepo := admin.NewRepo(pool)
	if consoleEnabled {
		if err := console.BootstrapAdmin(ctx, adminRepo, console.BootstrapConfig{
			Email:    bootstrap.Email,
			Password: bootstrap.Password,
		}); err != nil {
			return inboundWiring{}, fmt.Errorf("bootstrap admin: %w", err)
		}
	}

	// Adapters mount their channel-relative routes onto subRouter during
	// Manager.StartAll; buildRouter then mounts the populated mux under
	// /v1/inbound.
	subRouter := chi.NewRouter()
	deps := inbound.Deps{
		// `chi.Router` already satisfies `inbound.Mux` (single Method
		// method) — no wrapper needed (#66 review M-6). The old ChiMux
		// adapter struct was deleted.
		Mux: subRouter,
		Ingest: inbound.IngestFunc(func(ctx context.Context, tenantID string, keyID uuid.UUID, in domain.IngestInput) (int64, error) {
			return ingestor.IngestRow(ctx, tenantID, keyID, in)
		}),
		Sources: sources,
		Secrets: secrets,
		Metrics: inbound.NewPrometheusMetrics(metrics.Registry),
	}
	manager := inbound.NewManager(deps)
	if err := manager.StartAll(ctx); err != nil {
		return inboundWiring{}, fmt.Errorf("inbound manager: %w", err)
	}
	logext.Infof(ctx, "[%s] inbound framework ready,adapters:%d", where, len(inbound.Factories()))
	return inboundWiring{
		subRouter: subRouter,
		secrets:   secrets,
		sources:   sources,
		adminRepo: adminRepo,
		manager:   manager,
	}, nil
}
