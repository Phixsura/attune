// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/infra/secretstore"
)

func TestLoadPathNewConfig(t *testing.T) {
	path := writeConfig(t, validConfigYAML(t, validTinkKeyset(t)))
	cfg, err := LoadPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://attune:test@localhost:5432/attune?sslmode=disable" {
		t.Fatalf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.EnricherBatch != DefaultEnricherBatch {
		t.Fatalf("EnricherBatch = %d", cfg.EnricherBatch)
	}
	if cfg.EnricherQueueLen != DefaultEnricherQueueLen {
		t.Fatalf("EnricherQueueLen = %d", cfg.EnricherQueueLen)
	}
	if cfg.EnricherWorkers != DefaultEnricherWorkers {
		t.Fatalf("EnricherWorkers = %d", cfg.EnricherWorkers)
	}
	if cfg.EnricherBatchWindow != DefaultEnricherBatchWindow {
		t.Fatalf("EnricherBatchWindow = %s", cfg.EnricherBatchWindow)
	}
	if cfg.Audit.RetentionDays != DefaultAuditRetentionDays {
		t.Fatalf("Audit.RetentionDays = %d", cfg.Audit.RetentionDays)
	}
	if cfg.AuditPruneInterval != DefaultAuditPruneInterval {
		t.Fatalf("AuditPruneInterval = %s", cfg.AuditPruneInterval)
	}
	if cfg.GDPRExportTTL != DefaultGDPRExportTTL {
		t.Fatalf("GDPRExportTTL = %s", cfg.GDPRExportTTL)
	}
	if cfg.GDPRStepUpTTL != DefaultGDPRStepUpTTL {
		t.Fatalf("GDPRStepUpTTL = %s", cfg.GDPRStepUpTTL)
	}
	if cfg.GDPRDeleteGraceWindow != DefaultGDPRDeleteGraceWindow {
		t.Fatalf("GDPRDeleteGraceWindow = %s", cfg.GDPRDeleteGraceWindow)
	}
	if cfg.ShutdownDrainDelay != DefaultShutdownDrainDelay {
		t.Fatalf("ShutdownDrainDelay = %s", cfg.ShutdownDrainDelay)
	}
	if cfg.ShutdownTimeout != DefaultShutdownTimeout {
		t.Fatalf("ShutdownTimeout = %s", cfg.ShutdownTimeout)
	}
	if cfg.Console.BootstrapAdmin.Email != "admin@example.com" {
		t.Fatalf("bootstrap admin email = %q", cfg.Console.BootstrapAdmin.Email)
	}
}

func TestLoadPathParsesSecurityBlock(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\nsecurity:\n  allow_loopback_egress: true\n  allow_private_egress: true\n  trusted_proxy_hops: 2\n"
	cfg, err := LoadPath(writeConfig(t, raw))
	if err != nil {
		t.Fatalf("load with security block: %v", err)
	}
	if !cfg.Security.AllowLoopbackEgress || !cfg.Security.AllowPrivateEgress {
		t.Fatalf("egress flags not parsed: %+v", cfg.Security)
	}
	if cfg.Security.TrustedProxyHops != 2 {
		t.Fatalf("TrustedProxyHops = %d, want 2", cfg.Security.TrustedProxyHops)
	}
	p := cfg.EgressPolicy()
	if !p.AllowLoopback || !p.AllowPrivate {
		t.Fatalf("EgressPolicy() did not reflect config: %+v", p)
	}
}

func TestLoadPathSecurityDefaultsAreSafe(t *testing.T) {
	cfg, err := LoadPath(writeConfig(t, validConfigYAML(t, validTinkKeyset(t))))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Security.AllowLoopbackEgress || cfg.Security.AllowPrivateEgress || cfg.Security.TrustedProxyHops != 0 {
		t.Fatalf("security defaults should be locked down, got %+v", cfg.Security)
	}
}

