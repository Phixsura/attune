package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/handlers/console"
	"github.com/Phixsura/attune/internal/handlers/console/feedbackjob"
	consoleoidc "github.com/Phixsura/attune/internal/handlers/console/oidc"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/crypto"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/repo/admin"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	digestsubrepo "github.com/Phixsura/attune/internal/repo/digestsubscription"
	embeddingrepo "github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/repo/feedback"
	feedbackauditrepo "github.com/Phixsura/attune/internal/repo/feedbackaudit"
	feedbackjobrepo "github.com/Phixsura/attune/internal/repo/feedbackjob"
	feedbacktagrepo "github.com/Phixsura/attune/internal/repo/feedbacktag"
	feedbacktagassignmentrepo "github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
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
	"github.com/Phixsura/attune/internal/service/enrich"
	"github.com/Phixsura/attune/internal/service/feedbackbatch"
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
		if err := targets.Upsert(ctx, notifytarget.NotifyTarget{
			TenantID:        tenantID,
			DestinationType: notifytarget.DestRawWebhook,
			Audience:        audience,
			URL:             d.URL,
			Secret:          d.Secret,
			TimeoutSeconds:  timeout,
			Disabled:        d.Disabled,
		}); err != nil {
			return fmt.Errorf("custom_webhooks[%d]: upsert: %w", i, err)
		}
		logext.Infof(ctx, "[%s] custom webhook synced,tenant_slug:%s,audience:%s,disabled:%t",
			where, d.TenantSlug, audience, d.Disabled)
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
	age, err := outbox.OldestPendingAge(ctx)
	if err != nil {
		logext.Warnf(ctx, "[%s] failed,err:%+v", where, err.Error())
		return
	}
	metrics.OutboxLagSeconds.Set(age.Seconds())
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
	apiKeySvc := apikey.NewAPIKeys(apikeyrepo.NewAPIKey(pool))
	notifyTargetRepo := notifytarget.NewNotifyTarget(pool)
	feedbackRepo := feedback.NewFeedback(pool)

	authHandler := console.NewAuthHandler(signer, adminRepo, tenantRepo, cfg.ConsoleBaseURL)
	changePasswordHandler := console.NewChangePasswordHandler(adminRepo, signer)
	oidcUserRepo := oidcuserrepo.NewRepo(pool)
	me := console.NewMeHandler(signer, tenantRepo, userRepo, adminRepo, oidcUserRepo)
	apiKeys := console.NewAPIKeysHandler(apiKeySvc)
	notifyTargets := console.NewNotifyTargetsHandler(notifyTargetRepo)
	feedback := console.NewFeedbackHandler(feedbackRepo, tenantRepo)
	feedback.SetDrafter(replydraftsvc.NewReplyDrafter(replydraftrepo.NewDraftTaskRepo(pool), llm))
	// Per-tenant backstop on the synchronous Regenerate endpoint: generous
	// enough never to bother a human triaging (60/min, burst 20), tight enough
	// to bound a scripted loop's LLM spend on top of the per-row cooldown.
	feedback.SetRegenLimiter(ratelimit.New(60, 20, false, nil))
	usage := console.NewUsageHandler(feedbackRepo, llmauditrepo.New(pool))
	enrichConfig := console.NewEnrichConfigHandler(enrich.NewConfigService(tenantRepo))
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
	llmConfigRepo := llmconfigrepo.New(pool)
	searchRouter := llmrouter.New(llmConfigRepo, secrets)
	searchRateLimiter := ratelimit.NewMemorySlidingLimiter()
	searchCache := semanticsearch.NewPGCache(pool)
	searchSvc := semanticsearch.New(feedbackRepo, searchRouter, searchRateLimiter, searchCache)
	searchHandler := console.NewSearchHandler(searchSvc)

	// Job handler uses batch service (implements jobService interface).
	jobHandler := feedbackjob.NewHandler(batchSvc)

	// Tenant member repo for RBAC (#38).
	memberRepo := tenantmember.NewRepo(pool)

	return console.NewRouter(
		signer, authHandler, changePasswordHandler, me, apiKeys, notifyTargets, feedback,
		batchHandler,
		searchHandler,
		jobHandler,
		usage, enrichConfig, guardPolicies, inboundHandler, llmConfig, clustersHandler, digestSub,
		tagHandler, tagAssignmentHandler, workflowHandler, oidcHandler, adminRepo, memberRepo,
	).Mount(), nil
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
