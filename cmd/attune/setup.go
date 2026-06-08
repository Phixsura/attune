package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/handlers/console"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/repo/admin"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/feedback"
	inboundsourcerepo "github.com/Phixsura/attune/internal/repo/inboundsource"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/apikey"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// buildLLMClient picks an LLM backend from cfg.LLMProtocol (#10):
//
//   - LLMProtocolOpenAICompat hand-rolled /v1/chat/completions; covers
//     OpenAI / Azure / vLLM / ollama / oneapi.
//   - LLMProtocolOpenAIResponses openai-go/v3 client.Responses.New.
//   - LLMProtocolAnthropic anthropic-sdk-go with forced tool_use.
//   - LLMProtocolGemini google.golang.org/genai responseJsonSchema.
//
// config.validate() enforces protocol legality and required URL/key for
// each, so this function trusts cfg to be coherent.
//
// Adding a new backend is three edits: an entry in config.KnownLLMProtocols,
// a constant in config/llm_protocol.go, and a case here. There is no
// plugin registry on purpose.
func buildLLMClient(cfg *config.Config) (llmclient.LLMClient, error) {
	switch cfg.LLMProtocol {
	case config.LLMProtocolOpenAIResponses:
		return llmclient.NewOpenAIResponses(cfg.LLMOpenAIBaseURL, cfg.LLMOpenAIAPIKey)
	case config.LLMProtocolAnthropic:
		return llmclient.NewAnthropic(cfg.LLMOpenAIBaseURL, cfg.LLMOpenAIAPIKey)
	case config.LLMProtocolGemini:
		return llmclient.NewGemini(cfg.LLMOpenAIBaseURL, cfg.LLMOpenAIAPIKey)
	default: // LLMProtocolOpenAICompat — config.validate() already accepted the value
		return llmclient.NewOpenAICompat(cfg.LLMOpenAIBaseURL, cfg.LLMOpenAIAPIKey)
	}
}

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
// Post-#66 Plan T17 (Lark removal): the inline notifier path is gone —
// raw-webhook destinations were the only other channel and they deliver
// through the outbox worker reading tenant_notify_targets directly.
// Kept as a function (instead of inlining the nil) so a future outbound
// adapter SDK (#34) can re-introduce inline channels without touching
// every call site.
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
// on Lark OAuth + dev-login backdoor; both are gone, replaced by the
// local-admin password flow (auth.Handler + admin.Repo + bootstrap env).
//
// Console boots when ConsoleSessionKey is set and ConsoleBaseURL is
// non-empty. No more dev-login / insecure-cookies escape hatches.
func buildConsoleRouter(
	cfg *config.Config,
	pool *pgxpool.Pool,
	secrets inbound.SecretStore,
	sourceRepo *inboundsourcerepo.Repo,
	adminRepo *admin.Repo,
) (chi.Router, error) {
	if cfg.ConsoleBaseURL == "" {
		return nil, fmt.Errorf("console requires console_base_url")
	}
	signer, err := console.NewSigner(cfg.ConsoleSessionKey, false)
	if err != nil {
		return nil, err
	}

	tenantRepo := tenant.NewTenant(pool)
	userRepo := tenant.NewTenantUserRepo(pool)
	apiKeySvc := apikey.NewAPIKeys(apikeyrepo.NewAPIKey(pool))
	notifyTargetRepo := notifytarget.NewNotifyTarget(pool)
	feedbackRepo := feedback.NewFeedback(pool)

	authHandler := console.NewAuthHandler(signer, adminRepo, cfg.ConsoleBaseURL)
	me := console.NewMeHandler(signer, tenantRepo, userRepo)
	apiKeys := console.NewAPIKeysHandler(apiKeySvc)
	notifyTargets := console.NewNotifyTargetsHandler(notifyTargetRepo)
	feedback := console.NewFeedbackHandler(feedbackRepo, tenantRepo)
	usage := console.NewUsageHandler(feedbackRepo)
	enrichConfig := console.NewEnrichConfigHandler(enrich.NewConfigService(tenantRepo))
	inboundHandler := console.NewInboundHandler(sourceRepo, pool, secrets, cfg.ConsoleBaseURL)

	return console.NewRouter(
		signer, authHandler, me, apiKeys, notifyTargets, feedback, usage,
		enrichConfig, inboundHandler,
	).Mount(), nil
}