func TestLoadPathRejectsOldLLMFields(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\nllm_openai_api_key: sk-test\n"
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected unknown field error")
	}
	if !strings.Contains(err.Error(), "field llm_openai_api_key not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPathRejectsLLMBlock(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\nllm: {}\n"
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected unknown field error")
	}
	if !strings.Contains(err.Error(), "field llm not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPathRejectsFileIndirectionFields(t *testing.T) {
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"  url: \"postgres://attune:test@localhost:5432/attune?sslmode=disable\"",
		"  url_file: \"./database-url\"",
		1,
	)
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected unknown field error")
	}
	if !strings.Contains(err.Error(), "field url_file not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPathIgnoresRuntimeEnvOverrides(t *testing.T) {
	t.Setenv("FEEDBACK_API_DATABASE_URL", "postgres://attune:env@localhost:5432/env")
	t.Setenv("ATTUNE_BOOTSTRAP_ADMIN_EMAIL", "env-admin@example.com")
	t.Setenv("ATTUNE_CONFIRM_LARK_DELETE", "false")

	cfg, err := LoadPath(writeConfig(t, validConfigYAML(t, validTinkKeyset(t))))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://attune:test@localhost:5432/attune?sslmode=disable" {
		t.Fatalf("DatabaseURL = %q; want YAML value", cfg.DatabaseURL)
	}
	if cfg.Console.BootstrapAdmin.Email != "admin@example.com" {
		t.Fatalf("bootstrap admin = %q; want YAML value", cfg.Console.BootstrapAdmin.Email)
	}
	if !cfg.Migrations.ConfirmLarkDelete {
		t.Fatal("migrations.confirm_lark_delete should come from YAML")
	}
}

func TestLoadPathRejectsInvalidTinkKeyset(t *testing.T) {
	raw := validConfigYAML(t, "not-json")
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected invalid keyset error")
	}
	if !strings.Contains(err.Error(), "secrets.tink_keyset") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPathAcceptsLegacyInboundMasterKey(t *testing.T) {
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"secrets:\n  tink_keyset:",
		"secrets:\n  legacy_inbound_master_key: \""+hex.EncodeToString([]byte(strings.Repeat("a", 32)))+"\"\n  tink_keyset:",
		1,
	)
	cfg, err := LoadPath(writeConfig(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Secrets.LegacyInboundMasterKey == "" {
		t.Fatal("expected legacy key to load")
	}
}

func TestLoadPathRejectsInvalidLegacyInboundMasterKey(t *testing.T) {
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"secrets:\n  tink_keyset:",
		"secrets:\n  legacy_inbound_master_key: \"too-short\"\n  tink_keyset:",
		1,
	)
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected invalid legacy key error")
	}
	if !strings.Contains(err.Error(), "secrets.legacy_inbound_master_key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPathRejectsDeployCredentialPlaceholders(t *testing.T) {
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"01234567890123456789012345678901",
		"replace-with-32-or-more-random-characters",
		1,
	)
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected placeholder session key error")
	}
	if !strings.Contains(err.Error(), "console.session_key") {
		t.Fatalf("unexpected error: %v", err)
	}

	raw = strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"correct horse battery staple",
		"replace-this-after-first-login",
		1,
	)
	_, err = LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected placeholder bootstrap password error")
	}
	if !strings.Contains(err.Error(), "console.bootstrap_admin.password") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func validConfigYAML(t *testing.T, keysetJSON string) string {
	t.Helper()
	return strings.Replace(`port: 8090
database:
  url: "postgres://attune:test@localhost:5432/attune?sslmode=disable"
migrations:
  confirm_lark_delete: true
enricher:
  interval: "30s"
console:
  base_url: "https://attune.example.com"
  session_key: "01234567890123456789012345678901"
  bootstrap_admin:
    email: "admin@example.com"
    password: "correct horse battery staple"
secrets:
  tink_keyset: |
    TINK_KEYSET
observability:
  environment: "test"
rate_limit:
  per_minute: 10
  burst: 20
custom_webhooks: []
`, "TINK_KEYSET", indent(keysetJSON, "    "), 1)
}

