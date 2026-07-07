package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console"
	consolecustomerrequest "github.com/Phixsura/attune/internal/handlers/console/customerrequest"
	"github.com/Phixsura/attune/internal/handlers/console/enrichconfig"
	consoleenrichmentruntime "github.com/Phixsura/attune/internal/handlers/console/enrichmentruntime"
	consolefeedback "github.com/Phixsura/attune/internal/handlers/console/feedback"
	"github.com/Phixsura/attune/internal/handlers/console/feedbackjob"
	consolegdpr "github.com/Phixsura/attune/internal/handlers/console/gdpr"
	consoleoidc "github.com/Phixsura/attune/internal/handlers/console/oidc"
	"github.com/Phixsura/attune/internal/handlers/console/system"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/crypto"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/admin"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	auditevidencerepo "github.com/Phixsura/attune/internal/repo/auditevidence"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	auditlogviewrepo "github.com/Phixsura/attune/internal/repo/auditlogview"
	breakglassrepo "github.com/Phixsura/attune/internal/repo/breakglass"
	customerrequestrepo "github.com/Phixsura/attune/internal/repo/customerrequest"
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
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	oidcuserrepo "github.com/Phixsura/attune/internal/repo/oidcuser"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
	systemsettingsrepo "github.com/Phixsura/attune/internal/repo/systemsettings"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/repo/tenantmember"
	workflowstaterepo "github.com/Phixsura/attune/internal/repo/workflowstate"
	"github.com/Phixsura/attune/internal/service/apikey"
	auditevidencesvc "github.com/Phixsura/attune/internal/service/auditevidence"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	auditlogviewsvc "github.com/Phixsura/attune/internal/service/auditlogview"
	authmodesvc "github.com/Phixsura/attune/internal/service/authmode"
	breakglasssvc "github.com/Phixsura/attune/internal/service/breakglass"
	customerrequestsvc "github.com/Phixsura/attune/internal/service/customerrequest"
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
	"github.com/Phixsura/attune/internal/service/securityalert"
	"github.com/Phixsura/attune/internal/service/semanticsearch"
	workflowsvc "github.com/Phixsura/attune/internal/service/workflow"

	"github.com/Phixsura/attune/internal/preflight"
	preflightchecks "github.com/Phixsura/attune/internal/preflight/checks"
)

