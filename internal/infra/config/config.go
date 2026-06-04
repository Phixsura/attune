// Package config loads the listen YAML config and exposes a typed
// Config to the rest of the service.
package config

import (
	"context"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/wanmuchengchuan/listen/internal/logext"
)

const defaultPath = "config.yaml"
const envPath = "FEEDBACK_API_CONFIG"

type Config struct {
	Port             int           `yaml:"port"`
	DatabaseURL      string        `yaml:"database_url"`
	EnricherInterval time.Duration `yaml:"-"`
	EnricherBatch    int           `yaml:"enricher_batch"`

	// LLM endpoint — any OpenAI-compatible /v1/chat/completions backend.
	// Works with OpenAI / Azure OpenAI / vllm / ollama / oneapi.
	LLMOpenAIBaseURL string `yaml:"llm_openai_base_url"`
	LLMOpenAIAPIKey  string `yaml:"llm_openai_api_key"`

	LarkSigningSecret     string `yaml:"lark_signing_secret"`
	LarkVerificationToken string `yaml:"lark_verification_token"`
	LarkDefaultTenantSlug string `yaml:"lark_default_tenant_slug"`

	// Lark group bot outbound. Empty URLs disable the destination —
	// rows still land in Postgres normally. SINGLE-TENANT (dogfood);
	// per-tenant Lark wiring waits for OAuth flow.
	FeedbackPoolWebhookURL    string `yaml:"feedback_pool_webhook_url"`
	FeedbackPoolWebhookSecret string `yaml:"feedback_pool_webhook_secret"`
	DevRadarWebhookURL        string `yaml:"dev_radar_webhook_url"`
	DevRadarWebhookSecret     string `yaml:"dev_radar_webhook_secret"`

	// Per-tenant custom HTTPS webhooks. Sync'd into tenant_notify_targets
	// at startup; the console takes over CRUD afterwards. The tenant slug
	// must already exist in the tenants table.
	CustomWebhooks []CustomWebhookDest `yaml:"custom_webhooks"`

	// Console config. LarkAppID/Secret are used for OAuth +
	// app_access_token calls. ConsoleSessionKey must be ≥ 32 random bytes
	// (HMACs the session / CSRF tokens). ConsoleBaseURL is the Lark OAuth
	// callback redirect_uri origin (e.g. https://listen.example.com).
	LarkAppID         string `yaml:"lark_app_id"`
	LarkAppSecret     string `yaml:"lark_app_secret"`
	ConsoleSessionKey string `yaml:"console_session_key"`
	ConsoleBaseURL    string `yaml:"console_base_url"`

	// Per-tenant token-bucket rate limit guarding ingest.
	//   RateLimitPerMinute  sustained req/min (default 60)
	//   RateLimitBurst      bucket capacity (default 300)
	//   RateLimitDisabled   true → bypass everything (test/migration only)
	RateLimitPerMinute int  `yaml:"rate_limit_per_minute"`
	RateLimitBurst     int  `yaml:"rate_limit_burst"`
	RateLimitDisabled  bool `yaml:"rate_limit_disabled"`

	// ConsoleInsecureCookies drops the `Secure` cookie flag — required
	// only when serving console over plain HTTP. Never enable under TLS.
	ConsoleInsecureCookies bool `yaml:"console_insecure_cookies"`

	// ConsoleDevLogin enables GET /fb/v1/console/install/dev-login —
	// a backdoor that mints a session without Lark OAuth. Test loops
	// only. Logs WARN on every use. Never on in production.
	ConsoleDevLogin bool `yaml:"console_dev_login"`
}

type yamlConfig struct {
	Port                      int                 `yaml:"port"`
	DatabaseURL               string              `yaml:"database_url"`
	LLMOpenAIBaseURL          string              `yaml:"llm_openai_base_url"`
	LLMOpenAIAPIKey           string              `yaml:"llm_openai_api_key"`
	EnricherInterval          string              `yaml:"enricher_interval"`
	EnricherBatch             int                 `yaml:"enricher_batch"`
	LarkSigningSecret         string              `yaml:"lark_signing_secret"`
	LarkVerificationToken     string              `yaml:"lark_verification_token"`
	LarkDefaultTenantSlug     string              `yaml:"lark_default_tenant_slug"`
	FeedbackPoolWebhookURL    string              `yaml:"feedback_pool_webhook_url"`
	FeedbackPoolWebhookSecret string              `yaml:"feedback_pool_webhook_secret"`
	DevRadarWebhookURL        string              `yaml:"dev_radar_webhook_url"`
	DevRadarWebhookSecret     string              `yaml:"dev_radar_webhook_secret"`
	CustomWebhooks            []CustomWebhookDest `yaml:"custom_webhooks"`
	LarkAppID                 string              `yaml:"lark_app_id"`
	LarkAppSecret             string              `yaml:"lark_app_secret"`
	ConsoleSessionKey         string              `yaml:"console_session_key"`
	ConsoleBaseURL            string              `yaml:"console_base_url"`
	ConsoleInsecureCookies    bool                `yaml:"console_insecure_cookies"`
	ConsoleDevLogin           bool                `yaml:"console_dev_login"`
	RateLimitPerMinute        int                 `yaml:"rate_limit_per_minute"`
	RateLimitBurst            int                 `yaml:"rate_limit_burst"`
	RateLimitDisabled         bool                `yaml:"rate_limit_disabled"`
}