func TestLoadPathRejectsInvalidAuditRetention(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\naudit:\n  retention_days: -1\n"
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected audit.retention_days validation error")
	}
	if !strings.Contains(err.Error(), "audit.retention_days") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPathRejectsInvalidAuditPruneInterval(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\naudit:\n  prune_interval: \"bad\"\n"
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected audit.prune_interval parse error")
	}
	if !strings.Contains(err.Error(), "audit.prune_interval") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPathRejectsInvalidGDPRDurations(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\ngdpr:\n  delete_grace_window: \"bad\"\n"
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected gdpr.delete_grace_window parse error")
	}
	if !strings.Contains(err.Error(), "gdpr.delete_grace_window") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPathParsesExtendedEnricherConfig(t *testing.T) {
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"enricher:\n  interval: \"30s\"\n",
		"enricher:\n  interval: \"45s\"\n  batch: 12\n  queue_len: 250\n  workers: 4\n  batch_window: \"750ms\"\n  llm_max_qps: 8\n  llm_burst: 9\n",
		1,
	)
	cfg, err := LoadPath(writeConfig(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnricherInterval != 45*time.Second {
		t.Fatalf("EnricherInterval = %s, want 45s", cfg.EnricherInterval)
	}
	if cfg.EnricherBatch != 12 {
		t.Fatalf("EnricherBatch = %d, want 12", cfg.EnricherBatch)
	}
	if cfg.EnricherQueueLen != 250 {
		t.Fatalf("EnricherQueueLen = %d, want 250", cfg.EnricherQueueLen)
	}
	if cfg.EnricherWorkers != 4 {
		t.Fatalf("EnricherWorkers = %d, want 4", cfg.EnricherWorkers)
	}
	if cfg.EnricherBatchWindow != 750*time.Millisecond {
		t.Fatalf("EnricherBatchWindow = %s, want 750ms", cfg.EnricherBatchWindow)
	}
	if cfg.EnricherLLMMaxQPS != 8 {
		t.Fatalf("EnricherLLMMaxQPS = %v, want 8", cfg.EnricherLLMMaxQPS)
	}
	if cfg.EnricherLLMBurst != 9 {
		t.Fatalf("EnricherLLMBurst = %d, want 9", cfg.EnricherLLMBurst)
	}
}

func TestLoadPathRejectsInvalidExtendedEnricherConfig(t *testing.T) {
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"enricher:\n  interval: \"30s\"\n",
		"enricher:\n  interval: \"30s\"\n  queue_len: -1\n",
		1,
	)
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected enricher.queue_len validation error")
	}
	if !strings.Contains(err.Error(), "enricher.queue_len") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPathParsesShutdownConfig(t *testing.T) {
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"observability:\n",
		"shutdown:\n  drain_delay: \"250ms\"\n  timeout: \"2s\"\nobservability:\n",
		1,
	)
	cfg, err := LoadPath(writeConfig(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ShutdownDrainDelay.String() != "250ms" {
		t.Fatalf("ShutdownDrainDelay = %s, want 250ms", cfg.ShutdownDrainDelay)
	}
	if cfg.ShutdownTimeout.String() != "2s" {
		t.Fatalf("ShutdownTimeout = %s, want 2s", cfg.ShutdownTimeout)
	}
}

func TestLoadPathRejectsInvalidShutdownConfig(t *testing.T) {
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"observability:\n",
		"shutdown:\n  drain_delay: \"-1s\"\n  timeout: \"2s\"\nobservability:\n",
		1,
	)
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected shutdown.drain_delay validation error")
	}
	if !strings.Contains(err.Error(), "shutdown.drain_delay") {
		t.Fatalf("unexpected error: %v", err)
	}

	raw = strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"observability:\n",
		"shutdown:\n  drain_delay: \"1s\"\n  timeout: \"0s\"\nobservability:\n",
		1,
	)
	_, err = LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected shutdown.timeout validation error")
	}
	if !strings.Contains(err.Error(), "shutdown.timeout") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPathAcceptsSlackWebhookWithoutSecret(t *testing.T) {
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"custom_webhooks: []",
		`custom_webhooks:
  - tenant_slug: demo
    destination_type: slack
    audience: pool
    url: "https://hooks.slack.com/services/T00/B00/xxx"`,
		1,
	)
	cfg, err := LoadPath(writeConfig(t, raw))
	if err != nil {
		t.Fatalf("slack webhook without secret should be valid: %v", err)
	}
	if len(cfg.CustomWebhooks) != 1 {
		t.Fatalf("expected 1 custom webhook, got %d", len(cfg.CustomWebhooks))
	}
	w := cfg.CustomWebhooks[0]
	if w.DestinationType != "slack" {
		t.Fatalf("destination_type = %q, want slack", w.DestinationType)
	}
	if w.Secret != "" {
		t.Fatalf("secret = %q, want empty for slack", w.Secret)
	}
}

