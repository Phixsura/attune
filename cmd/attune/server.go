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
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/admin"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/guardpolicy"
	inboundsourcerepo "github.com/Phixsura/attune/internal/repo/inboundsource"
	llmauditrepo "github.com/Phixsura/attune/internal/repo/llmaudit"
	llmconfigrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/apikey"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	digestsvc "github.com/Phixsura/attune/internal/service/digest"
	embeddingsvc "github.com/Phixsura/attune/internal/service/embedding"
	"github.com/Phixsura/attune/internal/service/enrich"
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
	llmConfigRepo := llmconfigrepo.New(pool)
	llmConfig := llmconfigsvc.NewService(llmConfigRepo, secrets)
	if err := llmConfig.SyncKeyRegistry(ctx); err != nil {
		return fmt.Errorf("sync secret key registry: %w", err)
	}
	rawLLM := llmrouter.New(llmConfigRepo, secrets)
	guardedLLM := llmguard.NewClient(rawLLM, guardpolicy.New(pool))
	llm := llmauditsvc.NewClient(guardedLLM, llmauditrepo.New(pool))
	defer llm.Close()
	logext.Infof(ctx, "[%s] llm router ready,primary_secret_key:%s", where, secrets.PrimaryKeyID())

	feedbackRepo := feedback.NewFeedback(pool)
	apikeyRepo := apikeyrepo.NewAPIKey(pool)
	tenantRepo := tenant.NewTenant(pool)
	notifyTargetRepo := notifytarget.NewNotifyTarget(pool)
	outboxRepo := outboxrepo.NewOutbox(pool)
	enricher := enrich.NewEnricher(feedbackRepo, llm, "")
	ingestor := ingest.NewIngestor(feedbackRepo, enricher)
	apiKeys := apikey.NewAPIKeys(apikeyRepo)

	if err := syncCustomWebhooks(ctx, cfg.CustomWebhooks, tenantRepo, notifyTargetRepo); err != nil {
		return fmt.Errorf("sync custom webhooks: %w", err)
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
	go runAuditPruner(ctx, auditlogsvc.New(auditlogrepo.New(pool)), cfg.AuditRetention, cfg.AuditPruneInterval)

	batchJobWorker := startBackgroundWorkers(ctx, pool, enricher, rawLLM, llm, feedbackRepo, cfg.ConsoleBaseURL)
	defer batchJobWorker.Stop()

	ingestHandler := handlers.NewIngestHandler(ingestor)

	inb, err := setupInbound(ctx, pool, ingestor, secrets, cfg.Console.BootstrapAdmin, cfg.ConsoleSessionKey != "")
	if err != nil {
		return err
	}
	defer inb.shutdown()

	r, err := buildRouter(
		ctx, cfg, ingestHandler, apiKeys, pool, llm,
		inb.subRouter, inb.secrets, inb.sources, inb.adminRepo,
	)
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
	go worker.Run(ctx)
	go runQueueDepthRefresher(ctx, "embed", taskRepo.QueueDepthByTenant, func(d map[string]int64) {
		metrics.RefreshQueueDepth(metrics.EmbedQueueDepth, d)
	})
}

// startReplyDraftWorker wires the reply-draft outbox + worker. llm is the
// audit-wrapping client so each draft call lands in llm_audit
// (purpose='reply_draft').
func startReplyDraftWorker(ctx context.Context, pool *pgxpool.Pool, enricher *enrich.Enricher, llm llmclient.LLMClient) {
	repo := replydraftrepo.NewDraftTaskRepo(pool)
	enricher.SetDraftTask(repo)
	worker := replydraftsvc.NewWorker(repo, llm)
	go worker.Run(ctx)
	go runQueueDepthRefresher(ctx, "reply_draft", repo.QueueDepthByTenant, func(d map[string]int64) {
		metrics.RefreshQueueDepth(metrics.ReplyDraftQueueDepth, d)
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
) *batchjob.Worker {
	startEmbeddingWorker(ctx, pool, enricher, rawLLM, llm)
	startReplyDraftWorker(ctx, pool, enricher, llm)
	startDigestWorker(ctx, pool, llm, consoleBaseURL)

	worker := batchjob.New(
		feedbackjobrepo.New(pool),
		feedbackRepo,
		batchjob.Config{},
	)
	worker.Start(ctx)
	return worker
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
	go worker.Run(ctx)
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
	if err := pool.Ping(ctx); err != nil {
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
