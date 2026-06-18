// SPDX-License-Identifier: Apache-2.0

// Package config loads the single attune YAML runtime config.
package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const defaultPath = "config.yaml"

var activePath = defaultPath

// SetPath sets the config path used by Load. Empty resets to the default.
func SetPath(path string) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		activePath = defaultPath
		return
	}
	activePath = trimmed
}

// Path returns the config path Load will read.
func Path() string {
	return activePath
}

type Config struct {
	Port int

	Database      DatabaseConfig
	Migrations    MigrationsConfig
	Enricher      EnricherConfig
	Audit         AuditConfig
	GDPR          GDPRConfig
	Console       ConsoleConfig
	Shutdown      ShutdownConfig
	Secrets       SecretsConfig
	Observability ObservabilityConfig
	RateLimit     RateLimitConfig
	OIDC          OIDCConfig
	Security      SecurityConfig

	CustomWebhooks []CustomWebhookDest

	// Convenience fields used by legacy wiring while the runtime contract is
	// config-first/nested.
	DatabaseURL           string
	EnricherInterval      time.Duration
	EnricherBatch         int
	EnricherQueueLen      int
	EnricherWorkers       int
	EnricherBatchWindow   time.Duration
	EnricherLLMMaxQPS     float64
	EnricherLLMBurst      int
	ConsoleSessionKey     string
	ConsoleBaseURL        string
	RateLimitPerMinute    int
	RateLimitBurst        int
	RateLimitDisabled     bool
	AuditRetention        time.Duration
	AuditPruneInterval    time.Duration
	GDPRExportTTL         time.Duration
	GDPRStepUpTTL         time.Duration
	GDPRDeleteGraceWindow time.Duration
	ShutdownDrainDelay    time.Duration
	ShutdownTimeout       time.Duration
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

type MigrationsConfig struct {
	ConfirmLarkDelete bool `yaml:"confirm_lark_delete"`
}

type EnricherConfig struct {
	Interval    string  `yaml:"interval"`
	Batch       int     `yaml:"batch"`
	QueueLen    int     `yaml:"queue_len"`
	Workers     int     `yaml:"workers"`
	BatchWindow string  `yaml:"batch_window"`
	LLMMaxQPS   float64 `yaml:"llm_max_qps"`
	LLMBurst    int     `yaml:"llm_burst"`
}

type AuditConfig struct {
	RetentionDays int    `yaml:"retention_days"`
	PruneInterval string `yaml:"prune_interval"`
}

type GDPRConfig struct {
	ExportTTL         string `yaml:"export_ttl"`
	StepUpTTL         string `yaml:"step_up_ttl"`
	DeleteGraceWindow string `yaml:"delete_grace_window"`
}

type ConsoleConfig struct {
	BaseURL        string               `yaml:"base_url"`
	SessionKey     string               `yaml:"session_key"`
	BootstrapAdmin BootstrapAdminConfig `yaml:"bootstrap_admin"`
}

type ShutdownConfig struct {
	DrainDelay string `yaml:"drain_delay"`
	Timeout    string `yaml:"timeout"`
}

type BootstrapAdminConfig struct {
	Email    string `yaml:"email"`
	Password string `yaml:"password"`
}

type SecretsConfig struct {
	TinkKeyset             string `yaml:"tink_keyset"`
	LegacyInboundMasterKey string `yaml:"legacy_inbound_master_key"`
}

// SecurityConfig holds deploy-level egress + proxy hardening knobs.
type SecurityConfig struct {
	// AllowLoopbackEgress permits outbound webhook / LLM dials to loopback
	// (127.0.0.0/8, ::1, localhost). Off by default — production treats it as an
	// SSRF vector. Local dev and the loopback reverse-proxy e2e set it true.
	AllowLoopbackEgress bool `yaml:"allow_loopback_egress"`
	// AllowPrivateEgress permits outbound dials to RFC1918 / unique-local
	// networks. On-prem deployments co-located with an internal IMAP/LLM host set
	// this. Cloud-metadata, link-local, unspecified, and multicast targets are
	// always blocked regardless of these flags.
	AllowPrivateEgress bool `yaml:"allow_private_egress"`
	// TrustedProxyHops is the number of reverse proxies in front of attune that
	// append to X-Forwarded-For. The API-key IP allowlist reads the client IP
	// that many hops from the right of XFF. 0 (default) ignores XFF entirely and
	// uses the direct peer, so a direct client cannot spoof an allowlisted IP.
	TrustedProxyHops int `yaml:"trusted_proxy_hops"`
}

// EgressPolicy builds the nethardening policy from the security config.
func (c *Config) EgressPolicy() nethardening.Policy {
	return nethardening.Policy{
		AllowLoopback: c.Security.AllowLoopbackEgress,
		AllowPrivate:  c.Security.AllowPrivateEgress,
	}
}

type ObservabilityConfig struct {
	ServiceVersion string            `yaml:"service_version"`
	Environment    string            `yaml:"environment"`
	OTLPEndpoint   string            `yaml:"otlp_endpoint"`
	OTLPTracesPath string            `yaml:"otlp_traces_path"`
	OTLPHeaders    map[string]string `yaml:"otlp_headers"`
	OTLPInsecure   bool              `yaml:"otlp_insecure"`
}

type RateLimitConfig struct {
	PerMinute int  `yaml:"per_minute"`
	Burst     int  `yaml:"burst"`
	Disabled  bool `yaml:"disabled"`
}

type yamlConfig struct {
	Port           int                 `yaml:"port"`
	Database       DatabaseConfig      `yaml:"database"`
	Migrations     MigrationsConfig    `yaml:"migrations"`
	Enricher       EnricherConfig      `yaml:"enricher"`
	Audit          AuditConfig         `yaml:"audit"`
	GDPR           GDPRConfig          `yaml:"gdpr"`
	Console        ConsoleConfig       `yaml:"console"`
	Shutdown       ShutdownConfig      `yaml:"shutdown"`
	Secrets        SecretsConfig       `yaml:"secrets"`
	Observability  ObservabilityConfig `yaml:"observability"`
	RateLimit      RateLimitConfig     `yaml:"rate_limit"`
	OIDC           OIDCConfig          `yaml:"oidc"`
	CustomWebhooks []CustomWebhookDest `yaml:"custom_webhooks"`
}

// Load reads Path() and validates the runtime config. Environment variable
// overrides are intentionally unsupported: the YAML file is the source of
// truth for process config.
func Load() (*Config, error) {
	return LoadPath(Path())
}

// LoadPath reads a specific config path. Missing files are fatal because there
// is no env fallback in the config-first runtime contract.
func LoadPath(path string) (*Config, error) {
	const where = "config.Load"
	ctx := context.Background()
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		trimmed = defaultPath
	}
	raw, err := os.ReadFile(trimmed)
	if err != nil {
		logext.Errorf(ctx, "[%s] read file failed,path:%s,err:%+v",
			where, trimmed, err.Error())
		return nil, fmt.Errorf("read config %s: %w", trimmed, err)
	}
	yc, err := parseYAML(raw)
	if err != nil {
		logext.Errorf(ctx, "[%s] parse yaml failed,path:%s,err:%+v",
			where, trimmed, err.Error())
		return nil, fmt.Errorf("parse config %s: %w", trimmed, err)
	}
	c, err := buildConfig(yc)
	if err != nil {
		logext.Errorf(ctx, "[%s] validate failed,err:%+v", where, err.Error())
		return nil, err
	}
	logext.Infof(ctx, "[%s] OK,path:%s,port:%d,console_enabled:%t,secrets:%s,legacy_inbound_key:%t",
		where, trimmed, c.Port, c.ConsoleSessionKey != "",
		secretstore.RedactKeysetForLog(c.Secrets.TinkKeyset),
		strings.TrimSpace(c.Secrets.LegacyInboundMasterKey) != "")
	return c, nil
}