func TestLoadPathAcceptsMultiChannelWebhooks(t *testing.T) {
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"custom_webhooks: []",
		`custom_webhooks:
  - tenant_slug: demo
    destination_type: raw-webhook
    audience: pool
    url: "https://example.com/hook"
    secret: "0123456789abcdef"
  - tenant_slug: demo
    destination_type: slack
    audience: pool
    url: "https://hooks.slack.com/services/T00/B00/xxx"`,
		1,
	)
	cfg, err := LoadPath(writeConfig(t, raw))
	if err != nil {
		t.Fatalf("same tenant+audience but different dest types should be valid: %v", err)
	}
	if len(cfg.CustomWebhooks) != 2 {
		t.Fatalf("expected 2 custom webhooks, got %d", len(cfg.CustomWebhooks))
	}
}

func TestLoadPathRejectsSlackWebhookDuplicate(t *testing.T) {
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"custom_webhooks: []",
		`custom_webhooks:
  - tenant_slug: demo
    destination_type: slack
    audience: pool
    url: "https://hooks.slack.com/services/T00/B00/xxx"
  - tenant_slug: demo
    destination_type: slack
    audience: pool
    url: "https://hooks.slack.com/services/T00/B00/yyy"`,
		1,
	)
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("duplicate slack webhook for same tenant+audience should be rejected")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetPathAndPath(t *testing.T) {
	original := Path()
	t.Cleanup(func() {
		SetPath(original)
	})

	SetPath("  custom.yaml  ")
	if got := Path(); got != "custom.yaml" {
		t.Fatalf("Path() = %q, want custom.yaml", got)
	}

	SetPath("   ")
	if got := Path(); got != defaultPath {
		t.Fatalf("Path() = %q, want %q", got, defaultPath)
	}
}

func TestLoadUsesActivePath(t *testing.T) {
	original := Path()
	t.Cleanup(func() {
		SetPath(original)
	})

	path := writeConfig(t, validConfigYAML(t, validTinkKeyset(t)))
	SetPath(path)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 8090 {
		t.Fatalf("Port = %d, want 8090", cfg.Port)
	}
}

