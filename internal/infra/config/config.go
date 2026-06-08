// Package config loads the attune YAML config and exposes a typed
// Config to the rest of the service.
package config

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	defaultPath = "config.yaml"
	envPath     = "FEEDBACK_API_CONFIG"
)

type Config struct {
	Port             int           `yaml:"port"`
	DatabaseURL      string        `yaml:"database_url"`
	EnricherInterval time.Duration `yaml:"-"`
	EnricherBatch    int           `yaml:"enricher_batch"`

	// LLMProtocol selects which backend wire format to speak (#10).
	// See llm_protocol.go for the typed constants —
	// LLMProtocolOpenAICompat (default) covers OpenAI / Azure / vLLM /
	// ollama / oneapi via Chat Completions; LLMProtocolOpenAIResponses
	// hits /v1/responses via the official SDK; LLMProtocolAnthropic
	// uses Messages with forced tool-use for structured output;
	// LLMProtocolGemini uses Google generative-language.
	//
	// LLMOpenAIBaseURL / LLMOpenAIAPIKey are reused across protocols as
	// "the LLM endpoint URL / bearer key" — the field name is historical
	// and the docs explain the multi-protocol meaning.
	LLMProtocol      LLMProtocol `yaml:"llm_protocol"`
	LLMOpenAIBaseURL string      `yaml:"llm_openai_base_url"`
	LLMOpenAIAPIKey  string      `yaml:"llm_openai_api_key"`
	// LLMModel overrides the hardcoded enrichment model id. Empty falls
	// back to DefaultLLMModel — the operator only sets this when
	// pointing at a gateway that aliases (or hosts) a non-default model.
	LLMModel string `yaml:"llm_model"`

	// Per-tenant custom HTTPS webhooks. Sync'd into tenant_notify_targets
	// at startup; the console takes over CRUD afterwards. The tenant slug
	// must already exist in the tenants table.
	CustomWebhooks []CustomWebhookDest `yaml:"custom_webhooks"`

	// Console config. ConsoleSessionKey must be ≥ 32 random bytes
	// (HMACs the session / CSRF tokens). ConsoleBaseURL is the
	// origin used for safe-redirect rendering (e.g.
	// https://attune.example.com).
	ConsoleSessionKey string `yaml:"console_session_key"`
	ConsoleBaseURL    string `yaml:"console_base_url"`

	// Per-tenant token-bucket rate limit guarding ingest.
	// RateLimitPerMinute sustained req/min (default 60)
	// RateLimitBurst bucket capacity (default 300)
	// RateLimitDisabled true → bypass everything (test/migration only)
	RateLimitPerMinute int  `yaml:"rate_limit_per_minute"`
	RateLimitBurst     int  `yaml:"rate_limit_burst"`
	RateLimitDisabled  bool `yaml:"rate_limit_disabled"`
}