// syncCustomWebhooks upserts every entry in cfg.CustomWebhooks into
// tenant_notify_targets. Slug is resolved against the tenants table;
// unknown slugs abort startup so misconfigurations don't ship silently.
//
// This remains the startup config writer for tenant_notify_targets; Console
// integrations use their own handlers and repository paths.
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
// non-empty.
func buildConsoleRouter(
	cfg *config.Config,
	pool *pgxpool.Pool,
	secrets *secretstore.TinkStore,
	sourceRepo *inboundsourcerepo.Repo,
	adminRepo *admin.Repo,
	llm llmclient.LLMClient,
	enrichRuntime *enrichruntimesvc.Service,
	sources domain.SourceSet,
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
	settingsRepo := systemsettingsrepo.NewRepo(pool)
	auditLogSvc := auditlogsvc.New(auditlogrepo.New(pool))
	auditViewSvc := auditlogviewsvc.New(auditlogviewrepo.New(settingsRepo))
	apiKeySvc := apikey.NewAPIKeys(apikeyrepo.NewAPIKey(pool))
	notifyTargetRepo := notifytarget.NewNotifyTarget(pool)
	feedbackRepo := feedback.NewFeedback(pool)

	authHandler := console.NewAuthHandler(signer, adminRepo, tenantRepo, tenantmember.NewRepo(pool), cfg.ConsoleBaseURL)
	changePasswordHandler := console.NewChangePasswordHandler(adminRepo, signer)
	oidcUserRepo := oidcuserrepo.NewRepo(pool)
	me := console.NewMeHandler(signer, tenantRepo, userRepo, adminRepo, oidcUserRepo)
	auditLog := console.NewAuditLogHandler(auditLogSvc)
	auditLog.SetSavedViewService(auditViewSvc)
	apiKeys := console.NewAPIKeysHandler(apiKeySvc)
	notifyTargets := console.NewNotifyTargetsHandler(notifyTargetRepo)
	feedback := console.NewFeedbackHandler(feedbackRepo, tenantRepo)
	replyDraftRepo := replydraftrepo.NewDraftTaskRepo(pool)
	feedback.SetDrafter(replydraftsvc.NewReplyDrafter(replyDraftRepo, llm))
	feedback.SetReplyDraftWorkflow(replydraftsvc.NewWorkflow(replyDraftRepo, secrets, nil))
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
	guardPolicies := console.NewGuardPolicyHandler(guardpolicysvc.NewService(guardpolicyrepo.New(pool), sources))
	inboundHandler := console.NewInboundHandler(sourceRepo, pool, secrets, cfg.ConsoleBaseURL)
	llmConfig := console.NewLLMConfigHandler(llmconfigsvc.NewService(llmconfigrepo.New(pool), secrets))
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
	customerRequestHandler := buildCustomerRequestHandler(pool, idempotencyRepo, auditLogSvc)
	jobRepo := feedbackjobrepo.New(pool)
	batchSvc, batchHandler := buildBatchHandler(feedbackRepo, idempotencyRepo, jobRepo)

	searchHandler := buildSearchHandler(pool, secrets, feedbackRepo, tagAssignmentRepo, wfStateRepo)

	// Job handler uses batch service (implements jobService interface).
	jobHandler := feedbackjob.NewHandler(batchSvc)

	memberRepo := tenantmember.NewRepo(pool) // RBAC (#38); passed to both NewMemberHandler and NewRouter
	memberHandler := console.NewMemberHandler(memberRepo)
	auditTargets := []consoleAuditTarget{
		func(audit *auditlogsvc.Service) { apiKeys.SetAuditLogger(audit) },
		func(audit *auditlogsvc.Service) { feedback.SetAuditLogger(audit) },
		func(audit *auditlogsvc.Service) { notifyTargets.SetAuditLogger(audit) },
		func(audit *auditlogsvc.Service) { inboundHandler.SetAuditLogger(audit) },
		func(audit *auditlogsvc.Service) { memberHandler.SetAuditLogger(audit) },
		func(audit *auditlogsvc.Service) { guardPolicies.SetAuditLogger(audit) },
		func(audit *auditlogsvc.Service) { enrichConfig.SetAuditLogger(audit) },
		func(audit *auditlogsvc.Service) { jobHandler.SetAuditLogger(audit) },
		func(audit *auditlogsvc.Service) { llmConfig.SetAuditLogger(audit) },
		func(audit *auditlogsvc.Service) { workflowHandler.SetAuditLogger(audit) },
		func(audit *auditlogsvc.Service) { batchHandler.SetAuditLogger(audit) },
		func(audit *auditlogsvc.Service) { digestSub.SetAuditLogger(audit) },
		func(audit *auditlogsvc.Service) { tagHandler.SetAuditLogger(audit) },
	}
	if enrichmentRuntimeHandler != nil {
		auditTargets = append(auditTargets, func(audit *auditlogsvc.Service) {
			enrichmentRuntimeHandler.SetAuditLogger(audit)
		})
	}
	wireConsoleAuditLoggers(auditTargets, auditLogSvc)

	router := console.NewRouter(
		signer, authHandler, changePasswordHandler, me, auditLog, apiKeys, notifyTargets, feedback,
		batchHandler, searchHandler, jobHandler, gdprHandler, usage, enrichConfig, enrichmentRuntimeHandler,
		guardPolicies, inboundHandler, llmConfig, clustersHandler, digestSub, tagHandler, tagAssignmentHandler,
		workflowHandler, oidcHandler, memberHandler, adminRepo, memberRepo,
	)
	router.SetCustomerRequestHandler(customerRequestHandler)
	return configureConsoleRouter(router, pool, cfg, settingsRepo, auditLogSvc, signer, tenantRepo, adminRepo, feedbackRepo), nil
}

type consoleAuditTarget func(*auditlogsvc.Service)

