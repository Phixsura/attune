// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/secretstore"
)

func TestCLI_DB_BreakglassIssue_ReachesTenantResolution(t *testing.T) {
	setCLIConfig(t)

	err := runBreakglassIssue([]string{
		"--admin", "admin@example.com",
		"--tenant", "demo",
		"--ttl", "30m",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), `tenant "demo" not found`)
}

func TestCLI_DB_BreakglassIssue_DefaultTenantPath(t *testing.T) {
	setCLIConfig(t)

	err := runBreakglassIssue([]string{
		"--admin", "admin@example.com",
		"--ttl", "30m",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no tenant found")
}

func TestCLI_DB_BreakglassList_ReachesTenantResolution(t *testing.T) {
	setCLIConfig(t)

	err := runBreakglassList(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no tenant found")
}

func TestCLI_DB_BreakglassRevoke_ReachesTenantResolution(t *testing.T) {
	setCLIConfig(t)

	err := runBreakglassRevoke([]string{
		"--id", "token-1",
		"--tenant", "demo",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), `tenant "demo" not found`)
}

func TestCLI_DB_LLMChannelsList_ReachesServiceSetup(t *testing.T) {
	setCLIConfig(t)

	err := runLLMChannels([]string{"list"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "load config")
}

func TestCLI_DB_LLMAbilitiesList_ReachesServiceSetup(t *testing.T) {
	setCLIConfig(t)

	err := runLLMAbilitiesList([]string{"--channel", testUUID})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "load config")
}

func TestCLI_DB_LLMRoutesList_ReachesServiceSetup(t *testing.T) {
	setCLIConfig(t)

	err := runLLMRoutes([]string{"list"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "load config")
}

func setCLIConfig(t *testing.T) {
	t.Helper()

	keyset, err := secretstore.GenerateAES256GCMKeysetJSON()
	require.NoError(t, err)

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	escapedKeyset := strings.ReplaceAll(keyset, "\n", "\n    ")
	body := "database:\n" +
		"  url: \"postgres://attune@127.0.0.1:1/attune?sslmode=disable\"\n" +
		"console:\n" +
		"  base_url: \"http://127.0.0.1:8090\"\n" +
		"  session_key: \"01234567890123456789012345678901\"\n" +
		"  bootstrap_admin:\n" +
		"    email: \"admin@example.com\"\n" +
		"    password: \"a-strong-password-1234567890\"\n" +
		"secrets:\n" +
		"  tink_keyset: |\n" +
		"    " + escapedKeyset + "\n"

	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	config.SetPath(path)
	t.Cleanup(func() { config.SetPath("") })
}
