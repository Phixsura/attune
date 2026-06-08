// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Phixsura/attune/internal/infra/config"
)

func TestGetOrFile_PrefersFileVariant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("from-file-value"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATTUNE_TEST_SECRET_FILE", path)
	t.Setenv("ATTUNE_TEST_SECRET", "from-env-value")
	if got := config.GetOrFile("ATTUNE_TEST_SECRET"); got != "from-file-value" {
		t.Errorf("GetOrFile = %q; want %q", got, "from-file-value")
	}
}

func TestGetOrFile_FallsBackToEnv(t *testing.T) {
	t.Setenv("ATTUNE_TEST_SECRET2", "from-env-only")
	if got := config.GetOrFile("ATTUNE_TEST_SECRET2"); got != "from-env-only" {
		t.Errorf("GetOrFile = %q; want %q", got, "from-env-only")
	}
}

func TestGetOrFile_EmptyWhenNeitherSet(t *testing.T) {
	if got := config.GetOrFile("ATTUNE_NEVER_SET"); got != "" {
		t.Errorf("GetOrFile = %q; want empty", got)
	}
}

func TestGetOrFile_TrimsTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATTUNE_TEST_SECRET3_FILE", path)
	if got := config.GetOrFile("ATTUNE_TEST_SECRET3"); got != "value" {
		t.Errorf("GetOrFile = %q; want %q", got, "value")
	}
}

func TestGetOrFile_FileMissingReturnsEmptyNotEnvFallback(t *testing.T) {
	// Security: if _FILE is set but the file can't be read, do NOT
	// degrade to reading the plain env. A misconfigured secret reference
	// should fail loudly (empty value → bootstrap aborts) rather than
	// silently leak the env-var value.
	t.Setenv("ATTUNE_TEST_SECRET4_FILE", "/nonexistent/path/should/fail")
	t.Setenv("ATTUNE_TEST_SECRET4", "env-fallback-must-not-be-used")
	if got := config.GetOrFile("ATTUNE_TEST_SECRET4"); got != "" {
		t.Errorf("GetOrFile = %q; want empty (security: no env fallback when _FILE set but unreadable)", got)
	}
}
