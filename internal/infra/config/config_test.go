// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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

func TestLoadPathParsesIngestCORSOrigins(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\ningest:\n  cors_allowed_origins:\n    - \"https://App.Example.com/\"\n    - \" https://app.example.com \"\n    - \"http://localhost:5173\"\n"
	cfg, err := LoadPath(writeConfig(t, raw))
	require.NoError(t, err)
	require.Equal(t, []string{"https://app.example.com", "http://localhost:5173"}, cfg.IngestCORSAllowedOrigins)
}

func TestLoadPathNormalizesDefaultPortInIngestCORSOrigins(t *testing.T) {
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\ningest:\n  cors_allowed_origins:\n    - \"https://app.example.com:0443\"\n    - \"http://localhost:080\"\n    - \"https://[::1]:0443\"\n    - \"http://[::1]:8080\"\n"
	cfg, err := LoadPath(writeConfig(t, raw))
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{"https://app.example.com", "http://localhost", "https://[::1]", "http://[::1]:8080"},
		cfg.IngestCORSAllowedOrigins,
	)
}

func TestLoadPathIngestCORSDisabledByDefault(t *testing.T) {
	cfg, err := LoadPath(writeConfig(t, validConfigYAML(t, validTinkKeyset(t))))
	require.NoError(t, err)
	require.Empty(t, cfg.IngestCORSAllowedOrigins)
}

func TestLoadPathRejectsInvalidIngestCORSOrigins(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "path not allowed",
			raw:  validConfigYAML(t, validTinkKeyset(t)) + "\ningest:\n  cors_allowed_origins:\n    - \"https://app.example.com/widget\"\n",
			want: "must not include a path",
		},
		{
			name: "query not allowed",
			raw:  validConfigYAML(t, validTinkKeyset(t)) + "\ningest:\n  cors_allowed_origins:\n    - \"https://app.example.com?x=1\"\n",
			want: "must not include query or fragment",
		},
		{
			name: "scheme required",
			raw:  validConfigYAML(t, validTinkKeyset(t)) + "\ningest:\n  cors_allowed_origins:\n    - \"app.example.com\"\n",
			want: "must include scheme and host",
		},
		{
			name: "http or https only",
			raw:  validConfigYAML(t, validTinkKeyset(t)) + "\ningest:\n  cors_allowed_origins:\n    - \"ftp://app.example.com\"\n",
			want: "must use http or https",
		},
		{
			name: "wildcard mixed with explicit",
			raw:  validConfigYAML(t, validTinkKeyset(t)) + "\ningest:\n  cors_allowed_origins:\n    - \"*\"\n    - \"https://app.example.com\"\n",
			want: "cannot mix \"*\" with explicit origins",
		},
		{
			name: "invalid port rejected",
			raw:  validConfigYAML(t, validTinkKeyset(t)) + "\ningest:\n  cors_allowed_origins:\n    - \"https://app.example.com:99999\"\n",
			want: "must use a valid port in [1, 65535]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadPath(writeConfig(t, tc.raw))
			require.Error(t, err)
			require.Contains(t, err.Error(), "ingest.cors_allowed_origins")
			require.Contains(t, err.Error(), tc.want)
		})
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

// ---------------------------------------------------------------------------
// GDPR validation edge cases
// ---------------------------------------------------------------------------

func TestLoadPathRejectsInvalidGDPRExportTTL(t *testing.T) {
	t.Parallel()
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\ngdpr:\n  export_ttl: \"bad\"\n"
	_, err := LoadPath(writeConfig(t, raw))
	require.Error(t, err)
	require.Contains(t, err.Error(), "gdpr.export_ttl")
}

func TestLoadPathRejectsInvalidGDPRStepUpTTL(t *testing.T) {
	t.Parallel()
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\ngdpr:\n  step_up_ttl: \"bad\"\n"
	_, err := LoadPath(writeConfig(t, raw))
	require.Error(t, err)
	require.Contains(t, err.Error(), "gdpr.step_up_ttl")
}

func TestLoadPathRejectsNonPositiveGDPRExportTTL(t *testing.T) {
	t.Parallel()
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\ngdpr:\n  export_ttl: \"0s\"\n"
	_, err := LoadPath(writeConfig(t, raw))
	require.Error(t, err)
	require.Contains(t, err.Error(), "gdpr.export_ttl")
}