func validTinkKeyset(t *testing.T) string {
	t.Helper()
	keysetJSON, err := secretstore.GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatal(err)
	}
	return keysetJSON
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func writeConfig(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadPathMCPRequiresJWTSecretWhenEnabled(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\nmcp:\n  enabled: true\n"
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected mcp.oauth.jwt_secret validation error")
	}
	if !strings.Contains(err.Error(), "mcp.oauth.jwt_secret is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPathMCPRequiresJWTSecretMinLength(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\nmcp:\n  enabled: true\n  oauth:\n    jwt_secret: \"short\"\n"
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected mcp.oauth.jwt_secret length validation error")
	}
	if !strings.Contains(err.Error(), "mcp.oauth.jwt_secret must be at least 32 bytes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPathMCPRejectsPlaceholderJWTSecret(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\nmcp:\n  enabled: true\n  oauth:\n    jwt_secret: \"replace-with-32-or-more-random-characters\"\n"
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected placeholder jwt_secret validation error")
	}
	if !strings.Contains(err.Error(), "mcp.oauth.jwt_secret must be replaced") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadPathMCPDisabledSkipsValidation(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\nmcp:\n  enabled: false\n"
	_, err := LoadPath(writeConfig(t, raw))
	if err != nil {
		t.Fatalf("MCP disabled should skip validation: %v", err)
	}
}

func TestLoadPathMCPValidConfig(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\nmcp:\n  enabled: true\n  oauth:\n    jwt_secret: \"01234567890123456789012345678901\"\n"
	cfg, err := LoadPath(writeConfig(t, raw))
	if err != nil {
		t.Fatalf("valid MCP config should load: %v", err)
	}
	if !cfg.MCPEnabled {
		t.Fatal("MCPEnabled should be true")
	}
	if cfg.MCPPublicBaseURL != "https://attune.example.com" {
		t.Fatalf("MCPPublicBaseURL = %q, want console base URL fallback", cfg.MCPPublicBaseURL)
	}
	if cfg.MCPRateLimitPerMinute != 60 {
		t.Fatalf("MCPRateLimitPerMinute = %d, want 60", cfg.MCPRateLimitPerMinute)
	}
	if cfg.MCPRateLimitBurst != 10 {
		t.Fatalf("MCPRateLimitBurst = %d, want 10", cfg.MCPRateLimitBurst)
	}
	if cfg.MCPAccessTokenTTL != time.Hour {
		t.Fatalf("MCPAccessTokenTTL = %s, want 1h", cfg.MCPAccessTokenTTL)
	}
	if cfg.MCPRefreshTokenTTL != 168*time.Hour {
		t.Fatalf("MCPRefreshTokenTTL = %s, want 168h", cfg.MCPRefreshTokenTTL)
	}
}

func TestLoadPathMCPUsesExplicitPublicBaseURL(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\nmcp:\n  enabled: true\n  public_base_url: \"https://mcp.attune.example.com\"\n  oauth:\n    jwt_secret: \"01234567890123456789012345678901\"\n"
	cfg, err := LoadPath(writeConfig(t, raw))
	if err != nil {
		t.Fatalf("valid MCP config with explicit public base URL should load: %v", err)
	}
	if cfg.MCPPublicBaseURL != "https://mcp.attune.example.com" {
		t.Fatalf("MCPPublicBaseURL = %q", cfg.MCPPublicBaseURL)
	}
}

func TestLoadPathMCPDerivesPublicBaseURLFromIssuer(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\nmcp:\n  enabled: true\n  oauth:\n    issuer: \"https://api.attune.example.com\"\n    jwt_secret: \"01234567890123456789012345678901\"\n"
	cfg, err := LoadPath(writeConfig(t, raw))
	if err != nil {
		t.Fatalf("valid MCP config with issuer-derived public base URL should load: %v", err)
	}
	if cfg.MCPPublicBaseURL != "https://api.attune.example.com" {
		t.Fatalf("MCPPublicBaseURL = %q", cfg.MCPPublicBaseURL)
	}
}

func TestLoadPathMCPRequiresPublicBaseURLWhenConsoleDisabled(t *testing.T) {
	raw := strings.Replace(validConfigYAML(t, validTinkKeyset(t)), "console:\n  base_url: \"https://attune.example.com\"\n  session_key: \"01234567890123456789012345678901\"\n  bootstrap_admin:\n    email: \"admin@example.com\"\n    password: \"correct horse battery staple\"\n", "", 1) +
		"\nmcp:\n  enabled: true\n  oauth:\n    jwt_secret: \"01234567890123456789012345678901\"\n"
	_, err := LoadPath(writeConfig(t, raw))
	if err == nil {
		t.Fatal("expected mcp.public_base_url validation error")
	}
	if !strings.Contains(err.Error(), "mcp.public_base_url or console.base_url is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}