func parseYAML(raw []byte) (*yamlConfig, error) {
	yc := ptrext.Of(yamlConfig{})
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(yc); err != nil {
		return nil, err
	}
	return yc, nil
}

func buildConfig(yc *yamlConfig) (*Config, error) {
	c := ptrext.Of(Config{
		Port:           yc.Port,
		Database:       yc.Database,
		Migrations:     yc.Migrations,
		Enricher:       yc.Enricher,
		Audit:          yc.Audit,
		GDPR:           yc.GDPR,
		Console:        yc.Console,
		Shutdown:       yc.Shutdown,
		Secrets:        yc.Secrets,
		Observability:  yc.Observability,
		RateLimit:      yc.RateLimit,
		OIDC:           yc.OIDC,
		CustomWebhooks: yc.CustomWebhooks,
	})
	c.applyDefaults()
	if err := c.parseDerivedFields(); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) parseDerivedFields() error {
	d, err := time.ParseDuration(c.Enricher.Interval)
	if err != nil {
		return fmt.Errorf("enricher.interval: %w", err)
	}
	batchWindow, err := time.ParseDuration(c.Enricher.BatchWindow)
	if err != nil {
		return fmt.Errorf("enricher.batch_window: %w", err)
	}
	pruneInterval, err := time.ParseDuration(c.Audit.PruneInterval)
	if err != nil {
		return fmt.Errorf("audit.prune_interval: %w", err)
	}
	gdprExportTTL, err := time.ParseDuration(c.GDPR.ExportTTL)
	if err != nil {
		return fmt.Errorf("gdpr.export_ttl: %w", err)
	}
	gdprStepUpTTL, err := time.ParseDuration(c.GDPR.StepUpTTL)
	if err != nil {
		return fmt.Errorf("gdpr.step_up_ttl: %w", err)
	}
	gdprDeleteGrace, err := time.ParseDuration(c.GDPR.DeleteGraceWindow)
	if err != nil {
		return fmt.Errorf("gdpr.delete_grace_window: %w", err)
	}
	shutdownDrainDelay, err := time.ParseDuration(c.Shutdown.DrainDelay)
	if err != nil {
		return fmt.Errorf("shutdown.drain_delay: %w", err)
	}
	shutdownTimeout, err := time.ParseDuration(c.Shutdown.Timeout)
	if err != nil {
		return fmt.Errorf("shutdown.timeout: %w", err)
	}
	c.DatabaseURL = strings.TrimSpace(c.Database.URL)
	c.EnricherInterval = d
	c.EnricherBatch = c.Enricher.Batch
	c.EnricherQueueLen = c.Enricher.QueueLen
	c.EnricherWorkers = c.Enricher.Workers
	c.EnricherBatchWindow = batchWindow
	c.EnricherLLMMaxQPS = c.Enricher.LLMMaxQPS
	c.EnricherLLMBurst = c.Enricher.LLMBurst
	c.ConsoleSessionKey = strings.TrimSpace(c.Console.SessionKey)
	c.ConsoleBaseURL = strings.TrimSpace(c.Console.BaseURL)
	c.RateLimitPerMinute = c.RateLimit.PerMinute
	c.RateLimitBurst = c.RateLimit.Burst
	c.RateLimitDisabled = c.RateLimit.Disabled
	c.AuditRetention = time.Duration(c.Audit.RetentionDays) * 24 * time.Hour
	c.AuditPruneInterval = pruneInterval
	c.GDPRExportTTL = gdprExportTTL
	c.GDPRStepUpTTL = gdprStepUpTTL
	c.GDPRDeleteGraceWindow = gdprDeleteGrace
	c.ShutdownDrainDelay = shutdownDrainDelay
	c.ShutdownTimeout = shutdownTimeout
	return nil
}