// Load reads the YAML config from FEEDBACK_API_CONFIG (or ./config.yaml)
// and validates required fields. A missing config file is OK if every
// required field is supplied via env vars.
//
// Env var overrides (always win over YAML) — see env.go for the full list.
func Load() (*Config, error) {
	const where = "config.Load"
	ctx := context.Background()
	var yc yamlConfig
	path := os.Getenv(envPath)
	if path == "" {
		path = defaultPath
	}
	if raw, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(raw, &yc); err != nil {
			logext.Errorf(ctx, "[%s] parse yaml failed,path:%s,err:%+v",
				where, path, err.Error())
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		logext.Errorf(ctx, "[%s] read file failed,path:%s,err:%+v", where, path, err.Error())
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := applyEnvOverrides(&yc); err != nil {
		logext.Errorf(ctx, "[%s] env overrides failed,err:%+v", where, err.Error())
		return nil, err
	}
	c := &Config{
		Port:                      yc.Port,
		DatabaseURL:               yc.DatabaseURL,
		LLMOpenAIBaseURL:          yc.LLMOpenAIBaseURL,
		LLMOpenAIAPIKey:           yc.LLMOpenAIAPIKey,
		EnricherBatch:             yc.EnricherBatch,
		LarkSigningSecret:         yc.LarkSigningSecret,
		LarkVerificationToken:     yc.LarkVerificationToken,
		LarkDefaultTenantSlug:     yc.LarkDefaultTenantSlug,
		FeedbackPoolWebhookURL:    yc.FeedbackPoolWebhookURL,
		FeedbackPoolWebhookSecret: yc.FeedbackPoolWebhookSecret,
		DevRadarWebhookURL:        yc.DevRadarWebhookURL,
		DevRadarWebhookSecret:     yc.DevRadarWebhookSecret,
		CustomWebhooks:            yc.CustomWebhooks,
		LarkAppID:                 yc.LarkAppID,
		LarkAppSecret:             yc.LarkAppSecret,
		ConsoleSessionKey:         yc.ConsoleSessionKey,
		ConsoleBaseURL:            yc.ConsoleBaseURL,
		ConsoleInsecureCookies:    yc.ConsoleInsecureCookies,
		ConsoleDevLogin:           yc.ConsoleDevLogin,
		RateLimitPerMinute:        yc.RateLimitPerMinute,
		RateLimitBurst:            yc.RateLimitBurst,
		RateLimitDisabled:         yc.RateLimitDisabled,
	}
	if c.Port == 0 {
		c.Port = 8090
	}
	if c.LLMOpenAIBaseURL == "" {
		c.LLMOpenAIBaseURL = "https://api.openai.com"
	}
	if c.EnricherBatch == 0 {
		c.EnricherBatch = 10
	}
	if c.RateLimitPerMinute == 0 {
		c.RateLimitPerMinute = 60
	}
	if c.RateLimitBurst == 0 {
		c.RateLimitBurst = 300
	}
	if yc.EnricherInterval == "" {
		c.EnricherInterval = 30 * time.Second
	} else {
		d, err := time.ParseDuration(yc.EnricherInterval)
		if err != nil {
			return nil, fmt.Errorf("enricher_interval: %w", err)
		}
		c.EnricherInterval = d
	}
	if err := c.validate(); err != nil {
		logext.Errorf(ctx, "[%s] validate failed,err:%+v", where, err.Error())
		return nil, err
	}
	logext.Infof(ctx, "[%s] OK,port:%d,console_enabled:%t,lark_enabled:%t",
		where, c.Port, c.ConsoleSessionKey != "", c.LarkEnabled())
	return c, nil
}

// applyEnvOverrides lives in env.go to keep config.go scannable.

func (c *Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("config: database_url is required")
	}
	if c.LLMOpenAIBaseURL == "" {
		return fmt.Errorf("config: llm_openai_base_url is required")
	}
	if c.LarkSigningSecret != "" && c.LarkDefaultTenantSlug == "" {
		return fmt.Errorf("config: lark_default_tenant_slug is required when lark_signing_secret is set")
	}
	return c.validateCustomWebhooks()
}

// LarkEnabled reports whether the Lark webhook handler should accept events.
func (c *Config) LarkEnabled() bool { return c.LarkSigningSecret != "" }

// NotifyEnabled reports whether at least one outbound webhook is wired.
func (c *Config) NotifyEnabled() bool {
	return c.FeedbackPoolWebhookURL != "" || c.DevRadarWebhookURL != ""
}