func TestLoadPathRejectsNonPositiveGDPRStepUpTTL(t *testing.T) {
	t.Parallel()
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\ngdpr:\n  step_up_ttl: \"0s\"\n"
	_, err := LoadPath(writeConfig(t, raw))
	require.Error(t, err)
	require.Contains(t, err.Error(), "gdpr.step_up_ttl")
}

// ---------------------------------------------------------------------------
// Worker config validation
// ---------------------------------------------------------------------------

// TestValidateWorkerConfig exercises validateWorkerConfig directly because
// yamlConfig does not expose a workers: key (worker tuning is set via
// applyWorkerDefaults and not yet surfaced in YAML). The internal method is
// accessible because this test file is in the same package.
func TestValidateWorkerConfig(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(c *Config)
		errPart string
	}{
		{
			name:    "zero heartbeat_interval",
			mutate:  func(c *Config) { c.WorkerHeartbeatInterval = 0 },
			errPart: "workers.heartbeat_interval must be positive",
		},
		{
			name:    "zero stale_duration",
			mutate:  func(c *Config) { c.WorkerStaleDuration = 0 },
			errPart: "workers.stale_duration must be positive",
		},
		{
			name:    "zero drain_timeout",
			mutate:  func(c *Config) { c.WorkerDrainTimeout = 0 },
			errPart: "workers.drain_timeout must be positive",
		},
		{
			name:    "zero poll_interval",
			mutate:  func(c *Config) { c.WorkerPollInterval = 0 },
			errPart: "workers.poll_interval must be positive",
		},
		{
			name:    "zero max_attempts",
			mutate:  func(c *Config) { c.WorkerMaxAttempts = 0 },
			errPart: "workers.max_attempts must be positive",
		},
		{
			name:    "negative max_attempts",
			mutate:  func(c *Config) { c.WorkerMaxAttempts = -1 },
			errPart: "workers.max_attempts must be positive",
		},
		{
			name: "heartbeat_interval equals stale_duration",
			mutate: func(c *Config) {
				c.WorkerHeartbeatInterval = 5 * time.Minute
				c.WorkerStaleDuration = 5 * time.Minute
			},
			errPart: "workers.heartbeat_interval",
		},
		{
			name: "heartbeat_interval exceeds stale_duration",
			mutate: func(c *Config) {
				c.WorkerHeartbeatInterval = 10 * time.Minute
				c.WorkerStaleDuration = 5 * time.Minute
			},
			errPart: "workers.heartbeat_interval",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := validWorkerConfig()
			tc.mutate(c)
			err := c.validateWorkerConfig()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errPart)
		})
	}
}

func TestValidateWorkerConfigAcceptsValid(t *testing.T) {
	t.Parallel()
	c := validWorkerConfig()
	require.NoError(t, c.validateWorkerConfig())
}

// validWorkerConfig returns a Config with valid worker fields for direct
// validation testing.
func validWorkerConfig() *Config {
	return &Config{ // ptrext:allow test-fixture
		WorkerHeartbeatInterval: 90 * time.Second,
		WorkerStaleDuration:     5 * time.Minute,
		WorkerDrainTimeout:      30 * time.Second,
		WorkerPollInterval:      5 * time.Second,
		WorkerMaxAttempts:       5,
	}
}

// TestParseWorkerFields exercises parseWorkerFields directly to cover parse
// error branches (workers YAML key is not exposed in yamlConfig).
func TestParseWorkerFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(c *Config)
		errPart string
	}{
		{
			name:    "bad heartbeat_interval",
			mutate:  func(c *Config) { c.Workers.HeartbeatInterval = "bad" },
			errPart: "workers.heartbeat_interval",
		},
		{
			name:    "bad stale_duration",
			mutate:  func(c *Config) { c.Workers.StaleDuration = "bad" },
			errPart: "workers.stale_duration",
		},
		{
			name:    "bad drain_timeout",
			mutate:  func(c *Config) { c.Workers.DrainTimeout = "bad" },
			errPart: "workers.drain_timeout",
		},
		{
			name:    "bad poll_interval",
			mutate:  func(c *Config) { c.Workers.PollInterval = "bad" },
			errPart: "workers.poll_interval",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := &Config{} // ptrext:allow test-fixture
			c.applyWorkerDefaults()
			tc.mutate(c)
			err := c.parseWorkerFields()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errPart)
		})
	}
}