func (c *Config) applyDefaults() {
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.Enricher.Interval == "" {
		c.Enricher.Interval = DefaultEnricherInterval.String()
	}
	if c.Enricher.Batch == 0 {
		c.Enricher.Batch = DefaultEnricherBatch
	}
	if c.Enricher.QueueLen == 0 {
		c.Enricher.QueueLen = DefaultEnricherQueueLen
	}
	if c.Enricher.Workers == 0 {
		c.Enricher.Workers = DefaultEnricherWorkers
	}
	if c.Enricher.BatchWindow == "" {
		c.Enricher.BatchWindow = DefaultEnricherBatchWindow.String()
	}
	if c.Enricher.LLMBurst == 0 && c.Enricher.LLMMaxQPS > 0 {
		c.Enricher.LLMBurst = max(1, int(c.Enricher.LLMMaxQPS))
	}
	if c.Audit.RetentionDays == 0 {
		c.Audit.RetentionDays = DefaultAuditRetentionDays
	}
	if c.Audit.PruneInterval == "" {
		c.Audit.PruneInterval = DefaultAuditPruneInterval.String()
	}
	c.applyGDPRDefaults()
	if c.Shutdown.DrainDelay == "" {
		c.Shutdown.DrainDelay = DefaultShutdownDrainDelay.String()
	}
	if c.Shutdown.Timeout == "" {
		c.Shutdown.Timeout = DefaultShutdownTimeout.String()
	}
	if c.RateLimit.PerMinute == 0 {
		c.RateLimit.PerMinute = DefaultRateLimitPerMinute
	}
	if c.RateLimit.Burst == 0 {
		c.RateLimit.Burst = DefaultRateLimitBurst
	}
	c.applyObservabilityDefaults()
	c.OIDC.ApplyDefaults()
}

func (c *Config) applyGDPRDefaults() {
	if c.GDPR.ExportTTL == "" {
		c.GDPR.ExportTTL = DefaultGDPRExportTTL.String()
	}
	if c.GDPR.StepUpTTL == "" {
		c.GDPR.StepUpTTL = DefaultGDPRStepUpTTL.String()
	}
	if c.GDPR.DeleteGraceWindow == "" {
		c.GDPR.DeleteGraceWindow = DefaultGDPRDeleteGraceWindow.String()
	}
}