type yamlConfig struct {
	Port               int                 `yaml:"port"`
	DatabaseURL        string              `yaml:"database_url"`
	LLMProtocol        string              `yaml:"llm_protocol"`
	LLMOpenAIBaseURL   string              `yaml:"llm_openai_base_url"`
	LLMOpenAIAPIKey    string              `yaml:"llm_openai_api_key"`
	LLMModel           string              `yaml:"llm_model"`
	EnricherInterval   string              `yaml:"enricher_interval"`
	EnricherBatch      int                 `yaml:"enricher_batch"`
	CustomWebhooks     []CustomWebhookDest `yaml:"custom_webhooks"`
	ConsoleSessionKey  string              `yaml:"console_session_key"`
	ConsoleBaseURL     string              `yaml:"console_base_url"`
	RateLimitPerMinute int                 `yaml:"rate_limit_per_minute"`
	RateLimitBurst     int                 `yaml:"rate_limit_burst"`
	RateLimitDisabled  bool                `yaml:"rate_limit_disabled"`
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
	if err := applyEnvOverrides(&yc); err != nil { // ptrext:allow out-param
		logext.Errorf(ctx, "[%s] env overrides failed,err:%+v", where, err.Error())
		return nil, err
	}
	c := ptrext.Of(Config{
		Port:               yc.Port,
		DatabaseURL:        yc.DatabaseURL,
		LLMProtocol:        LLMProtocol(yc.LLMProtocol),
		LLMOpenAIBaseURL:   yc.LLMOpenAIBaseURL,
		LLMOpenAIAPIKey:    yc.LLMOpenAIAPIKey,
		LLMModel:           yc.LLMModel,
		EnricherBatch:      yc.EnricherBatch,
		CustomWebhooks:     yc.CustomWebhooks,
		ConsoleSessionKey:  yc.ConsoleSessionKey,
		ConsoleBaseURL:     yc.ConsoleBaseURL,
		RateLimitPerMinute: yc.RateLimitPerMinute,
		RateLimitBurst:     yc.RateLimitBurst,
		RateLimitDisabled:  yc.RateLimitDisabled,
	})
	// defaults — extracted to keep Load's CCN ≤ 15 (§1).
	c.applyDefaults()

	if yc.EnricherInterval == "" {
		c.EnricherInterval = DefaultEnricherInterval
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
	logext.Infof(ctx, "[%s] OK,port:%d,console_enabled:%t",
		where, c.Port, c.ConsoleSessionKey != "")
	return c, nil
}

// applyEnvOverrides lives in env.go to keep config.go scannable.

// applyDefaults fills zero-valued fields with the defaults declared in
// defaults.go. Extracted from Load to keep its CCN ≤ 15 (§1 quality
// gates); kept thin (only zero checks + assignment) so the rationale
// for each value lives next to its `const` rather than inlined here.
func (c *Config) applyDefaults() {
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.LLMProtocol == "" {
		c.LLMProtocol = DefaultLLMProtocol
	}
	if c.LLMOpenAIBaseURL == "" && c.LLMProtocol == DefaultLLMProtocol {
		c.LLMOpenAIBaseURL = DefaultLLMOpenAIBaseURL
	}
	if c.LLMModel == "" {
		c.LLMModel = DefaultLLMModel
	}
	if c.EnricherBatch == 0 {
		c.EnricherBatch = DefaultEnricherBatch
	}
	if c.RateLimitPerMinute == 0 {
		c.RateLimitPerMinute = DefaultRateLimitPerMinute
	}
	if c.RateLimitBurst == 0 {
		c.RateLimitBurst = DefaultRateLimitBurst
	}
}

func (c *Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("config: database_url is required")
	}
	if !c.LLMProtocol.IsValid() {
		known := KnownLLMProtocols()
		names := make([]string, len(known))
		for i, k := range known {
			names[i] = string(k)
		}
		return fmt.Errorf(
			"config: llm_protocol %q is not one of %s",
			c.LLMProtocol, strings.Join(names, " / "),
		)
	}
	// openai-compat needs an explicit base URL (Azure / vLLM / ollama / oneapi
	// have no SDK default). The three SDK-backed protocols fall back to the
	// vendor's default host when LLMOpenAIBaseURL is empty.
	if c.LLMProtocol == LLMProtocolOpenAICompat && c.LLMOpenAIBaseURL == "" {
		return fmt.Errorf("config: llm_openai_base_url is required when llm_protocol=%s", LLMProtocolOpenAICompat)
	}
	// The three SDK-backed protocols refuse an empty bearer key in their
	// constructors; surface that as a config error instead of a runtime
	// failure on the first enrich call.
	if c.LLMProtocol != LLMProtocolOpenAICompat && c.LLMOpenAIAPIKey == "" {
		return fmt.Errorf("config: llm_openai_api_key is required when llm_protocol=%s",
			c.LLMProtocol)
	}
	return c.validateCustomWebhooks()
}
