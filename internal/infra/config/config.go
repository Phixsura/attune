// SPDX-License-Identifier: Apache-2.0

// Package config loads the single attune YAML runtime config.
package config

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const defaultPath = "config.yaml"

const (
	ProfileDev        = "dev"
	ProfileProduction = "production"
)

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
	Profile string
	Port    int

	Ingest        IngestConfig
	Database      DatabaseConfig
	Migrations    MigrationsConfig
	Enricher      EnricherConfig
	Workers       WorkersConfig
	Audit         AuditConfig
	AuditEvidence AuditEvidenceConfig
	GDPR          GDPRConfig
	Console       ConsoleConfig
	Slack         SlackConfig
	Intercom      IntercomConfig
	Shutdown      ShutdownConfig
	Secrets       SecretsConfig
	Observability ObservabilityConfig
	RateLimit     RateLimitConfig
	OIDC          OIDCConfig
	Security      SecurityConfig
	MCP           MCPConfig

	CustomWebhooks []CustomWebhookDest

	// Convenience fields used by legacy wiring while the runtime contract is
	// config-first/nested.
	DatabaseURL             string
	EnricherInterval        time.Duration
	EnricherBatch           int
	EnricherQueueLen        int
	EnricherWorkers         int
	EnricherBatchWindow     time.Duration
	EnricherLLMMaxQPS       float64
	EnricherLLMBurst        int
	ConsoleSessionKey       string
	ConsoleBaseURL          string
	SlackAPIBaseURL         string
	IntercomAPIBaseURL      string
	RateLimitPerMinute      int
	RateLimitBurst          int
	RateLimitDisabled       bool
	AuditRetention          time.Duration
	AuditPruneInterval      time.Duration
	AuditEvidenceExportTTL  time.Duration
	AuditEvidenceSigningKey []byte
	GDPRExportTTL           time.Duration
	GDPRStepUpTTL           time.Duration
	GDPRDeleteGraceWindow   time.Duration
	ShutdownDrainDelay      time.Duration
	ShutdownTimeout         time.Duration

	// MCP convenience fields
	MCPEnabled            bool
	MCPPublicBaseURL      string
	MCPAccessTokenTTL     time.Duration
	MCPRefreshTokenTTL    time.Duration
	MCPRateLimitPerMinute int
	MCPRateLimitBurst     int

	// Worker convenience fields
	WorkerHeartbeatInterval  time.Duration
	WorkerStaleDuration      time.Duration
	WorkerDrainTimeout       time.Duration
	WorkerPollInterval       time.Duration
	WorkerMaxAttempts        int
	IngestCORSAllowedOrigins []string
}

