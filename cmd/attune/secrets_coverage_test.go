// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRunSecrets_GenerateKeysetDispatch verifies the "generate-keyset" verb is
// dispatched and the command succeeds (no config required).
func TestRunSecrets_GenerateKeysetDispatch(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return runSecrets([]string{"generate-keyset"})
	})
	require.NoError(t, err)
	trimmed := strings.TrimSpace(out)
	require.True(t, strings.HasPrefix(trimmed, "{"), "output should be a JSON object")
	require.True(t, strings.HasSuffix(trimmed, "}"), "output should be a JSON object")
}

// TestRunSecrets_KeysetInfoDispatch verifies the "keyset-info" verb is
// dispatched and fails at config.Load (no ATTUNE_CONFIG set).
func TestRunSecrets_KeysetInfoDispatch(t *testing.T) {
	err := runSecrets([]string{"keyset-info"})
	require.Error(t, err)
}

// TestRunSecrets_AddKeyDispatch verifies the "add-key" verb is dispatched and
// fails at config.Load (no ATTUNE_CONFIG set).
func TestRunSecrets_AddKeyDispatch(t *testing.T) {
	err := runSecrets([]string{"add-key"})
	require.Error(t, err)
}

// TestRunSecrets_ReencryptDispatch verifies the "reencrypt" verb is dispatched
// and fails inside withSecretRotationService at config.Load.
func TestRunSecrets_ReencryptDispatch(t *testing.T) {
	err := runSecrets([]string{"reencrypt"})
	require.Error(t, err)
}

// TestRunSecretsReencrypt_FlagParse exercises flag parsing with no flags, which
// still reaches withSecretRotationService and fails at config.Load.
func TestRunSecretsReencrypt_FlagParse(t *testing.T) {
	err := runSecretsReencrypt([]string{})
	require.Error(t, err)
}

// TestRunSecretsReencrypt_WithFlags exercises the --apply and --from-key-id
// flags so both bool and string flag paths are exercised before reaching
// withSecretRotationService which fails at config.Load.
func TestRunSecretsReencrypt_WithFlags(t *testing.T) {
	err := runSecretsReencrypt([]string{"--apply", "--from-key-id", "some-key"})
	require.Error(t, err)
}

// TestRunSecretsSetPrimary_ValidKeyID supplies --key-id so the required-arg
// guard is passed; the call then fails inside configuredTinkKeyset at
// config.Load.
func TestRunSecretsSetPrimary_ValidKeyID(t *testing.T) {
	err := runSecretsSetPrimary([]string{"--key-id", "abc"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--key-id is required")
}

// TestRunSecretsDeleteKey_ValidKeyID supplies --key-id so the required-arg
// guard is passed; the call then fails inside configuredTinkKeyset at
// config.Load.
func TestRunSecretsDeleteKey_ValidKeyID(t *testing.T) {
	err := runSecretsDeleteKey([]string{"--key-id", "abc"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--key-id is required")
}

// TestRunSecretsRetireKey_ValidKeyID supplies --key-id so the required-arg
// guard is passed; the call then fails inside withSecretRotationService at
// config.Load.
func TestRunSecretsRetireKey_ValidKeyID(t *testing.T) {
	err := runSecretsRetireKey([]string{"--key-id", "abc"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--key-id is required")
}

// TestRunSecretsGenerateKeyset_ExtraArgs verifies that extra positional
// arguments cause an immediate usage error before any key is generated.
func TestRunSecretsGenerateKeyset_ExtraArgs(t *testing.T) {
	err := runSecretsGenerateKeyset([]string{"extra"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage: attune secrets generate-keyset")
}

// TestRunSecretsKeysetInfo_ExtraArgs verifies that extra positional arguments
// cause a usage error before config.Load is attempted.
func TestRunSecretsKeysetInfo_ExtraArgs(t *testing.T) {
	err := runSecretsKeysetInfo([]string{"extra"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "usage: attune secrets keyset-info")
}

// TestRunSecretsAddKey_FlagParse exercises the --primary flag path; the call
// still fails at configuredTinkKeyset / config.Load.
func TestRunSecretsAddKey_FlagParse(t *testing.T) {
	err := runSecretsAddKey([]string{"--primary"})
	require.Error(t, err)
}

// TestRunSecrets_DefaultUsage verifies that an unrecognised verb returns the
// secrets usage error.
func TestRunSecrets_DefaultUsage(t *testing.T) {
	err := runSecrets([]string{"unknown-verb"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "generate-keyset")
}
