// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// setNoConfig points config.Load at a guaranteed-absent path so unit tests
// that reach connectDatabase get a deterministic "load config" error rather
// than accidentally hitting a real config.yaml.
func setNoConfig(t *testing.T) {
	t.Helper()
	config.SetPath("/nonexistent/no-config-for-tests.yaml")
	t.Cleanup(func() { config.SetPath("") })
}

// ── runMigrationsStatus ───────────────────────────────────────────────────────

func TestRunMigrationsStatus_FlagParse(t *testing.T) {
	err := runMigrationsStatus([]string{"--unknown-flag"})
	require.Error(t, err)
}

func TestRunMigrationsStatus_ConfigFail(t *testing.T) {
	setNoConfig(t)
	err := runMigrationsStatus([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

// ── runMigrationsVerify ───────────────────────────────────────────────────────

func TestRunMigrationsVerify_FlagParse(t *testing.T) {
	err := runMigrationsVerify([]string{"--unknown-flag"})
	require.Error(t, err)
}

func TestRunMigrationsVerify_ConfigFail(t *testing.T) {
	setNoConfig(t)
	err := runMigrationsVerify([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

// ── runMigrationsDryRun ───────────────────────────────────────────────────────

func TestRunMigrationsDryRun_ConfigFail(t *testing.T) {
	setNoConfig(t)
	err := runMigrationsDryRun([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

// ── runMigrationsBaseline ─────────────────────────────────────────────────────

func TestRunMigrationsBaseline_NoVersion(t *testing.T) {
	err := runMigrationsBaseline([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--version is required")
}

func TestRunMigrationsBaseline_VersionOutOfRange(t *testing.T) {
	// Version 99999 is well beyond the actual migration count; LoadMigrationNames
	// reads from the embedded FS so this works without a DB or config file.
	err := runMigrationsBaseline([]string{"--version", "99999"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range")
}

func TestRunMigrationsBaseline_ConfigFail(t *testing.T) {
	setNoConfig(t)
	// Version 1 is in range for any real migration set; gets past the range
	// check and reaches connectDatabase.
	err := runMigrationsBaseline([]string{"--version", "1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

// ── runMigrationsRepair ───────────────────────────────────────────────────────

func TestRunMigrationsRepair_ConfigFail(t *testing.T) {
	setNoConfig(t)
	// --version 1 passes the zero-check; next stop is connectDatabase.
	err := runMigrationsRepair([]string{"--version", "1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

// ── runMigrationsCmd dispatch ─────────────────────────────────────────────────

func TestRunMigrationsCmd_StatusDispatch(t *testing.T) {
	setNoConfig(t)
	err := runMigrationsCmd([]string{"status"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

func TestRunMigrationsCmd_VerifyDispatch(t *testing.T) {
	setNoConfig(t)
	err := runMigrationsCmd([]string{"verify"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

func TestRunMigrationsCmd_DryRunDispatch(t *testing.T) {
	setNoConfig(t)
	err := runMigrationsCmd([]string{"dry-run"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

func TestRunMigrationsCmd_BaselineDispatch(t *testing.T) {
	// baseline with no --version errors before reaching connectDatabase.
	err := runMigrationsCmd([]string{"baseline"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--version is required")
}

// ── verificationResults struct ────────────────────────────────────────────────

func TestVerificationResults_AllPassed_Fields(t *testing.T) {
	r := verificationResults{
		Duplicates: true,
		Checksums:  true,
		Manifest:   true,
		NoTx:       true,
		Passed:     true,
	}
	require.True(t, r.Duplicates)
	require.True(t, r.Checksums)
	require.True(t, r.Manifest)
	require.True(t, r.NoTx)
	require.True(t, r.Passed)
	require.Empty(t, r.DuplicatesError)
	require.Empty(t, r.ChecksumsError)
	require.Empty(t, r.ManifestError)
	require.Empty(t, r.NoTxError)
}

func TestVerificationResults_SomeFailed_Fields(t *testing.T) {
	r := verificationResults{
		Duplicates:     true,
		Checksums:      false,
		ChecksumsError: "checksum mismatch on 003_add_table.sql",
		Manifest:       false,
		ManifestError:  "manifest hash mismatch",
		NoTx:           true,
		Passed:         false,
	}
	require.False(t, r.Passed)
	require.False(t, r.Checksums)
	require.Equal(t, "checksum mismatch on 003_add_table.sql", r.ChecksumsError)
	require.False(t, r.Manifest)
	require.Equal(t, "manifest hash mismatch", r.ManifestError)
	require.True(t, r.Duplicates)
	require.True(t, r.NoTx)
}

// ── connectDatabase ───────────────────────────────────────────────────────────

func TestConnectDatabase_NoConfig(t *testing.T) {
	setNoConfig(t)
	pool, err := connectDatabase(context.Background())
	require.Error(t, err)
	require.Nil(t, pool)
	require.Contains(t, err.Error(), "load config")
}

// ── printVerificationResults (passed path only — failed path calls os.Exit) ──

func TestPrintVerificationResults_AllPassed(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	r := verificationResults{
		Duplicates: true,
		Checksums:  true,
		Manifest:   true,
		NoTx:       true,
		Passed:     true,
	}
	// Must not call os.Exit because Passed == true.
	err := printVerificationResults(r)
	require.NoError(t, err)
}

// ── ptrext import smoke-check ─────────────────────────────────────────────────

func TestMigrationsCoverage_PtrextOf(t *testing.T) {
	// Ensures the ptrext import is exercised and the file compiles.
	v := ptrext.Of(42)
	require.Equal(t, 42, ptrext.Indirect(v))
}