func wireConsoleAuditLoggers(targets []consoleAuditTarget, auditLogSvc *auditlogsvc.Service) {
	for _, target := range targets {
		target(auditLogSvc)
	}
}

func buildCustomerRequestHandler(pool *pgxpool.Pool, idempotencyRepo idempotencyrepo.Store, auditLogSvc *auditlogsvc.Service) *consolecustomerrequest.Handler {
	customerRequestRepo := customerrequestrepo.New(pool)
	return console.NewCustomerRequestHandler(customerrequestsvc.New(customerRequestRepo, idempotencyRepo, auditLogSvc))
}

func buildBatchHandler(feedbackRepo *feedback.FeedbackRepo, idempotencyRepo idempotencyrepo.Store, jobRepo feedbackjobrepo.Store) (feedbackbatch.Service, *consolefeedback.BatchHandler) {
	batchSvc := feedbackbatch.New(
		feedbackRepo,
		idempotencyRepo,
		jobRepo,
		ratelimit.NewMemorySlidingLimiter(),
		ratelimit.NewMemoryConcurrencyLimiter(),
	)
	return batchSvc, console.NewBatchHandler(batchSvc)
}

func configureConsoleRouter(
	router *console.Router,
	pool *pgxpool.Pool,
	cfg *config.Config,
	settingsRepo *systemsettingsrepo.Repo,
	auditLogSvc *auditlogsvc.Service,
	signer *console.Signer,
	tenantRepo *tenant.TenantRepo,
	adminRepo *admin.Repo,
	feedbackRepo *feedback.FeedbackRepo,
) chi.Router {
	router.SetQualityActionHandler(console.NewQualityActionHandler(feedbackRepo))
	attachOptionalHandlers(router, pool, cfg, settingsRepo, auditLogSvc, signer, tenantRepo, adminRepo)
	return router.Mount()
}

func buildSearchHandler(
	pool *pgxpool.Pool,
	secrets *secretstore.TinkStore,
	feedbackRepo *feedback.FeedbackRepo,
	tagAssignmentRepo *feedbacktagassignmentrepo.Repo,
	wfStateRepo *workflowstaterepo.Repo,
) *consolefeedback.SearchHandler {
	searchHandler := console.NewSearchHandler(semanticsearch.New(feedbackRepo,
		llmrouter.New(llmconfigrepo.New(pool), secrets),
		ratelimit.NewMemorySlidingLimiter(), semanticsearch.NewPGCache(pool)))
	searchHandler.SetSearchOperations(feedbackRepo)
	searchHandler.SetTagAssignments(tagAssignmentRepo)
	searchHandler.SetWorkflowStates(wfStateRepo)
	return searchHandler
}

func attachOptionalHandlers(router *console.Router, pool *pgxpool.Pool, cfg *config.Config, settingsRepo *systemsettingsrepo.Repo, auditLogSvc *auditlogsvc.Service, signer *console.Signer, tenantRepo *tenant.TenantRepo, adminRepo *admin.Repo) {
	attachOutboxHandler(router, pool, auditLogSvc)
	attachAuditEvidenceHandler(router, pool, cfg, auditLogSvc)
	attachMCPClientHandler(router, cfg, pool, auditLogSvc)
	attachPreflightHandler(router, cfg, pool)
	attachRecoveryHandler(router, pool)
	attachReleaseInfoHandler(router, cfg)
	attachSSOCutoverHandler(router, pool, cfg, settingsRepo, auditLogSvc, signer, tenantRepo, adminRepo)
}

// attachOutboxHandler wires the notify dead-queue console handler (#33). Kept
// out of buildConsoleRouter (and off the already-large NewRouter signature) via
// a setter.
func attachOutboxHandler(router *console.Router, pool *pgxpool.Pool, audit *auditlogsvc.Service) {
	h := console.NewOutboxHandler(outboxrepo.NewOutbox(pool))
	h.SetAuditLogger(audit)
	router.SetOutboxHandler(h)
}

