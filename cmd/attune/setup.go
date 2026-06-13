package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/handlers/console"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/repo/admin"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	embeddingrepo "github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/repo/feedback"
	guardpolicyrepo "github.com/Phixsura/attune/internal/repo/guardpolicy"
	inboundsourcerepo "github.com/Phixsura/attune/internal/repo/inboundsource"
	llmauditrepo "github.com/Phixsura/attune/internal/repo/llmaudit"
	llmconfigrepo "github.com/Phixsura/attune/internal/repo/llmconfig"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/apikey"
	"github.com/Phixsura/attune/internal/service/enrich"
	guardpolicysvc "github.com/Phixsura/attune/internal/service/guardpolicy"
	llmconfigsvc "github.com/Phixsura/attune/internal/service/llmconfig"
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

// buildNotifier composes the active outbound chain.
//
// Post-#66 Plan T17: the inline notifier path is gone — raw-webhook
// destinations were the only other channel and they deliver through
// the outbox worker reading tenant_notify_targets directly. Kept as a
// function (instead of inlining the nil) so a future outbound adapter
// SDK (#34) can re-introduce inline channels without touching every
// call site.
func buildNotifier(
	ctx context.Context,
	_ *config.Config,
	_ *notifytarget.NotifyTargetRepo,
) (notify.Notifier, error) {
	const where = "main.buildNotifier"
	logext.Infof(ctx, "[%s] no inline notifiers wired (raw-webhook delivers via outbox; #34 will re-add)", where)
	return nil, nil
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
	me := console.NewMeHandler(signer, tenantRepo, userRepo, adminRepo)
	apiKeys := console.NewAPIKeysHandler(apiKeySvc)
	notifyTargets := console.NewNotifyTargetsHandler(notifyTargetRepo)
	feedback := console.NewFeedbackHandler(feedbackRepo, tenantRepo)
	usage := console.NewUsageHandler(feedbackRepo, llmauditrepo.New(pool))
	enrichConfig := console.NewEnrichConfigHandler(enrich.NewConfigService(tenantRepo))
	guardPolicies := console.NewGuardPolicyHandler(guardpolicysvc.NewService(guardpolicyrepo.New(pool)))
	inboundHandler := console.NewInboundHandler(sourceRepo, pool, secrets, cfg.ConsoleBaseURL)
	llmConfig := console.NewLLMConfigHandler(
		llmconfigsvc.NewService(llmconfigrepo.New(pool), secrets),
	)
	clustersHandler := console.NewClustersHandler(embeddingrepo.NewTaskRepo(pool))

	return console.NewRouter(
		signer, authHandler, changePasswordHandler, me, apiKeys, notifyTargets, feedback, usage,
		enrichConfig, guardPolicies, inboundHandler, llmConfig, clustersHandler, adminRepo,
	).Mount(), nil
}
