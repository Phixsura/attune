package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/handlers/console"
	"github.com/Phixsura/attune/internal/handlers/console/enrichconfig"
	consoleenrichmentruntime "github.com/Phixsura/attune/internal/handlers/console/enrichmentruntime"
	"github.com/Phixsura/attune/internal/handlers/console/feedbackjob"
	consolegdpr "github.com/Phixsura/attune/internal/handlers/console/gdpr"
	consoleoidc "github.com/Phixsura/attune/internal/handlers/console/oidc"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/crypto"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/admin"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	digestsubrepo "github.com/Phixsura/attune/internal/repo/digestsubscription"
	embeddingrepo "github.com/Phixsura/attune/internal/repo/embedding"
	enrichruntime "github.com/Phixsura/attune/internal/repo/enrichmentruntime"
	"github.com/Phixsura/attune/internal/repo/feedback"
	feedbackauditrepo "github.com/Phixsura/attune/internal/repo/feedbackaudit"
	feedbackjobrepo "github.com/Phixsura/attune/internal/repo/feedbackjob"
	feedbacktagrepo "github.com/Phixsura/attune/internal/repo/feedbacktag"
	feedbacktagassignmentrepo "github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
	gdprrepo "github.com/Phixsura/attune/internal/repo/gdpr"
	guardpolicyrepo "github.com/Phixsura/attune/internal/repo/guardpolicy"
	idempotencyrepo "github.com/Phixsura/attune/internal/repo/idempotency"
	inboundsourcerepo "github.com/Phixsura/attune/internal/repo/inboundsource"
	llmauditrepo "github.com/Phixsura/attune/internal/repo/llmaudit"
	llmconfigrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	oidcuserrepo "github.com/Phixsura/attune/internal/repo/oidcuser"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/repo/tenantmember"
	workflowstaterepo "github.com/Phixsura/attune/internal/repo/workflowstate"
	"github.com/Phixsura/attune/internal/service/apikey"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	"github.com/Phixsura/attune/internal/service/enrich"
	enrichruntimesvc "github.com/Phixsura/attune/internal/service/enrichruntime"
	evalsvc "github.com/Phixsura/attune/internal/service/eval"
	"github.com/Phixsura/attune/internal/service/feedbackbatch"
	gdprsvc "github.com/Phixsura/attune/internal/service/gdpr"
	guardpolicysvc "github.com/Phixsura/attune/internal/service/guardpolicy"
	llmconfigsvc "github.com/Phixsura/attune/internal/service/llmconfig"
	"github.com/Phixsura/attune/internal/service/llmrouter"
	"github.com/Phixsura/attune/internal/service/oidcauth"
	replydraftsvc "github.com/Phixsura/attune/internal/service/replydraft"
	"github.com/Phixsura/attune/internal/service/semanticsearch"
	workflowsvc "github.com/Phixsura/attune/internal/service/workflow"
)

// syncCustomWebhooks upserts every entry in cfg.CustomWebhooks into
// tenant_notify_targets. Slug is resolved against the tenants table;
// unknown slugs abort startup so misconfigurations don't ship silently.
//
// : this is the only writer to tenant_notify_targets. a follow-up
// adds a console UI but the same upsert semantics still apply.
func syncCustomWebhooks(
	ctx context.Context,
	dests []config.CustomWebhookDest,
	tenants *tenant.TenantRepo,
	targets *notifytarget.NotifyTargetRepo,
) error {
	const where = "main.syncCustomWebhooks"
	if len(dests) == 0 {
		logext.Infof(ctx, "[%s] OK,no custom webhooks", where)
		return nil
	}
	logext.Infof(ctx, "[%s] start,count:%d", where, len(dests))
	for i, d := range dests {
		tenantID, err := tenants.ResolveSlug(ctx, d.TenantSlug)
		if errors.Is(err, tenant.ErrTenantNotFound) {
			logext.Errorf(ctx, "[%s] reject: tenant slug not found,idx:%d,slug:%s",
				where, i, d.TenantSlug)
			return fmt.Errorf("custom_webhooks[%d]: tenant slug %q not found in tenants table",
				i, d.TenantSlug)
		}
		if err != nil {
			logext.Errorf(ctx, "[%s] ResolveSlug failed,idx:%d,slug:%s,err:%+v",
				where, i, d.TenantSlug, err.Error())
			return fmt.Errorf("custom_webhooks[%d]: resolve slug %q: %w",
				i, d.TenantSlug, err)
		}
		audience := d.Audience
		if audience == "" {
			audience = notifytarget.AudiencePool
		}
		timeout := d.TimeoutSeconds
		if timeout <= 0 {
			timeout = 10
		}
		destType := d.DestinationType
		if destType == "" {
			destType = notifytarget.DestRawWebhook
		}
		if err := targets.Upsert(ctx, notifytarget.NotifyTarget{
			TenantID:        tenantID,
			DestinationType: destType,
			Audience:        audience,
			URL:             d.URL,
			Secret:          d.Secret,
			TimeoutSeconds:  timeout,
			Disabled:        d.Disabled,
		}); err != nil {
			return fmt.Errorf("custom_webhooks[%d]: upsert: %w", i, err)
		}
		logext.Infof(ctx, "[%s] custom webhook synced,tenant_slug:%s,dest_type:%s,audience:%s,disabled:%t",
			where, d.TenantSlug, destType, audience, d.Disabled)
	}
	return nil
}