type IngestConfig struct {
	// CORSAllowedOrigins enables browser cross-origin ingest for the exact
	// listed origins. Empty keeps first-party CORS disabled; use this only for
	// publishable ingest:write widgets, not server-only management APIs.
	CORSAllowedOrigins []string `yaml:"cors_allowed_origins"`
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

// WorkersConfig holds background worker tuning parameters.
type WorkersConfig struct {
	HeartbeatInterval string `yaml:"heartbeat_interval"` // e.g. "90s"
	StaleDuration     string `yaml:"stale_duration"`     // e.g. "5m"
	DrainTimeout      string `yaml:"drain_timeout"`      // e.g. "30s"
	PollInterval      string `yaml:"poll_interval"`      // e.g. "5s"
	MaxAttempts       int    `yaml:"max_attempts"`       // default 5
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

type AuditEvidenceConfig struct {
	ExportTTL  string `yaml:"export_ttl"`
	SigningKey string `yaml:"signing_key"`
}

type ConsoleConfig struct {
	BaseURL        string               `yaml:"base_url"`
	SessionKey     string               `yaml:"session_key"`
	BootstrapAdmin BootstrapAdminConfig `yaml:"bootstrap_admin"`
}

type SlackConfig struct {
	// APIBaseURL points the Slack client at a mock or regional API base.
	// Empty keeps the default slack.com API origin.
	APIBaseURL string `yaml:"api_base_url"`
}

type IntercomConfig struct {
	// APIBaseURL points the Intercom client at a mock API base for local
	// stacks and tests. Empty keeps the region-derived *.intercom.io
	// origin (and its host allowlist).
	APIBaseURL string `yaml:"api_base_url"`
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
	// AlertWebhookURL is the URL to POST security alerts to (break-glass use,
	// suspicious activity, etc.). Empty disables alerting.
	AlertWebhookURL string `yaml:"alert_webhook_url"`
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

// MCPConfig holds Model Context Protocol server settings.
type MCPConfig struct {
	Enabled                 bool               `yaml:"enabled"`
	PublicBaseURL           string             `yaml:"public_base_url"`
	OAuth                   MCPOAuthConfig     `yaml:"oauth"`
	RateLimit               MCPRateLimitConfig `yaml:"rate_limit"`
	AllowedRedirectPatterns []string           `yaml:"allowed_redirect_patterns"`
}

// MCPOAuthConfig holds OAuth 2.1 Authorization Server settings.
type MCPOAuthConfig struct {
	// JWTSecret is the HS256 signing key for MCP access tokens.
	// Must be at least 32 bytes when MCP is enabled.
	JWTSecret       string `yaml:"jwt_secret"`
	Issuer          string `yaml:"issuer"`
	AccessTokenTTL  string `yaml:"access_token_ttl"`
	RefreshTokenTTL string `yaml:"refresh_token_ttl"`
}

// MCPRateLimitConfig holds MCP-specific rate limiting.
type MCPRateLimitConfig struct {
	RequestsPerMinute int `yaml:"requests_per_minute"`
	Burst             int `yaml:"burst"`
}

type yamlConfig struct {
	Profile        string              `yaml:"profile"`
	Port           int                 `yaml:"port"`
	Ingest         IngestConfig        `yaml:"ingest"`
	Database       DatabaseConfig      `yaml:"database"`
	Migrations     MigrationsConfig    `yaml:"migrations"`
	Enricher       EnricherConfig      `yaml:"enricher"`
	Audit          AuditConfig         `yaml:"audit"`
	AuditEvidence  AuditEvidenceConfig `yaml:"audit_evidence"`
	GDPR           GDPRConfig          `yaml:"gdpr"`
	Console        ConsoleConfig       `yaml:"console"`
	Slack          SlackConfig         `yaml:"slack"`
	Intercom       IntercomConfig      `yaml:"intercom"`
	Shutdown       ShutdownConfig      `yaml:"shutdown"`
	Secrets        SecretsConfig       `yaml:"secrets"`
	Observability  ObservabilityConfig `yaml:"observability"`
	RateLimit      RateLimitConfig     `yaml:"rate_limit"`
	OIDC           OIDCConfig          `yaml:"oidc"`
	Security       SecurityConfig      `yaml:"security"`
	MCP            MCPConfig           `yaml:"mcp"`
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
	logext.Infof(ctx, "[%s] OK,path:%s,profile:%s,port:%d,console_enabled:%t,secrets:%s,legacy_inbound_key:%t",
		where, trimmed, c.Profile, c.Port, c.ConsoleSessionKey != "",
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
		Profile:        yc.Profile,
		Port:           yc.Port,
		Ingest:         yc.Ingest,
		Database:       yc.Database,
		Migrations:     yc.Migrations,
		Enricher:       yc.Enricher,
		Audit:          yc.Audit,
		AuditEvidence:  yc.AuditEvidence,
		GDPR:           yc.GDPR,
		Console:        yc.Console,
		Slack:          yc.Slack,
		Intercom:       yc.Intercom,
		Shutdown:       yc.Shutdown,
		Secrets:        yc.Secrets,
		Observability:  yc.Observability,
		RateLimit:      yc.RateLimit,
		OIDC:           yc.OIDC,
		Security:       yc.Security,
		MCP:            yc.MCP,
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
	if err := c.parseEnricherFields(); err != nil {
		return err
	}
	if err := c.parseAuditFields(); err != nil {
		return err
	}
	if err := c.parseAuditEvidenceFields(); err != nil {
		return err
	}
	if err := c.parseGDPRFields(); err != nil {
		return err
	}
	if err := c.parseShutdownFields(); err != nil {
		return err
	}
	if err := c.parseIngestFields(); err != nil {
		return err
	}
	c.parseSimpleFields() // must come before parseMCPFields (uses ConsoleBaseURL)
	if err := c.parseMCPFields(); err != nil {
		return err
	}
	if err := c.parseWorkerFields(); err != nil {
		return err
	}
	return nil
}

func (c *Config) parseEnricherFields() error {
	d, err := time.ParseDuration(c.Enricher.Interval)
	if err != nil {
		return fmt.Errorf("enricher.interval: %w", err)
	}
	batchWindow, err := time.ParseDuration(c.Enricher.BatchWindow)
	if err != nil {
		return fmt.Errorf("enricher.batch_window: %w", err)
	}
	c.EnricherInterval = d
	c.EnricherBatch = c.Enricher.Batch
	c.EnricherQueueLen = c.Enricher.QueueLen
	c.EnricherWorkers = c.Enricher.Workers
	c.EnricherBatchWindow = batchWindow
	c.EnricherLLMMaxQPS = c.Enricher.LLMMaxQPS
	c.EnricherLLMBurst = c.Enricher.LLMBurst
	return nil
}

func (c *Config) parseAuditFields() error {
	pruneInterval, err := time.ParseDuration(c.Audit.PruneInterval)
	if err != nil {
		return fmt.Errorf("audit.prune_interval: %w", err)
	}
	c.AuditRetention = time.Duration(c.Audit.RetentionDays) * 24 * time.Hour
	c.AuditPruneInterval = pruneInterval
	return nil
}

func (c *Config) parseAuditEvidenceFields() error {
	ttl, err := time.ParseDuration(c.AuditEvidence.ExportTTL)
	if err != nil {
		return fmt.Errorf("audit_evidence.export_ttl: %w", err)
	}
	c.AuditEvidenceExportTTL = ttl
	if raw := strings.TrimSpace(c.AuditEvidence.SigningKey); raw != "" {
		key, err := hex.DecodeString(raw)
		if err != nil {
			return fmt.Errorf("audit_evidence.signing_key: must be hex-encoded: %w", err)
		}
		const ed25519SeedSize = 32
		if len(key) < ed25519SeedSize {
			return fmt.Errorf("audit_evidence.signing_key: must be at least %d bytes (got %d)", ed25519SeedSize, len(key))
		}
		c.AuditEvidenceSigningKey = key
	}
	return nil
}

func (c *Config) parseGDPRFields() error {
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
	c.GDPRExportTTL = gdprExportTTL
	c.GDPRStepUpTTL = gdprStepUpTTL
	c.GDPRDeleteGraceWindow = gdprDeleteGrace
	return nil
}

func (c *Config) parseShutdownFields() error {
	shutdownDrainDelay, err := time.ParseDuration(c.Shutdown.DrainDelay)
	if err != nil {
		return fmt.Errorf("shutdown.drain_delay: %w", err)
	}
	shutdownTimeout, err := time.ParseDuration(c.Shutdown.Timeout)
	if err != nil {
		return fmt.Errorf("shutdown.timeout: %w", err)
	}
	c.ShutdownDrainDelay = shutdownDrainDelay
	c.ShutdownTimeout = shutdownTimeout
	return nil
}

func (c *Config) parseIngestFields() error {
	origins, err := normalizeConfiguredOrigins(c.Ingest.CORSAllowedOrigins, "ingest.cors_allowed_origins")
	if err != nil {
		return err
	}
	c.Ingest.CORSAllowedOrigins = origins
	c.IngestCORSAllowedOrigins = append([]string(nil), origins...)
	return nil
}

func (c *Config) parseMCPFields() error {
	c.MCPEnabled = c.MCP.Enabled
	c.MCPPublicBaseURL = deriveMCPPublicBaseURL(c.MCP.PublicBaseURL, c.MCP.OAuth.Issuer, c.ConsoleBaseURL)
	mcpAccessTTL, err := time.ParseDuration(c.MCP.OAuth.AccessTokenTTL)
	if err != nil {
		return fmt.Errorf("mcp.oauth.access_token_ttl: %w", err)
	}
	mcpRefreshTTL, err := time.ParseDuration(c.MCP.OAuth.RefreshTokenTTL)
	if err != nil {
		return fmt.Errorf("mcp.oauth.refresh_token_ttl: %w", err)
	}
	c.MCPAccessTokenTTL = mcpAccessTTL
	c.MCPRefreshTokenTTL = mcpRefreshTTL
	c.MCPRateLimitPerMinute = c.MCP.RateLimit.RequestsPerMinute
	c.MCPRateLimitBurst = c.MCP.RateLimit.Burst
	return nil
}

func (c *Config) parseWorkerFields() error {
	workerHeartbeat, err := time.ParseDuration(c.Workers.HeartbeatInterval)
	if err != nil {
		return fmt.Errorf("workers.heartbeat_interval: %w", err)
	}
	workerStale, err := time.ParseDuration(c.Workers.StaleDuration)
	if err != nil {
		return fmt.Errorf("workers.stale_duration: %w", err)
	}
	workerDrain, err := time.ParseDuration(c.Workers.DrainTimeout)
	if err != nil {
		return fmt.Errorf("workers.drain_timeout: %w", err)
	}
	workerPoll, err := time.ParseDuration(c.Workers.PollInterval)
	if err != nil {
		return fmt.Errorf("workers.poll_interval: %w", err)
	}
	c.WorkerHeartbeatInterval = workerHeartbeat
	c.WorkerStaleDuration = workerStale
	c.WorkerDrainTimeout = workerDrain
	c.WorkerPollInterval = workerPoll
	c.WorkerMaxAttempts = c.Workers.MaxAttempts
	return nil
}

func (c *Config) parseSimpleFields() {
	c.DatabaseURL = strings.TrimSpace(c.Database.URL)
	c.ConsoleSessionKey = strings.TrimSpace(c.Console.SessionKey)
	c.ConsoleBaseURL = strings.TrimSpace(c.Console.BaseURL)
	c.SlackAPIBaseURL = strings.TrimSpace(c.Slack.APIBaseURL)
	c.IntercomAPIBaseURL = strings.TrimSpace(c.Intercom.APIBaseURL)
	c.RateLimitPerMinute = c.RateLimit.PerMinute
	c.RateLimitBurst = c.RateLimit.Burst
	c.RateLimitDisabled = c.RateLimit.Disabled
}

// IsProduction reports whether the runtime profile is production.
func (c *Config) IsProduction() bool {
	if c == nil {
		return false
	}
	return normalizeProfile(c.Profile) == ProfileProduction
}

func (c *Config) applyDefaults() {
	c.Profile = normalizeProfile(c.Profile)
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
	c.applyAuditEvidenceDefaults()
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
	c.applyWorkerDefaults()
	c.applyMCPDefaults()
}

func (c *Config) applyAuditEvidenceDefaults() {
	if c.AuditEvidence.ExportTTL == "" {
		c.AuditEvidence.ExportTTL = DefaultAuditEvidenceExportTTL.String()
	}
}

func (c *Config) applyWorkerDefaults() {
	if c.Workers.HeartbeatInterval == "" {
		c.Workers.HeartbeatInterval = DefaultWorkerHeartbeatInterval.String()
	}
	if c.Workers.StaleDuration == "" {
		c.Workers.StaleDuration = DefaultWorkerStaleDuration.String()
	}
	if c.Workers.DrainTimeout == "" {
		c.Workers.DrainTimeout = DefaultWorkerDrainTimeout.String()
	}
	if c.Workers.PollInterval == "" {
		c.Workers.PollInterval = DefaultWorkerPollInterval.String()
	}
	if c.Workers.MaxAttempts == 0 {
		c.Workers.MaxAttempts = DefaultWorkerMaxAttempts
	}
}

func (c *Config) applyMCPDefaults() {
	if c.MCP.OAuth.AccessTokenTTL == "" {
		c.MCP.OAuth.AccessTokenTTL = "1h"
	}
	if c.MCP.OAuth.RefreshTokenTTL == "" {
		c.MCP.OAuth.RefreshTokenTTL = "168h" // 7 days
	}
	if c.MCP.RateLimit.RequestsPerMinute == 0 {
		c.MCP.RateLimit.RequestsPerMinute = 60
	}
	if c.MCP.RateLimit.Burst == 0 {
		c.MCP.RateLimit.Burst = 10
	}
	if len(c.MCP.AllowedRedirectPatterns) == 0 {
		c.MCP.AllowedRedirectPatterns = []string{
			"http://127.0.0.1:*",
			"http://localhost:*",
		}
	}
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
		c.Observability.Environment = c.Profile
		if c.Observability.Environment == "" {
			c.Observability.Environment = DefaultEnvironment
		}
	}
	if c.Observability.OTLPTracesPath == "" {
		c.Observability.OTLPTracesPath = DefaultOTLPTracesPath
	}
}

func normalizeProfile(profile string) string {
	trimmed := strings.ToLower(strings.TrimSpace(profile))
	if trimmed == "" {
		return ProfileDev
	}
	return trimmed
}

func (c *Config) validate() error {
	for _, check := range []func() error{
		c.validateDatabaseURL,
		c.validateProfile,
		c.validateSecretsConfig,
		c.validateConsole,
		c.validateSlackConfig,
		c.validateIntercomConfig,
		c.validateSecurityConfig,
		c.OIDC.Validate,
		c.validateAuditConfig,
		c.validateAuditEvidenceConfig,
		c.validateGDPRConfig,
		c.validateShutdownConfig,
		c.validateEnricherConfig,
		c.validateWorkerConfig,
		c.validateMCPConfig,
		c.validateIngestConfig,
	} {
		if err := check(); err != nil {
			return err
		}
	}
	return c.validateCustomWebhooks()
}

func (c *Config) validateDatabaseURL() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("config: database.url is required")
	}
	return nil
}

func (c *Config) validateSecretsConfig() error {
	if strings.TrimSpace(c.Secrets.TinkKeyset) == "" {
		return fmt.Errorf("config: secrets.tink_keyset is required")
	}
	if _, err := secretstore.NewTinkStoreFromJSON(c.Secrets.TinkKeyset); err != nil {
		return fmt.Errorf("config: secrets.tink_keyset: %w", err)
	}
	if strings.TrimSpace(c.Secrets.LegacyInboundMasterKey) == "" {
		return nil
	}
	if _, err := secretstore.DecodeLegacyInboundMasterKey(c.Secrets.LegacyInboundMasterKey); err != nil {
		return fmt.Errorf("config: secrets.legacy_inbound_master_key: %w", err)
	}
	return nil
}

func (c *Config) validateProfile() error {
	switch normalizeProfile(c.Profile) {
	case ProfileDev, ProfileProduction:
		return nil
	default:
		return fmt.Errorf("config: profile must be one of %q or %q", ProfileDev, ProfileProduction)
	}
}

func (c *Config) validateIngestConfig() error {
	if len(c.IngestCORSAllowedOrigins) == 0 {
		return nil
	}
	if len(c.IngestCORSAllowedOrigins) > 1 {
		for _, origin := range c.IngestCORSAllowedOrigins {
			if origin == "*" {
				return fmt.Errorf("config: ingest.cors_allowed_origins cannot mix \"*\" with explicit origins")
			}
		}
	}
	return nil
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

func (c *Config) validateAuditEvidenceConfig() error {
	if c.AuditEvidenceExportTTL <= 0 {
		return fmt.Errorf("config: audit_evidence.export_ttl must be positive")
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

func (c *Config) validateWorkerConfig() error {
	if c.WorkerHeartbeatInterval <= 0 {
		return fmt.Errorf("config: workers.heartbeat_interval must be positive")
	}
	if c.WorkerStaleDuration <= 0 {
		return fmt.Errorf("config: workers.stale_duration must be positive")
	}
	if c.WorkerDrainTimeout <= 0 {
		return fmt.Errorf("config: workers.drain_timeout must be positive")
	}
	if c.WorkerPollInterval <= 0 {
		return fmt.Errorf("config: workers.poll_interval must be positive")
	}
	if c.WorkerMaxAttempts <= 0 {
		return fmt.Errorf("config: workers.max_attempts must be positive")
	}
	if c.WorkerHeartbeatInterval >= c.WorkerStaleDuration {
		return fmt.Errorf("config: workers.heartbeat_interval (%v) must be less than workers.stale_duration (%v)",
			c.WorkerHeartbeatInterval, c.WorkerStaleDuration)
	}
	return nil
}

func (c *Config) validateMCPConfig() error {
	if !c.MCP.Enabled {
		return nil
	}
	secret := strings.TrimSpace(c.MCP.OAuth.JWTSecret)
	if secret == "" {
		return fmt.Errorf("config: mcp.oauth.jwt_secret is required when MCP is enabled")
	}
	if len(secret) < 32 {
		return fmt.Errorf("config: mcp.oauth.jwt_secret must be at least 32 bytes")
	}
	if secret == "replace-with-32-or-more-random-characters" {
		return fmt.Errorf("config: mcp.oauth.jwt_secret must be replaced with a random value")
	}
	if c.MCPAccessTokenTTL <= 0 {
		return fmt.Errorf("config: mcp.oauth.access_token_ttl must be positive")
	}
	if c.MCPRefreshTokenTTL <= 0 {
		return fmt.Errorf("config: mcp.oauth.refresh_token_ttl must be positive")
	}
	if c.MCPRateLimitPerMinute <= 0 {
		return fmt.Errorf("config: mcp.rate_limit.requests_per_minute must be positive")
	}
	if c.MCPRateLimitBurst <= 0 {
		return fmt.Errorf("config: mcp.rate_limit.burst must be positive")
	}
	if c.MCPPublicBaseURL == "" {
		return fmt.Errorf("config: mcp.public_base_url or console.base_url is required when MCP is enabled")
	}
	if _, err := url.ParseRequestURI(c.MCPPublicBaseURL); err != nil {
		return fmt.Errorf("config: mcp.public_base_url must be a valid URL")
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
	if _, err := url.ParseRequestURI(c.ConsoleBaseURL); err != nil {
		return fmt.Errorf("config: console.base_url must be a valid URL")
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

func (c *Config) validateSlackConfig() error {
	if c.SlackAPIBaseURL == "" {
		return nil
	}
	parsed, err := url.ParseRequestURI(c.SlackAPIBaseURL)
	if err != nil {
		return fmt.Errorf("config: slack.api_base_url must be a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("config: slack.api_base_url must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("config: slack.api_base_url must include a host")
	}
	return nil
}

func (c *Config) validateIntercomConfig() error {
	if c.IntercomAPIBaseURL == "" {
		return nil
	}
	parsed, err := url.ParseRequestURI(c.IntercomAPIBaseURL)
	if err != nil {
		return fmt.Errorf("config: intercom.api_base_url must be a valid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("config: intercom.api_base_url must use http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("config: intercom.api_base_url must include a host")
	}
	return nil
}

func (c *Config) validateSecurityConfig() error {
	if c.Security.TrustedProxyHops < 0 {
		return fmt.Errorf("config: security.trusted_proxy_hops must be non-negative")
	}
	return nil
}

func deriveMCPPublicBaseURL(explicit, issuer, consoleBaseURL string) string {
	if base := strings.TrimRight(strings.TrimSpace(explicit), "/"); base != "" {
		return base
	}
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	if issuer != "" {
		if parsed, err := url.ParseRequestURI(issuer); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			path := strings.TrimSuffix(parsed.Path, "/")
			switch {
			case path == "":
				return parsed.Scheme + "://" + parsed.Host
			case strings.HasSuffix(path, "/mcp/oauth"):
				return parsed.Scheme + "://" + parsed.Host + strings.TrimSuffix(path, "/mcp/oauth")
			}
		}
	}
	return strings.TrimRight(consoleBaseURL, "/")
}

func normalizeConfiguredOrigins(origins []string, field string) ([]string, error) {
	if len(origins) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(origins))
	out := make([]string, 0, len(origins))
	for _, raw := range origins {
		origin, err := normalizeOrigin(raw, field)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[origin]; ok {
			continue
		}
		seen[origin] = struct{}{}
		out = append(out, origin)
	}
	return out, nil
}

func normalizeOrigin(raw, field string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("config: %s entries must not be empty", field)
	}
	if trimmed == "*" {
		return trimmed, nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("config: %s contains invalid origin %q: %w", field, trimmed, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("config: %s origin %q must include scheme and host", field, trimmed)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("config: %s origin %q must use http or https", field, trimmed)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("config: %s origin %q must not include credentials", field, trimmed)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("config: %s origin %q must not include query or fragment", field, trimmed)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("config: %s origin %q must not include a path", field, trimmed)
	}
	if _, err := canonicalPort(parsed, field); err != nil {
		return "", err
	}
	return strings.ToLower(parsed.Scheme) + "://" + canonicalOriginHost(parsed), nil
}

func canonicalOriginHost(parsed *url.URL) string {
	hostname := strings.ToLower(parsed.Hostname())
	port, _ := canonicalPort(parsed, "")
	if isDefaultOriginPort(strings.ToLower(parsed.Scheme), port) {
		port = ""
	}
	if port == "" {
		if strings.Contains(hostname, ":") {
			return "[" + hostname + "]"
		}
		return hostname
	}
	return net.JoinHostPort(hostname, port)
}

func canonicalPort(parsed *url.URL, field string) (string, error) {
	port := parsed.Port()
	if port == "" {
		return "", nil
	}
	value, err := strconv.Atoi(port)
	if err != nil || value <= 0 || value > 65535 {
		if field == "" {
			return "", fmt.Errorf("invalid port %q", port)
		}
		return "", fmt.Errorf("config: %s origin %q must use a valid port in [1, 65535]", field, parsed.String())
	}
	return strconv.Itoa(value), nil
}

func isDefaultOriginPort(scheme, port string) bool {
	switch scheme {
	case "http":
		return port == "80"
	case "https":
		return port == "443"
	default:
		return false
	}
}