// ---------------------------------------------------------------------------
// Enricher config validation edge cases
// ---------------------------------------------------------------------------

func TestLoadPathRejectsInvalidEnricherEdgeCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		replace string
		errPart string
	}{
		{
			name:    "negative batch",
			replace: "enricher:\n  interval: \"30s\"\n  batch: -1\n",
			errPart: "enricher.batch",
		},
		{
			name:    "negative workers",
			replace: "enricher:\n  interval: \"30s\"\n  workers: -1\n",
			errPart: "enricher.workers",
		},
		{
			name:    "bad batch_window",
			replace: "enricher:\n  interval: \"30s\"\n  batch_window: \"bad\"\n",
			errPart: "enricher.batch_window",
		},
		{
			name:    "zero batch_window",
			replace: "enricher:\n  interval: \"30s\"\n  batch_window: \"0s\"\n",
			errPart: "enricher.batch_window",
		},
		{
			name:    "negative llm_max_qps",
			replace: "enricher:\n  interval: \"30s\"\n  llm_max_qps: -1\n",
			errPart: "enricher.llm_max_qps",
		},
		{
			name:    "negative llm_burst",
			replace: "enricher:\n  interval: \"30s\"\n  llm_burst: -1\n",
			errPart: "enricher.llm_burst",
		},
		{
			name:    "bad interval",
			replace: "enricher:\n  interval: \"bad\"\n",
			errPart: "enricher.interval",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := strings.Replace(
				validConfigYAML(t, validTinkKeyset(t)),
				"enricher:\n  interval: \"30s\"\n",
				tc.replace,
				1,
			)
			_, err := LoadPath(writeConfig(t, raw))
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errPart)
		})
	}
}

func TestLoadPathEnricherLLMBurstDefaultFromQPS(t *testing.T) {
	t.Parallel()
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"enricher:\n  interval: \"30s\"\n",
		"enricher:\n  interval: \"30s\"\n  llm_max_qps: 5\n",
		1,
	)
	cfg, err := LoadPath(writeConfig(t, raw))
	require.NoError(t, err)
	require.Equal(t, 5, cfg.EnricherLLMBurst, "llm_burst should default to int(llm_max_qps)")
}

func TestLoadPathEnricherLLMBurstDefaultMinOne(t *testing.T) {
	t.Parallel()
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"enricher:\n  interval: \"30s\"\n",
		"enricher:\n  interval: \"30s\"\n  llm_max_qps: 0.5\n",
		1,
	)
	cfg, err := LoadPath(writeConfig(t, raw))
	require.NoError(t, err)
	require.Equal(t, 1, cfg.EnricherLLMBurst, "llm_burst should be at least 1 when qps > 0")
}

// ---------------------------------------------------------------------------
// Console validation edge cases
// ---------------------------------------------------------------------------

func TestLoadPathRejectsConsoleEdgeCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		consoleYAML string
		errPart     string
	}{
		{
			name:        "base_url without session_key",
			consoleYAML: "console:\n  base_url: \"https://attune.example.com\"\n",
			errPart:     "console.base_url and console.session_key must be set together",
		},
		{
			name:        "session_key without base_url",
			consoleYAML: "console:\n  session_key: \"01234567890123456789012345678901\"\n",
			errPart:     "console.base_url and console.session_key must be set together",
		},
		{
			name: "session_key too short",
			consoleYAML: "console:\n  base_url: \"https://attune.example.com\"\n  session_key: \"short\"\n" +
				"  bootstrap_admin:\n    email: \"a@b.com\"\n    password: \"correct horse battery staple\"\n",
			errPart: "console.session_key must be at least 32 bytes",
		},
		{
			name: "bootstrap email without password",
			consoleYAML: "console:\n  base_url: \"https://attune.example.com\"\n  session_key: \"01234567890123456789012345678901\"\n" +
				"  bootstrap_admin:\n    email: \"admin@example.com\"\n",
			errPart: "console.bootstrap_admin.password is required",
		},
		{
			name: "bootstrap password without email",
			consoleYAML: "console:\n  base_url: \"https://attune.example.com\"\n  session_key: \"01234567890123456789012345678901\"\n" +
				"  bootstrap_admin:\n    password: \"correct horse battery staple\"\n",
			errPart: "console.bootstrap_admin.email is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw := strings.Replace(
				validConfigYAML(t, validTinkKeyset(t)),
				"console:\n  base_url: \"https://attune.example.com\"\n  session_key: \"01234567890123456789012345678901\"\n  bootstrap_admin:\n    email: \"admin@example.com\"\n    password: \"correct horse battery staple\"\n",
				tc.consoleYAML,
				1,
			)
			_, err := LoadPath(writeConfig(t, raw))
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.errPart)
		})
	}
}