// runOutboxLagRefresher ticks every 30s and updates the
// attune_outbox_lag_seconds gauge. Decoupling refresh from Prometheus
// scrape avoids one DB roundtrip per scrape (could be 1/sec for tight
// alerting setups) and keeps the gauge meaningful even when scrape
// frequency varies.
func runOutboxLagRefresher(ctx context.Context, outbox *outboxrepo.OutboxRepo) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	// Refresh once at startup so the gauge isn't stuck at 0 until the
	// first tick after the service starts.
	refreshOutboxLag(ctx, outbox)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			refreshOutboxLag(ctx, outbox)
		}
	}
}

func refreshOutboxLag(ctx context.Context, outbox *outboxrepo.OutboxRepo) {
	const where = "main.refreshOutboxLag"
	if age, err := outbox.OldestPendingAge(ctx); err != nil {
		logext.Warnf(ctx, "[%s] lag failed,err:%+v", where, err.Error())
	} else {
		metrics.OutboxLagSeconds.Set(age.Seconds())
	}
	if dead, err := outbox.DeadCount(ctx); err != nil {
		logext.Warnf(ctx, "[%s] dead count failed,err:%+v", where, err.Error())
	} else {
		metrics.OutboxDeadRows.Set(float64(dead))
	}
}

// buildConsoleRouter wires the Console (auth + /me + /logout + resource
// endpoints + #66 inbound source management). Pre-#66 the console relied
// on an external OAuth + dev-login backdoor; both are gone, replaced by
// the local-admin password flow (auth.Handler + admin.Repo + bootstrap config).
//
// Console boots when ConsoleSessionKey is set and ConsoleBaseURL is
// non-empty. No more dev-login / insecure-cookies escape hatches.
func buildConsoleRouter(
	cfg *config.Config,
	pool *pgxpool.Pool,
	secrets *secretstore.TinkStore,
	sourceRepo *inboundsourcerepo.Repo,
	adminRepo *admin.Repo,
	llm llmclient.LLMClient,
	enrichRuntime *enrichruntimesvc.Service,
) (chi.Router, error) {
	if cfg.ConsoleBaseURL == "" {
		return nil, fmt.Errorf("console requires console.base_url")
	}
	signer, err := console.NewSigner(cfg.ConsoleSessionKey)
	if err != nil {
		return nil, err
	}

	tenantRepo := tenant.NewTenant(pool)
	userRepo := tenant.NewTenantUserRepo(pool)
	auditLogSvc := auditlogsvc.New(auditlogrepo.New(pool))
	apiKeySvc := apikey.NewAPIKeys(apikeyrepo.NewAPIKey(pool))
	notifyTargetRepo := notifytarget.NewNotifyTarget(pool)
	feedbackRepo := feedback.NewFeedback(pool)

	authHandler := console.NewAuthHandler(signer, adminRepo, tenantRepo, tenantmember.NewRepo(pool), cfg.ConsoleBaseURL)
	changePasswordHandler := console.NewChangePasswordHandler(adminRepo, signer)
	oidcUserRepo := oidcuserrepo.NewRepo(pool)
	me := console.NewMeHandler(signer, tenantRepo, userRepo, adminRepo, oidcUserRepo)
	auditLog := console.NewAuditLogHandler(auditLogSvc)
	apiKeys := console.NewAPIKeysHandler(apiKeySvc)
	notifyTargets := console.NewNotifyTargetsHandler(notifyTargetRepo)
	feedback := console.NewFeedbackHandler(feedbackRepo, tenantRepo)
	feedback.SetDrafter(replydraftsvc.NewReplyDrafter(replydraftrepo.NewDraftTaskRepo(pool), llm))
	// Per-tenant backstop on the synchronous Regenerate endpoint: generous
	// enough never to bother a human triaging (60/min, burst 20), tight enough
	// to bound a scripted loop's LLM spend on top of the per-row cooldown.
	feedback.SetRegenLimiter(ratelimit.New(60, 20, false, nil))
	usage := console.NewUsageHandler(feedbackRepo, llmauditrepo.New(pool))
	gdprHandler := buildGDPRHandler(cfg, pool, auditLogSvc, signer, adminRepo)
	enrichConfig := buildEnrichConfigHandler(tenantRepo, feedbackRepo, llm)
	var enrichmentRuntimeHandler *consoleenrichmentruntime.Handler
	if enrichRuntime != nil {
		enrichmentRuntimeHandler = console.NewEnrichmentRuntimeHandler(enrichRuntime, cfg.GDPRStepUpTTL)
	}
	guardPolicies := console.NewGuardPolicyHandler(guardpolicysvc.NewService(guardpolicyrepo.New(pool)))
	inboundHandler := console.NewInboundHandler(sourceRepo, pool, secrets, cfg.ConsoleBaseURL)
	llmConfig := console.NewLLMConfigHandler(
		llmconfigsvc.NewService(llmconfigrepo.New(pool), secrets),
	)
	clustersHandler := console.NewClustersHandler(embeddingrepo.NewTaskRepo(pool))
	digestSub := console.NewDigestSubscriptionHandler(digestsubrepo.New(pool), tenantRepo)
	tagRepo := feedbacktagrepo.New(pool)
	tagAssignmentRepo := feedbacktagassignmentrepo.New(pool)
	feedback.SetTagAssignments(tagAssignmentRepo)
	wfStateRepo := workflowstaterepo.New(pool)
	wfAuditRepo := feedbackauditrepo.New(pool)
	wfSvc := workflowsvc.NewService(wfStateRepo, wfAuditRepo, pool)
	feedback.SetWorkflow(wfSvc)
	feedback.SetAuditReader(wfAuditRepo)
	feedback.SetWorkflowStates(wfStateRepo)
	tagHandler := console.NewTagHandler(tagRepo)
	tagAssignmentHandler := console.NewTagAssignmentHandler(tagRepo, tagAssignmentRepo)
	workflowHandler := console.NewWorkflowHandler(wfStateRepo, wfSvc)

	// OIDC SSO handler (optional, only when OIDC is configured).
	oidcHandler := buildOIDCHandler(context.Background(), cfg, pool, signer, tenantRepo)

	// Batch operations service dependencies.
	idempotencyRepo := idempotencyrepo.New(pool)
	jobRepo := feedbackjobrepo.New(pool)
	batchRateLimiter := ratelimit.NewMemorySlidingLimiter()
	batchConcurrency := ratelimit.NewMemoryConcurrencyLimiter()
	batchSvc := feedbackbatch.New(feedbackRepo, idempotencyRepo, jobRepo, batchRateLimiter, batchConcurrency)
	batchHandler := console.NewBatchHandler(batchSvc)

	// Semantic search service dependencies.
	searchRouter := llmrouter.New(llmconfigrepo.New(pool), secrets)
	searchSvc := semanticsearch.New(feedbackRepo, searchRouter,
		ratelimit.NewMemorySlidingLimiter(), semanticsearch.NewPGCache(pool))
	searchHandler := console.NewSearchHandler(searchSvc)

	// Job handler uses batch service (implements jobService interface).
	jobHandler := feedbackjob.NewHandler(batchSvc)

	// Tenant member repo and handler for RBAC (#38).
	memberRepo := tenantmember.NewRepo(pool)
	memberHandler := console.NewMemberHandler(memberRepo)
	apiKeys.SetAuditLogger(auditLogSvc)
	notifyTargets.SetAuditLogger(auditLogSvc)
	inboundHandler.SetAuditLogger(auditLogSvc)
	memberHandler.SetAuditLogger(auditLogSvc)
	guardPolicies.SetAuditLogger(auditLogSvc)
	enrichConfig.SetAuditLogger(auditLogSvc)
	if enrichmentRuntimeHandler != nil {
		enrichmentRuntimeHandler.SetAuditLogger(auditLogSvc)
	}
	jobHandler.SetAuditLogger(auditLogSvc)
	llmConfig.SetAuditLogger(auditLogSvc)
	workflowHandler.SetAuditLogger(auditLogSvc)
	batchHandler.SetAuditLogger(auditLogSvc)
	digestSub.SetAuditLogger(auditLogSvc)
	tagHandler.SetAuditLogger(auditLogSvc)

	router := console.NewRouter(
		signer, authHandler, changePasswordHandler, me, auditLog, apiKeys, notifyTargets, feedback,
		batchHandler,
		searchHandler,
		jobHandler,
		gdprHandler,
		usage, enrichConfig, enrichmentRuntimeHandler, guardPolicies, inboundHandler, llmConfig, clustersHandler, digestSub,
		tagHandler, tagAssignmentHandler, workflowHandler, oidcHandler, memberHandler, adminRepo, memberRepo,
	)
	attachOutboxHandler(router, pool, auditLogSvc)
	return router.Mount(), nil
}