func attachAuditEvidenceHandler(router *console.Router, pool *pgxpool.Pool, cfg *config.Config, audit *auditlogsvc.Service) {
	svc := auditevidencesvc.New(
		auditevidencerepo.New(pool),
		audit,
		audit,
		auditevidencesvc.WithExportTTL(cfg.AuditEvidenceExportTTL),
	)
	router.SetAuditEvidenceHandler(console.NewAuditEvidenceHandler(svc))
}

// attachPreflightHandler wires the production readiness preflight handler (#149).
func attachPreflightHandler(router *console.Router, cfg *config.Config, pool *pgxpool.Pool) {
	env := ptrext.Of(preflight.Environment{Cfg: cfg, Pool: pool})
	preflightchecks.SetMigrationTotal(database.MigrationCount())
	router.SetPreflightHandler(system.NewPreflightHandler(env))
}

// attachRecoveryHandler wires the restore-drill recovery status handler.
func attachRecoveryHandler(router *console.Router, pool *pgxpool.Pool) {
	router.SetRecoveryHandler(system.NewRecoveryHandler(pool))
}

// attachReleaseInfoHandler wires the Reliability release-context handler.
func attachReleaseInfoHandler(router *console.Router, cfg *config.Config) {
	router.SetReleaseInfoHandler(system.NewReleaseHandler(cfg))
}

// attachMCPClientHandler wires the MCP OAuth client console handler (#93).
func attachMCPClientHandler(router *console.Router, cfg *config.Config, pool *pgxpool.Pool, audit *auditlogsvc.Service) {
	h := console.NewMCPClientHandler(
		mcprepo.NewClients(pool),
		mcprepo.NewTokens(pool),
		mcprepo.NewSessions(pool),
		mcprepo.NewToolPolicies(pool),
	)
	h.SetAuditLogger(audit)
	h.SetConnectionProfile(cfg.MCPPublicBaseURL, cfg.MCP.OAuth.Issuer)
	router.SetMCPClientHandler(h)
}

// attachSSOCutoverHandler wires the SSO cutover and break-glass handlers (#158).
func attachSSOCutoverHandler(router *console.Router, pool *pgxpool.Pool, cfg *config.Config, settingsRepo *systemsettingsrepo.Repo, audit *auditlogsvc.Service, signer *console.Signer, tenantRepo *tenant.TenantRepo, adminRepo *admin.Repo) {
	bgRepo := breakglassrepo.NewRepo(pool)
	bgSvc := breakglasssvc.NewService(bgRepo, breakglasssvc.DefaultConfig())

	preflightEnv := ptrext.Of(preflight.Environment{Cfg: cfg, Pool: pool})
	authModeSvc := authmodesvc.NewService(
		settingsRepo,
		preflightRunnerFunc(preflight.RunChecks),
		preflightEnv,
		bgSvc.CountValid,
	)

	alertSvc := securityalert.NewService(cfg.Security.AlertWebhookURL)
	lockoutTracker := breakglasssvc.NewLockoutTracker(breakglasssvc.DefaultLockoutConfig())

	router.SetSSOCutoverHandler(console.NewSSOCutoverHandler(authModeSvc))
	router.SetBreakGlassAPIHandler(console.NewBreakGlassAPIHandler(bgSvc, audit, alertSvc))
	router.SetBreakGlassHandler(console.NewBreakGlassHandler(bgSvc, signer, tenantRepo, adminIDByEmailAdapter{adminRepo}, audit, alertSvc, lockoutTracker, cfg.ConsoleBaseURL))
}

// adminIDByEmailAdapter adapts admin.Repo to the adminIDByEmailResolver interface.
type adminIDByEmailAdapter struct {
	repo *admin.Repo
}

func (a adminIDByEmailAdapter) GetIDByEmail(ctx context.Context, email string) (string, error) {
	admin, err := a.repo.GetByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	return admin.ID, nil
}

// preflightRunnerFunc adapts a function to the authmode.PreflightRunner interface.
type preflightRunnerFunc func(ctx context.Context, env *preflight.Environment, names []string) preflight.Report

func (f preflightRunnerFunc) RunChecks(ctx context.Context, env *preflight.Environment, names []string) preflight.Report {
	return f(ctx, env, names)
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