func TestLoadPathAcceptsConsoleWithoutBootstrapAdmin(t *testing.T) {
	t.Parallel()
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"console:\n  base_url: \"https://attune.example.com\"\n  session_key: \"01234567890123456789012345678901\"\n  bootstrap_admin:\n    email: \"admin@example.com\"\n    password: \"correct horse battery staple\"\n",
		"console:\n  base_url: \"https://attune.example.com\"\n  session_key: \"01234567890123456789012345678901\"\n",
		1,
	)
	cfg, err := LoadPath(writeConfig(t, raw))
	require.NoError(t, err)
	require.Empty(t, cfg.Console.BootstrapAdmin.Email)
	require.Empty(t, cfg.Console.BootstrapAdmin.Password)
}

func TestLoadPathAcceptsNoConsoleBlock(t *testing.T) {
	t.Parallel()
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"console:\n  base_url: \"https://attune.example.com\"\n  session_key: \"01234567890123456789012345678901\"\n  bootstrap_admin:\n    email: \"admin@example.com\"\n    password: \"correct horse battery staple\"\n",
		"",
		1,
	)
	cfg, err := LoadPath(writeConfig(t, raw))
	require.NoError(t, err)
	require.Empty(t, cfg.ConsoleBaseURL)
	require.Empty(t, cfg.ConsoleSessionKey)
}

// ---------------------------------------------------------------------------
// Shutdown parse edge cases
// ---------------------------------------------------------------------------

func TestLoadPathRejectsUnparseableShutdownTimeout(t *testing.T) {
	t.Parallel()
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"observability:\n",
		"shutdown:\n  timeout: \"bad\"\nobservability:\n",
		1,
	)
	_, err := LoadPath(writeConfig(t, raw))
	require.Error(t, err)
	require.Contains(t, err.Error(), "shutdown.timeout")
}

func TestLoadPathRejectsUnparseableShutdownDrainDelay(t *testing.T) {
	t.Parallel()
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"observability:\n",
		"shutdown:\n  drain_delay: \"bad\"\nobservability:\n",
		1,
	)
	_, err := LoadPath(writeConfig(t, raw))
	require.Error(t, err)
	require.Contains(t, err.Error(), "shutdown.drain_delay")
}

// ---------------------------------------------------------------------------
// Audit parse/validate edge cases
// ---------------------------------------------------------------------------

func TestLoadPathRejectsUnparseableAuditPruneInterval(t *testing.T) {
	t.Parallel()
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\naudit:\n  prune_interval: \"not-a-duration\"\n"
	_, err := LoadPath(writeConfig(t, raw))
	require.Error(t, err)
	require.Contains(t, err.Error(), "audit.prune_interval")
}

func TestLoadPathRejectsZeroAuditPruneInterval(t *testing.T) {
	t.Parallel()
	raw := validConfigYAML(t, validTinkKeyset(t)) + "\naudit:\n  prune_interval: \"0s\"\n"
	_, err := LoadPath(writeConfig(t, raw))
	require.Error(t, err)
	require.Contains(t, err.Error(), "audit.prune_interval")
}

// ---------------------------------------------------------------------------
// Missing database URL
// ---------------------------------------------------------------------------

func TestLoadPathRejectsMissingDatabaseURL(t *testing.T) {
	t.Parallel()
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"database:\n  url: \"postgres://attune:test@localhost:5432/attune?sslmode=disable\"\n",
		"database:\n  url: \"\"\n",
		1,
	)
	_, err := LoadPath(writeConfig(t, raw))
	require.Error(t, err)
	require.Contains(t, err.Error(), "database.url")
}

// ---------------------------------------------------------------------------
// Missing / empty tink keyset
// ---------------------------------------------------------------------------