// attachOutboxHandler wires the notify dead-queue console handler (#33). Kept
// out of buildConsoleRouter (and off the already-large NewRouter signature) via
// a setter.
func attachOutboxHandler(router *console.Router, pool *pgxpool.Pool, audit *auditlogsvc.Service) {
	h := console.NewOutboxHandler(outboxrepo.NewOutbox(pool))
	h.SetAuditLogger(audit)
	router.SetOutboxHandler(h)
}

func buildGDPRHandler(
	cfg *config.Config,
	pool *pgxpool.Pool,
	auditLogSvc *auditlogsvc.Service,
	signer *console.Signer,
	adminRepo *admin.Repo,
) *consolegdpr.Handler {
	return console.NewGDPRHandler(
		gdprsvc.New(
			gdprrepo.New(pool),
			auditLogSvc,
			gdprsvc.WithAuditRetention(cfg.Audit.RetentionDays, cfg.AuditPruneInterval),
			gdprsvc.WithExportTTL(cfg.GDPRExportTTL),
			gdprsvc.WithStepUpTTL(cfg.GDPRStepUpTTL),
			gdprsvc.WithDeleteGraceWindow(cfg.GDPRDeleteGraceWindow),
		),
		signer,
		adminRepo,
		cfg.GDPRStepUpTTL,
	)
}

