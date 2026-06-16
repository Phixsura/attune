// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if cfg.Audit.RetentionDays != DefaultAuditRetentionDays {
		t.Fatalf("Audit.RetentionDays = %d", cfg.Audit.RetentionDays)
	}
	if cfg.AuditPruneInterval != DefaultAuditPruneInterval {
		t.Fatalf("AuditPruneInterval = %s", cfg.AuditPruneInterval)
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