func TestLoadPathRejectsMissingTinkKeyset(t *testing.T) {
	t.Parallel()
	raw := validConfigYAML(t, "   ")
	_, err := LoadPath(writeConfig(t, raw))
	require.Error(t, err)
	require.Contains(t, err.Error(), "secrets.tink_keyset")
}

// ---------------------------------------------------------------------------
// Default port
// ---------------------------------------------------------------------------

func TestLoadPathUsesDefaultPort(t *testing.T) {
	t.Parallel()
	raw := strings.Replace(
		validConfigYAML(t, validTinkKeyset(t)),
		"port: 8090\n",
		"",
		1,
	)
	cfg, err := LoadPath(writeConfig(t, raw))
	require.NoError(t, err)
	require.Equal(t, DefaultPort, cfg.Port)
}

// ---------------------------------------------------------------------------
// Empty path fallback
// ---------------------------------------------------------------------------

func TestLoadPathEmptyStringFallsBackToDefault(t *testing.T) {
	t.Parallel()
	_, err := LoadPath("   ")
	require.Error(t, err, "empty path should try default file which does not exist")
}

// ---------------------------------------------------------------------------
// Worker defaults verification
// ---------------------------------------------------------------------------

func TestLoadPathWorkerDefaults(t *testing.T) {
	t.Parallel()
	raw := validConfigYAML(t, validTinkKeyset(t))
	cfg, err := LoadPath(writeConfig(t, raw))
	require.NoError(t, err)
	require.Equal(t, DefaultWorkerHeartbeatInterval, cfg.WorkerHeartbeatInterval)
	require.Equal(t, DefaultWorkerStaleDuration, cfg.WorkerStaleDuration)
	require.Equal(t, DefaultWorkerDrainTimeout, cfg.WorkerDrainTimeout)
	require.Equal(t, DefaultWorkerPollInterval, cfg.WorkerPollInterval)
	require.Equal(t, DefaultWorkerMaxAttempts, cfg.WorkerMaxAttempts)
}

func TestIsAllowedWebhookURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		url  string
		want bool
	}{
		{"https://example.com/hook", true},
		{"http://example.com/hook", false},
		{"http://127.0.0.1:8080/hook", true},
		{"http://localhost:9090/hook", true},
		{"http://[::1]:8080/hook", true},
		{"ftp://example.com", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isAllowedWebhookURL(c.url); got != c.want {
			t.Errorf("isAllowedWebhookURL(%q) = %v, want %v", c.url, got, c.want)
		}
	}
}

func TestHasPrivateIPPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		host string
		want bool
	}{
		{"10.0.0.1", true},
		{"192.168.1.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"172.15.0.1", false},
		{"172.32.0.1", false},
		{"8.8.8.8", false},
		{"example.com", false},
	}
	for _, c := range cases {
		if got := hasPrivateIPPrefix(c.host); got != c.want {
			t.Errorf("hasPrivateIPPrefix(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestParseAuditEvidenceFields_ValidTTL(t *testing.T) {
	t.Parallel()
	c := &Config{}
	c.AuditEvidence.ExportTTL = "24h"
	err := c.parseAuditEvidenceFields()
	require.NoError(t, err)
	require.Equal(t, 24*time.Hour, c.AuditEvidenceExportTTL)
}

func TestParseAuditEvidenceFields_InvalidTTL(t *testing.T) {
	t.Parallel()
	c := &Config{}
	c.AuditEvidence.ExportTTL = "bad"
	err := c.parseAuditEvidenceFields()
	require.Error(t, err)
	require.Contains(t, err.Error(), "audit_evidence.export_ttl")
}

func TestParseAuditEvidenceFields_ValidSigningKey(t *testing.T) {
	t.Parallel()
	seed := strings.Repeat("ab", 32) // 64 hex chars = 32 bytes
	c := &Config{}
	c.AuditEvidence.ExportTTL = "1h"
	c.AuditEvidence.SigningKey = seed
	err := c.parseAuditEvidenceFields()
	require.NoError(t, err)
	require.Len(t, c.AuditEvidenceSigningKey, 32)
}

func TestParseAuditEvidenceFields_InvalidHex(t *testing.T) {
	t.Parallel()
	c := &Config{}
	c.AuditEvidence.ExportTTL = "1h"
	c.AuditEvidence.SigningKey = "not-hex"
	err := c.parseAuditEvidenceFields()
	require.Error(t, err)
	require.Contains(t, err.Error(), "hex-encoded")
}

func TestParseAuditEvidenceFields_ShortKey(t *testing.T) {
	t.Parallel()
	shortKey := hex.EncodeToString([]byte("short"))
	c := &Config{}
	c.AuditEvidence.ExportTTL = "1h"
	c.AuditEvidence.SigningKey = shortKey
	err := c.parseAuditEvidenceFields()
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least 32 bytes")
}

func TestParseAuditEvidenceFields_EmptyKeyOK(t *testing.T) {
	t.Parallel()
	c := &Config{}
	c.AuditEvidence.ExportTTL = "1h"
	err := c.parseAuditEvidenceFields()
	require.NoError(t, err)
	require.Nil(t, c.AuditEvidenceSigningKey)
}

func TestValidateMCPConfig_Disabled(t *testing.T) {
	t.Parallel()
	c := &Config{}
	require.NoError(t, c.validateMCPConfig())
}

func TestValidateMCPConfig_EmptyJWTSecret(t *testing.T) {
	t.Parallel()
	c := &Config{MCPEnabled: true}
	c.MCP.Enabled = true
	err := c.validateMCPConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "jwt_secret is required")
}

func TestValidateMCPConfig_ShortJWTSecret(t *testing.T) {
	t.Parallel()
	c := &Config{MCPEnabled: true}
	c.MCP.Enabled = true
	c.MCP.OAuth.JWTSecret = "too-short"
	err := c.validateMCPConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least 32 bytes")
}

func TestValidateMCPConfig_PlaceholderJWTSecret(t *testing.T) {
	t.Parallel()
	c := &Config{MCPEnabled: true}
	c.MCP.Enabled = true
	c.MCP.OAuth.JWTSecret = "replace-with-32-or-more-random-characters"
	err := c.validateMCPConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "replaced with a random value")
}

func TestValidateMCPConfig_NegativeAccessTokenTTL(t *testing.T) {
	t.Parallel()
	c := &Config{MCPEnabled: true}
	c.MCP.Enabled = true
	c.MCP.OAuth.JWTSecret = strings.Repeat("x", 32)
	c.MCPAccessTokenTTL = -1
	err := c.validateMCPConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "access_token_ttl must be positive")
}

func TestValidateMCPConfig_NegativeRefreshTokenTTL(t *testing.T) {
	t.Parallel()
	c := &Config{MCPEnabled: true}
	c.MCP.Enabled = true
	c.MCP.OAuth.JWTSecret = strings.Repeat("x", 32)
	c.MCPAccessTokenTTL = time.Hour
	c.MCPRefreshTokenTTL = -1
	err := c.validateMCPConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "refresh_token_ttl must be positive")
}

func TestValidateMCPConfig_NegativeRateLimit(t *testing.T) {
	t.Parallel()
	c := &Config{MCPEnabled: true}
	c.MCP.Enabled = true
	c.MCP.OAuth.JWTSecret = strings.Repeat("x", 32)
	c.MCPAccessTokenTTL = time.Hour
	c.MCPRefreshTokenTTL = time.Hour
	c.MCPRateLimitPerMinute = -1
	err := c.validateMCPConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "requests_per_minute must be positive")
}

func TestValidateMCPConfig_NegativeBurst(t *testing.T) {
	t.Parallel()
	c := &Config{MCPEnabled: true}
	c.MCP.Enabled = true
	c.MCP.OAuth.JWTSecret = strings.Repeat("x", 32)
	c.MCPAccessTokenTTL = time.Hour
	c.MCPRefreshTokenTTL = time.Hour
	c.MCPRateLimitPerMinute = 100
	c.MCPRateLimitBurst = -1
	err := c.validateMCPConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "burst must be positive")
}

func TestValidateMCPConfig_MissingPublicBaseURL(t *testing.T) {
	t.Parallel()
	c := &Config{MCPEnabled: true}
	c.MCP.Enabled = true
	c.MCP.OAuth.JWTSecret = strings.Repeat("x", 32)
	c.MCPAccessTokenTTL = time.Hour
	c.MCPRefreshTokenTTL = time.Hour
	c.MCPRateLimitPerMinute = 100
	c.MCPRateLimitBurst = 10
	err := c.validateMCPConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "public_base_url")
}