func (c *Config) applyObservabilityDefaults() {
	if c.Observability.ServiceVersion == "" {
		c.Observability.ServiceVersion = DefaultServiceVersion
	}
	if c.Observability.Environment == "" {
		c.Observability.Environment = DefaultEnvironment
	}
	if c.Observability.OTLPTracesPath == "" {
		c.Observability.OTLPTracesPath = DefaultOTLPTracesPath
	}
}

func (c *Config) validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("config: database.url is required")
	}
	if strings.TrimSpace(c.Secrets.TinkKeyset) == "" {
		return fmt.Errorf("config: secrets.tink_keyset is required")
	}
	if _, err := secretstore.NewTinkStoreFromJSON(c.Secrets.TinkKeyset); err != nil {
		return fmt.Errorf("config: secrets.tink_keyset: %w", err)
	}
	if strings.TrimSpace(c.Secrets.LegacyInboundMasterKey) != "" {
		if _, err := secretstore.DecodeLegacyInboundMasterKey(c.Secrets.LegacyInboundMasterKey); err != nil {
			return fmt.Errorf("config: secrets.legacy_inbound_master_key: %w", err)
		}
	}
	if err := c.validateConsole(); err != nil {
		return err
	}
	if err := c.OIDC.Validate(); err != nil {
		return err
	}
	if err := c.validateAuditConfig(); err != nil {
		return err
	}
	if err := c.validateGDPRConfig(); err != nil {
		return err
	}
	if err := c.validateShutdownConfig(); err != nil {
		return err
	}
	if err := c.validateEnricherConfig(); err != nil {
		return err
	}
	return c.validateCustomWebhooks()
}

func (c *Config) validateAuditConfig() error {
	if c.Audit.RetentionDays < 1 {
		return fmt.Errorf("config: audit.retention_days must be at least 1")
	}
	if c.AuditPruneInterval <= 0 {
		return fmt.Errorf("config: audit.prune_interval must be positive")
	}
	return nil
}

func (c *Config) validateGDPRConfig() error {
	if c.GDPRExportTTL <= 0 {
		return fmt.Errorf("config: gdpr.export_ttl must be positive")
	}
	if c.GDPRStepUpTTL <= 0 {
		return fmt.Errorf("config: gdpr.step_up_ttl must be positive")
	}
	if c.GDPRDeleteGraceWindow <= 0 {
		return fmt.Errorf("config: gdpr.delete_grace_window must be positive")
	}
	return nil
}

func (c *Config) validateShutdownConfig() error {
	if c.ShutdownDrainDelay < 0 {
		return fmt.Errorf("config: shutdown.drain_delay must be non-negative")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("config: shutdown.timeout must be positive")
	}
	return nil
}

func (c *Config) validateEnricherConfig() error {
	if c.EnricherBatch <= 0 {
		return fmt.Errorf("config: enricher.batch must be positive")
	}
	if c.EnricherQueueLen <= 0 {
		return fmt.Errorf("config: enricher.queue_len must be positive")
	}
	if c.EnricherWorkers <= 0 {
		return fmt.Errorf("config: enricher.workers must be positive")
	}
	if c.EnricherBatchWindow <= 0 {
		return fmt.Errorf("config: enricher.batch_window must be positive")
	}
	if c.EnricherLLMMaxQPS < 0 {
		return fmt.Errorf("config: enricher.llm_max_qps must be non-negative")
	}
	if c.EnricherLLMBurst < 0 {
		return fmt.Errorf("config: enricher.llm_burst must be non-negative")
	}
	return nil
}

func (c *Config) validateConsole() error {
	hasBase := c.ConsoleBaseURL != ""
	hasSession := c.ConsoleSessionKey != ""
	if hasBase != hasSession {
		return fmt.Errorf("config: console.base_url and console.session_key must be set together")
	}
	if !hasSession {
		return nil
	}
	if len(c.ConsoleSessionKey) < 32 {
		return fmt.Errorf("config: console.session_key must be at least 32 bytes")
	}
	if c.ConsoleSessionKey == "replace-with-32-or-more-random-characters" {
		return fmt.Errorf("config: console.session_key must be replaced with a random value")
	}
	if c.Console.BootstrapAdmin.Email != "" && c.Console.BootstrapAdmin.Password == "" {
		return fmt.Errorf("config: console.bootstrap_admin.password is required when email is set")
	}
	if c.Console.BootstrapAdmin.Password != "" && c.Console.BootstrapAdmin.Email == "" {
		return fmt.Errorf("config: console.bootstrap_admin.email is required when password is set")
	}
	if c.Console.BootstrapAdmin.Password == "replace-this-after-first-login" {
		return fmt.Errorf("config: console.bootstrap_admin.password must be replaced before startup")
	}
	return nil
}