// buildOIDCHandler creates the OIDC handler if OIDC is enabled, otherwise returns nil.
func buildOIDCHandler(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	signer *console.Signer,
	tenants oidcauth.TenantResolver,
) *consoleoidc.Handler {
	if !cfg.OIDC.Enabled {
		return nil
	}

	oidcUsers := oidcuserrepo.NewRepo(pool)
	oidcSvc, err := oidcauth.NewService(ctx, &cfg.OIDC, oidcUsers, tenants) // ptrext:allow struct-field
	if err != nil {
		logext.Errorf(ctx, "[buildOIDCHandler] OIDC service init failed,err:%s", err.Error())
		return nil
	}
	if oidcSvc == nil {
		return nil
	}

	// OIDC state cookie encryption uses the first 32 bytes of the session key.
	aeadKey := []byte(cfg.ConsoleSessionKey)
	if len(aeadKey) > 32 {
		aeadKey = aeadKey[:32]
	}
	aead, err := crypto.NewAEAD(aeadKey)
	if err != nil {
		logext.Errorf(ctx, "[buildOIDCHandler] AEAD init failed,err:%s", err.Error())
		return nil
	}

	return consoleoidc.NewHandler(oidcSvc, signer, aead, cfg.ConsoleBaseURL)
}

func enrichmentRuntimeBootstrapSpec(cfg *config.Config) enrichruntime.Spec {
	return enrichruntime.Spec{
		QueueLen:            int32(cfg.EnricherQueueLen),
		Workers:             int32(cfg.EnricherWorkers),
		BatchSize:           int32(cfg.EnricherBatch),
		BatchWindow:         cfg.EnricherBatchWindow,
		SweepInterval:       cfg.EnricherInterval,
		LLMRateLimitEnabled: cfg.EnricherLLMMaxQPS > 0,
		LLMMaxQPS:           cfg.EnricherLLMMaxQPS,
		LLMBurst:            int32(cfg.EnricherLLMBurst),
	}
}