func TestValidateMCPConfig_InvalidPublicBaseURL(t *testing.T) {
	t.Parallel()
	c := &Config{MCPEnabled: true}
	c.MCP.Enabled = true
	c.MCP.OAuth.JWTSecret = strings.Repeat("x", 32)
	c.MCPAccessTokenTTL = time.Hour
	c.MCPRefreshTokenTTL = time.Hour
	c.MCPRateLimitPerMinute = 100
	c.MCPRateLimitBurst = 10
	c.MCPPublicBaseURL = "not a valid url"
	err := c.validateMCPConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "valid URL")
}

func TestValidateConsole_MismatchedBaseAndSession(t *testing.T) {
	t.Parallel()
	c := &Config{ConsoleBaseURL: "https://example.com"}
	err := c.validateConsole()
	require.Error(t, err)
	require.Contains(t, err.Error(), "set together")
}

func TestValidateConsole_ShortSessionKey(t *testing.T) {
	t.Parallel()
	c := &Config{
		ConsoleBaseURL:    "https://example.com",
		ConsoleSessionKey: "short",
	}
	err := c.validateConsole()
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least 32 bytes")
}

func TestValidateConsole_PlaceholderSessionKey(t *testing.T) {
	t.Parallel()
	c := &Config{
		ConsoleBaseURL:    "https://example.com",
		ConsoleSessionKey: "replace-with-32-or-more-random-characters",
	}
	err := c.validateConsole()
	require.Error(t, err)
	require.Contains(t, err.Error(), "replaced with a random value")
}

func TestValidateConsole_PasswordWithoutEmail(t *testing.T) {
	t.Parallel()
	c := &Config{
		ConsoleBaseURL:    "https://example.com",
		ConsoleSessionKey: strings.Repeat("x", 32),
	}
	c.Console.BootstrapAdmin.Password = "secret123"
	err := c.validateConsole()
	require.Error(t, err)
	require.Contains(t, err.Error(), "email is required")
}

func TestValidateConsole_EmailWithoutPassword(t *testing.T) {
	t.Parallel()
	c := &Config{
		ConsoleBaseURL:    "https://example.com",
		ConsoleSessionKey: strings.Repeat("x", 32),
	}
	c.Console.BootstrapAdmin.Email = "admin@example.com"
	err := c.validateConsole()
	require.Error(t, err)
	require.Contains(t, err.Error(), "password is required")
}

func TestValidateConsole_PlaceholderPassword(t *testing.T) {
	t.Parallel()
	c := &Config{
		ConsoleBaseURL:    "https://example.com",
		ConsoleSessionKey: strings.Repeat("x", 32),
	}
	c.Console.BootstrapAdmin.Email = "admin@example.com"
	c.Console.BootstrapAdmin.Password = "replace-this-after-first-login"
	err := c.validateConsole()
	require.Error(t, err)
	require.Contains(t, err.Error(), "replaced before startup")
}

func TestValidateGDPRConfig_AllZero(t *testing.T) {
	t.Parallel()
	c := &Config{}
	err := c.validateGDPRConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "export_ttl must be positive")
}

func TestValidateGDPRConfig_ZeroStepUpTTL(t *testing.T) {
	t.Parallel()
	c := &Config{GDPRExportTTL: time.Hour}
	err := c.validateGDPRConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "step_up_ttl must be positive")
}

func TestValidateGDPRConfig_ZeroDeleteGrace(t *testing.T) {
	t.Parallel()
	c := &Config{GDPRExportTTL: time.Hour, GDPRStepUpTTL: time.Hour}
	err := c.validateGDPRConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "delete_grace_window must be positive")
}

func TestValidateGDPRConfig_AllValid(t *testing.T) {
	t.Parallel()
	c := &Config{
		GDPRExportTTL:         time.Hour,
		GDPRStepUpTTL:         time.Minute,
		GDPRDeleteGraceWindow: 24 * time.Hour,
	}
	require.NoError(t, c.validateGDPRConfig())
}

func TestValidateAuditEvidenceConfig_PositiveTTL(t *testing.T) {
	t.Parallel()
	c := &Config{}
	c.AuditEvidenceExportTTL = time.Hour
	require.NoError(t, c.validateAuditEvidenceConfig())
}

func TestValidateAuditEvidenceConfig_ZeroTTL(t *testing.T) {
	t.Parallel()
	c := &Config{}
	err := c.validateAuditEvidenceConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be positive")
}
