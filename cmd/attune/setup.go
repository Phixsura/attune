package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/handlers/console"
	"github.com/Phixsura/attune/internal/infra/config"
	larkclient "github.com/Phixsura/attune/internal/infra/lark"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/notify"
	"github.com/Phixsura/attune/internal/notify/adapter/larkwebhook"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/lark"
	"github.com/Phixsura/attune/internal/repo/notifytarget"
	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/service/apikey"
	"github.com/Phixsura/attune/internal/service/enrich"
)

// buildLLMClient picks an LLM backend from cfg.LLMProtocol. The four
// supported protocols (#10):
//
// - "openai-compat" hand-rolled /v1/chat/completions client; covers
// OpenAI / Azure / vLLM / ollama / oneapi.
// - "openai-responses" openai-go/v3 client.Responses.New.
// - "anthropic" anthropic-sdk-go with forced tool_use.
// - "gemini" google.golang.org/genai responseJsonSchema.
//
// config.validate() enforces protocol legality and required URL/key for
// each, so this function trusts cfg to be coherent.
//
// Users who need another provider implement llmclient.LLMClient and
// wire it here — there is no plugin registry on purpose.
func buildLLMClient(cfg *config.Config) (llmclient.LLMClient, error) {
	switch cfg.LLMProtocol {
	case "openai-responses":
		return llmclient.NewOpenAIResponses(cfg.LLMOpenAIBaseURL, cfg.LLMOpenAIAPIKey)
	case "anthropic":
		return llmclient.NewAnthropic(cfg.LLMOpenAIBaseURL, cfg.LLMOpenAIAPIKey)
	case "gemini":
		return llmclient.NewGemini(cfg.LLMOpenAIBaseURL, cfg.LLMOpenAIAPIKey)
	default: // "openai-compat" — config.validate() already accepted the value
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
		slog.InfoContext(ctx, "no custom webhooks configured")
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
		slog.InfoContext(ctx, "custom webhook synced",
			"tenant_slug", d.TenantSlug, "audience", audience, "disabled", d.Disabled)
	}
	return nil
}

// buildNotifier composes the active outbound chain. returns
// either a single LarkWebhook, a MultiNotifier (Lark + Raw), or nil
// (everything disabled). enricher must tolerate nil — Lark / Raw both
// disabled is a valid dev configuration.
//
// a follow-up will load Lark per-tenant from the same notify_targets table,
// at which point this function becomes "build MultiNotifier from
// notify_targets".
func buildNotifier(
	ctx context.Context,
	cfg *config.Config,
	_ *notifytarget.NotifyTargetRepo,
) (notify.Notifier, error) {
	// Design contract (v0.4 §3.6): raw-webhook delivers via outbox ONLY
	// (at-least-once via DB queue + worker). Lark group bot delivers
	// inline through this Notifier (best-effort, NoRetry — duplicate
	// cards spam chats). If we put RawWebhookRouter here too we'd
	// double-push: once inline + once from outbox worker.
	//
	// Outcome: only Lark goes through the inline notifier. raw-webhook
	// destinations are picked up by the outbox worker reading
	// tenant_notify_targets directly.
	if !cfg.NotifyEnabled() {
		slog.InfoContext(ctx, "no inline notifiers wired (lark webhook URLs empty)")
		return nil, nil
	}
	// Local name avoids shadowing the imported `lark` package
	// (internal/repo/lark — see callers of lark.NewLarkInstallRepo).
	larkBot := larkwebhook.NewLarkWebhook(
		cfg.FeedbackPoolWebhookURL, cfg.FeedbackPoolWebhookSecret,
		cfg.DevRadarWebhookURL, cfg.DevRadarWebhookSecret,
	)
	slog.InfoContext(ctx, "lark webhook wired",
		"pool", larkBot.PoolEnabled(), "radar", larkBot.RadarEnabled())
	return larkBot, nil
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
	age, err := outbox.OldestPendingAge(ctx)
	if err != nil {
		slog.WarnContext(ctx, "outbox lag refresh failed", "err", err)
		return
	}
	metrics.OutboxLagSeconds.Set(age.Seconds())
}

// buildConsoleRouter wires the Console (auth + OAuth + /me +
// /logout for now; resource endpoints land in subsequent commits). Keeps
// console wiring isolated from the main ingest path so the legacy API
// continues to boot even if console config is incomplete.
//
// Two HTTP-only escape hatches activate together when their config flags
// are set, and ONLY together:
// - ConsoleInsecureCookies=true → Secure cookie flag dropped
// - ConsoleDevLogin=true → /install/dev-login backdoor mounted
//
// They MUST be off in any real TLS-fronted deployment. The combined check
// here makes "accidentally enable just one" impossible.
func buildConsoleRouter(cfg *config.Config, pool *pgxpool.Pool) (chi.Router, error) {
	ctx := context.Background()
	if cfg.LarkAppID == "" || cfg.LarkAppSecret == "" {
		return nil, fmt.Errorf("console requires lark_app_id + lark_app_secret")
	}
	if cfg.ConsoleBaseURL == "" {
		return nil, fmt.Errorf("console requires console_base_url")
	}
	if cfg.ConsoleDevLogin && !cfg.ConsoleInsecureCookies {
		return nil, fmt.Errorf(
			"console: dev_login requires insecure_cookies (TLS would lose the test session cookie)",
		)
	}
	signer, err := console.NewSigner(cfg.ConsoleSessionKey, cfg.ConsoleInsecureCookies)
	if err != nil {
		return nil, err
	}
	if cfg.ConsoleInsecureCookies {
		slog.WarnContext(ctx, "console: INSECURE cookies enabled — for HTTP testing only")
	}

	larkClient := larkclient.New(cfg.LarkAppID, cfg.LarkAppSecret)
	tenantRepo := tenant.NewTenant(pool)
	userRepo := tenant.NewTenantUserRepo(pool)
	installRepo := lark.NewLarkInstallRepo(pool)
	apiKeySvc := apikey.NewAPIKeys(apikeyrepo.NewAPIKey(pool))
	notifyTargetRepo := notifytarget.NewNotifyTarget(pool)
	feedbackRepo := feedback.NewFeedback(pool)

	oauth := console.NewOAuthHandler(signer, larkClient, tenantRepo, userRepo, installRepo,
		cfg.LarkAppID, cfg.ConsoleBaseURL)
	me := console.NewMeHandler(signer, tenantRepo, userRepo)
	apiKeys := console.NewAPIKeysHandler(apiKeySvc)
	notifyTargets := console.NewNotifyTargetsHandler(notifyTargetRepo)
	feedback := console.NewFeedbackHandler(feedbackRepo, tenantRepo)
	usage := console.NewUsageHandler(feedbackRepo)
	enrichConfig := console.NewEnrichConfigHandler(enrich.NewConfigService(tenantRepo))

	var devLogin http.Handler
	if cfg.ConsoleDevLogin {
		slog.WarnContext(ctx, "console: dev-login BACKDOOR enabled at /fb/v1/console/install/dev-login")
		devLogin = console.NewDevLoginHandler(signer, tenantRepo, userRepo, cfg.ConsoleBaseURL)
	}
	return console.NewRouter(
		signer, oauth, me, apiKeys, notifyTargets, feedback, usage, enrichConfig, devLogin,
	).Mount(), nil
}