func enrichmentRuntimeBootstrapVersion(cfg *config.Config) string {
	return fmt.Sprintf(
		"enricher:%d:%d:%d:%s:%s:%g:%d",
		cfg.EnricherQueueLen,
		cfg.EnricherWorkers,
		cfg.EnricherBatch,
		cfg.EnricherBatchWindow,
		cfg.EnricherInterval,
		cfg.EnricherLLMMaxQPS,
		cfg.EnricherLLMBurst,
	)
}

// buildEnrichConfigHandler wires the enrich-config console handler with its
// eval-suggestions getter and a per-tenant rate limit (6/min, burst 2) — each
// eval-suggestions call runs an LLM eval, so a scripted caller must be capped.
func buildEnrichConfigHandler(
	tenantRepo *tenant.TenantRepo,
	feedbackRepo *feedback.FeedbackRepo,
	llm llmclient.LLMClient,
) *enrichconfig.Handler {
	h := console.NewEnrichConfigHandler(enrich.NewConfigService(tenantRepo))
	h.SetEvalGetter(buildEvalSuggestionsGetter(feedbackRepo, tenantRepo, llm))
	h.SetEvalLimiter(ratelimit.New(6, 2, false, nil))
	return h
}

const (
	// evalSuggestionsWindow is how far back the Console eval-suggestions
	// endpoint samples enriched rows.
	evalSuggestionsWindow = 30 * 24 * time.Hour
	// evalSuggestionsSample is the per-call sample size (each sampled row is
	// re-classified by the LLM).
	evalSuggestionsSample = 20
	// evalSuggestionsTimeout bounds the whole eval. Each sampled row makes one
	// LLM call (bounded only by the channel timeout), so without a ceiling a
	// slow/hung provider could pin a request for many minutes. On deadline the
	// remaining classify calls fail fast and the loop returns what it has.
	evalSuggestionsTimeout = 60 * time.Second
)

// buildEvalSuggestionsGetter builds an enrichconfig.EvalSuggestionsGetter that
// runs quick consistency evals via the console API (#83).
func buildEvalSuggestionsGetter(
	feedbackRepo *feedback.FeedbackRepo,
	tenantRepo *tenant.TenantRepo,
	llm llmclient.LLMClient,
) enrichconfig.EvalSuggestionsGetter {
	evalEnricher := enrich.NewEnricher(feedbackRepo, llm, "")
	evaluator := evalsvc.NewEvaluator(feedbackRepo, tenantRepo, evalEnricher)
	return func(ctx context.Context, tenantID string) (*enrichconfig.SuggestedAttrsReport, error) {
		ctx, cancel := context.WithTimeout(ctx, evalSuggestionsTimeout)
		defer cancel()
		since := time.Now().Add(-evalSuggestionsWindow)
		report, err := evaluator.RunConsistencyForTenant(ctx, tenantID, since, evalSuggestionsSample)
		if err != nil {
			return nil, err
		}
		return evalReportToEnrichconfig(report.SuggestedAttrs), nil
	}
}

func evalReportToEnrichconfig(sa evalsvc.SuggestedAttrsReport) *enrichconfig.SuggestedAttrsReport {
	out := ptrext.Of(enrichconfig.SuggestedAttrsReport{
		Coverage: sa.Coverage,
	})
	for _, c := range sa.Candidates {
		out.Candidates = append(out.Candidates, enrichconfig.SuggestedCandidate{
			Dim:            c.Dim,
			Value:          c.Value,
			Count:          c.Count,
			Confidence:     c.Confidence,
			CoverageImpact: c.CoverageImpact,
		})
	}
	for _, r := range sa.Recommendations {
		out.Recommendations = append(out.Recommendations, enrichconfig.SuggestedRecommendation{
			Action: r.Action,
			Dim:    r.Dim,
			Value:  r.Value,
			Reason: r.Reason,
			Impact: r.Impact,
		})
	}
	return out
}
