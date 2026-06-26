// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test fixtures

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/restoredrill"
)

// cliExtraTestUUID is a well-formed UUID used by tests in this file.
const cliExtraTestUUID = "550e8400-e29b-41d4-a716-446655440000"

// cliExtraSetNoConfig points config.Load at a guaranteed-absent path so unit
// tests that reach connectDatabase get a deterministic "load config" error.
func cliExtraSetNoConfig(t *testing.T) {
	t.Helper()
	config.SetPath("/nonexistent/no-config-for-extra-tests.yaml")
	t.Cleanup(func() { config.SetPath("") })
}

// ── runLLMChannelUpdate — config load and flag paths ─────────────────────────

func TestCLIExtra_LLMChannelUpdate_AllFlagsReachConfigLoad(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runLLMChannelUpdate([]string{
		"--id", cliExtraTestUUID,
		"--name", "new-name",
		"--protocol", "anthropic",
		"--base-url", "https://api.anthropic.com",
		"--auth-mode", "none",
		"--status", "disabled",
		"--priority", "5",
		"--weight", "3",
		"--timeout-seconds", "120",
		"--api-key", "sk-test",
	})
	require.Error(t, err)
	// Should fail at config.Load, not flag parsing.
	require.NotContains(t, err.Error(), "flag")
}

func TestCLIExtra_LLMChannelUpdate_NoIDDefaultUUID(t *testing.T) {
	// No --id flag: uuid.Parse("") fails.
	err := runLLMChannelUpdate([]string{"--name", "new-name"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--id must be a UUID")
}

// ── runLLMChannelCreate — all flags ──────────────────────────────────────────

func TestCLIExtra_LLMChannelCreate_AllFlags(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runLLMChannelCreate([]string{
		"--name", "full-channel",
		"--protocol", "gemini",
		"--base-url", "https://gemini.example.com",
		"--auth-mode", "bearer",
		"--status", "draining",
		"--priority", "10",
		"--weight", "5",
		"--timeout-seconds", "90",
		"--api-key", "sk-gemini-key",
	})
	require.Error(t, err)
	// Reaches config.Load, not flag parse.
	require.NotContains(t, err.Error(), "flag")
}

// ── runLLMChannelGet — empty ID ──────────────────────────────────────────────

func TestCLIExtra_LLMChannelGet_EmptyID(t *testing.T) {
	err := runLLMChannelGet([]string{"--id", ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--id must be a UUID")
}

func TestCLIExtra_LLMChannelGet_NoFlag(t *testing.T) {
	err := runLLMChannelGet([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--id must be a UUID")
}

// ── runLLMChannelTest — extra flag paths ─────────────────────────────────────

func TestCLIExtra_LLMChannelTest_WithPromptAndModel(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runLLMChannelTest([]string{
		"--id", cliExtraTestUUID,
		"--provider-model", "gpt-4o",
		"--prompt", "Hello, world!",
	})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "flag")
	require.NotContains(t, err.Error(), "--id must be a UUID")
}

func TestCLIExtra_LLMChannelTest_EmptyID(t *testing.T) {
	err := runLLMChannelTest([]string{"--id", ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--id must be a UUID")
}

func TestCLIExtra_LLMChannelTest_NoFlags(t *testing.T) {
	err := runLLMChannelTest([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--id must be a UUID")
}

// ── runLLMAbilitiesList — empty channel ──────────────────────────────────────

func TestCLIExtra_LLMAbilitiesList_EmptyChannel(t *testing.T) {
	err := runLLMAbilitiesList([]string{"--channel", ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--channel must be a UUID")
}

func TestCLIExtra_LLMAbilitiesList_NoFlags(t *testing.T) {
	err := runLLMAbilitiesList([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--channel must be a UUID")
}

// ── runLLMRouteUpsert — tenant and enabled flag paths ────────────────────────

func TestCLIExtra_LLMRouteUpsert_AllFlags(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runLLMRouteUpsert([]string{
		"--tenant", "demo",
		"--purpose", "eval",
		"--logical-model", "claude-opus-4-20250514",
		"--enabled=false",
	})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "flag")
}

func TestCLIExtra_LLMRouteUpsert_PurposeOnly(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runLLMRouteUpsert([]string{"--purpose", "eval"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "flag")
}

// ── runLLMRouteDelete — tenant flag ──────────────────────────────────────────

func TestCLIExtra_LLMRouteDelete_WithTenant(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runLLMRouteDelete([]string{"--tenant", "demo", "--purpose", "eval"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "flag")
}

// ── runLLMChannelDelete — empty ID ───────────────────────────────────────────

func TestCLIExtra_LLMChannelDelete_EmptyID(t *testing.T) {
	err := runLLMChannelDelete([]string{"--id", ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--id must be a UUID")
}

func TestCLIExtra_LLMChannelDelete_NoFlags(t *testing.T) {
	err := runLLMChannelDelete([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--id must be a UUID")
}

// ── runLLMAbilityUpsert — empty channel ──────────────────────────────────────

func TestCLIExtra_LLMAbilityUpsert_EmptyChannel(t *testing.T) {
	err := runLLMAbilityUpsert([]string{
		"--channel", "",
		"--logical-model", "gpt-4",
		"--provider-model", "gpt-4",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--channel must be a UUID")
}

func TestCLIExtra_LLMAbilityUpsert_AllFlags(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runLLMAbilityUpsert([]string{
		"--channel", cliExtraTestUUID,
		"--logical-model", "gpt-4o",
		"--provider-model", "gpt-4o-2025-05-13",
		"--enabled=false",
		"--priority", "3",
		"--weight", "7",
	})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--channel must be a UUID")
}

// ── runLLMAbilityDelete — empty channel ──────────────────────────────────────

func TestCLIExtra_LLMAbilityDelete_EmptyChannel(t *testing.T) {
	err := runLLMAbilityDelete([]string{"--channel", "", "--logical-model", "gpt-4"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--channel must be a UUID")
}

func TestCLIExtra_LLMAbilityDelete_NoFlags(t *testing.T) {
	err := runLLMAbilityDelete([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--channel must be a UUID")
}

// ── runEval — config load paths ──────────────────────────────────────────────

func TestCLIExtra_Eval_WithSinceFlag(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runEval([]string{"--mode", "consistency", "--since", "2026-05-01", "--sample", "10"})
	require.Error(t, err)
}

func TestCLIExtra_Eval_WithOutputFlag(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runEval([]string{"--mode", "consistency", "--output", "/tmp/test-report.md"})
	require.Error(t, err)
}

func TestCLIExtra_Eval_ExportForHumanNoTenant(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runEval([]string{"--mode", "export-for-human", "--sample", "10"})
	require.Error(t, err)
}

func TestCLIExtra_Eval_ScoreHumanNoTenant(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runEval([]string{"--mode", "score-human"})
	require.Error(t, err)
}

func TestCLIExtra_Eval_AllFlagsConsistency(t *testing.T) {
	cliExtraSetNoConfig(t)
	dir := t.TempDir()
	err := runEval([]string{
		"--mode", "consistency",
		"--since", "2026-01-01T00:00:00Z",
		"--sample", "25",
		"--output", dir + "/eval-out.md",
	})
	require.Error(t, err)
}

func TestCLIExtra_Eval_ExportWithTenant(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runEval([]string{"--mode", "export-for-human", "--tenant", "demo", "--sample", "5"})
	require.Error(t, err)
}

func TestCLIExtra_Eval_ScoreHumanWithTenant(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runEval([]string{
		"--mode", "score-human",
		"--tenant", "demo",
		"--input", "/tmp/labels.csv",
	})
	require.Error(t, err)
}

func TestCLIExtra_Eval_UnknownMode(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runEval([]string{"--mode", "nonexistent"})
	require.Error(t, err)
}

// ── runSecretsSetPrimary — flag parse error ──────────────────────────────────

func TestCLIExtra_SecretsSetPrimary_FlagParseError(t *testing.T) {
	err := runSecretsSetPrimary([]string{"--unknown-flag"})
	require.Error(t, err)
}

func TestCLIExtra_SecretsSetPrimary_EmptyKeyID(t *testing.T) {
	err := runSecretsSetPrimary([]string{"--key-id", ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--key-id is required")
}

// ── runSecretsDeleteKey — flag parse error ────────────────────────────────────

func TestCLIExtra_SecretsDeleteKey_FlagParseError(t *testing.T) {
	err := runSecretsDeleteKey([]string{"--unknown-flag"})
	require.Error(t, err)
}

func TestCLIExtra_SecretsDeleteKey_EmptyKeyID(t *testing.T) {
	err := runSecretsDeleteKey([]string{"--key-id", ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--key-id is required")
}

// ── runSecretsRetireKey — flag parse and apply paths ─────────────────────────

func TestCLIExtra_SecretsRetireKey_FlagParseError(t *testing.T) {
	err := runSecretsRetireKey([]string{"--unknown-flag"})
	require.Error(t, err)
}

func TestCLIExtra_SecretsRetireKey_WithApply(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runSecretsRetireKey([]string{"--key-id", "key123", "--apply"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--key-id is required")
}

func TestCLIExtra_SecretsRetireKey_EmptyKeyID(t *testing.T) {
	err := runSecretsRetireKey([]string{"--key-id", ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--key-id is required")
}

// ── runSecretsReencrypt — flag parse error ────────────────────────────────────

func TestCLIExtra_SecretsReencrypt_FlagParseError(t *testing.T) {
	err := runSecretsReencrypt([]string{"--unknown-flag"})
	require.Error(t, err)
}

// ── runSecretsAddKey — flag parse error ──────────────────────────────────────

func TestCLIExtra_SecretsAddKey_FlagParseError(t *testing.T) {
	err := runSecretsAddKey([]string{"--unknown-flag"})
	require.Error(t, err)
}

// ── runEmbedBackfill — config load path ──────────────────────────────────────

func TestCLIExtra_EmbedBackfill_SetNoConfig(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runEmbedBackfill([]string{"--tenant", "demo", "--batch", "200"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--tenant is required")
}

func TestCLIExtra_EmbedBackfill_AllFlags(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runEmbedBackfill([]string{"--tenant", "demo", "--batch", "500", "--force"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--tenant is required")
}

func TestCLIExtra_EmbedBackfill_EmptyTenant(t *testing.T) {
	err := runEmbedBackfill([]string{"--tenant", ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--tenant is required")
}

// ── runEmbedReset — config load path ─────────────────────────────────────────

func TestCLIExtra_EmbedReset_SetNoConfig(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runEmbedReset([]string{"--tenant", "demo"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--tenant is required")
}

func TestCLIExtra_EmbedReset_EmptyTenant(t *testing.T) {
	err := runEmbedReset([]string{"--tenant", ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--tenant is required")
}

// ── runEmbedStatus — config load path ────────────────────────────────────────

func TestCLIExtra_EmbedStatus_SetNoConfig(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runEmbedStatus([]string{"--tenant", "demo"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "--tenant is required")
}

func TestCLIExtra_EmbedStatus_EmptyTenant(t *testing.T) {
	err := runEmbedStatus([]string{"--tenant", ""})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--tenant is required")
}

// ── runRestoreDrillStatus — format variants ──────────────────────────────────

func TestCLIExtra_DrillStatus_JSONFormat(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runRestoreDrillStatus([]string{"--format", "json"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

func TestCLIExtra_DrillStatus_TextFormat(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runRestoreDrillStatus([]string{"--format", "text"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

// ── runRestoreDrillHistory — flag variants ───────────────────────────────────

func TestCLIExtra_DrillHistory_JSONFormat(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runRestoreDrillHistory([]string{"--format", "json"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

func TestCLIExtra_DrillHistory_WithLimit(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runRestoreDrillHistory([]string{"--limit", "5"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

func TestCLIExtra_DrillHistory_AllFlags(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runRestoreDrillHistory([]string{"--limit", "10", "--format", "json"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

// ── runRestoreDrillCmd — help/unknown paths ──────────────────────────────────

func TestCLIExtra_DrillCmd_HelpFlag(t *testing.T) {
	// No t.Parallel(): writes to os.Stderr.
	err := runRestoreDrillCmd([]string{"help"})
	require.NoError(t, err)
}

func TestCLIExtra_DrillCmd_DashH(t *testing.T) {
	// No t.Parallel(): writes to os.Stderr.
	err := runRestoreDrillCmd([]string{"-h"})
	require.NoError(t, err)
}

func TestCLIExtra_DrillCmd_DashDashHelp(t *testing.T) {
	// No t.Parallel(): writes to os.Stderr.
	err := runRestoreDrillCmd([]string{"--help"})
	require.NoError(t, err)
}

func TestCLIExtra_DrillCmd_NoArgs(t *testing.T) {
	// No t.Parallel(): writes to os.Stderr.
	err := runRestoreDrillCmd([]string{})
	require.NoError(t, err)
}

func TestCLIExtra_DrillCmd_UnknownSubcmd(t *testing.T) {
	err := runRestoreDrillCmd([]string{"bogus"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown subcommand: bogus")
}

// ── runRestoreDrillRun — config load and flag paths ──────────────────────────

func TestCLIExtra_DrillRun_TargetReachesConfigLoad(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runRestoreDrillRun([]string{"--target", "postgres://localhost/restored"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

func TestCLIExtra_DrillRun_RestoreFromPsqlTool(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runRestoreDrillRun([]string{
		"--restore-from", "backup.dump",
		"--admin-url", "postgres://admin@localhost/postgres",
		"--restore-tool", "psql",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

func TestCLIExtra_DrillRun_RestoreFromPgRestore(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runRestoreDrillRun([]string{
		"--restore-from", "backup.dump",
		"--admin-url", "postgres://admin@localhost/postgres",
		"--restore-tool", "pg_restore",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

func TestCLIExtra_DrillRun_WithAllOptionalFlags(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runRestoreDrillRun([]string{
		"--target", "postgres://localhost/restored",
		"--baseline-url", "",
		"--backup-ref", "daily-2026-06-27",
		"--backup-taken-at", "2026-06-27T00:00:00Z",
		"--restore-duration", "5m30s",
		"--rpo-target", "24h",
		"--rto-target", "30m",
		"--record",
		"--deep",
		"--warn-exit",
		"--format", "json",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

// NOTE: runRestoreDrillRun calls config.Load() before withRecoveryOpts, so
// invalid duration/time flags cannot be tested through runRestoreDrillRun
// without a running config. withRecoveryOpts error paths are tested directly
// via TestCLIExtra_WithRecoveryOpts_Bad* below.

// ── finalizeDrill — format and verdict paths ─────────────────────────────────

func TestCLIExtra_FinalizeDrill_JSONFormat(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	report := restoredrill.DrillReport{
		Status:        restoredrill.StatusPass,
		SchemaVersion: 10,
		DurationMS:    300,
		Checks: []restoredrill.CheckResult{
			{Name: "connectivity", Status: restoredrill.StatusPass, Message: "ok"},
		},
	}
	err := finalizeDrill(context.Background(), "", report, false, "json", false)
	require.NoError(t, err)
}

func TestCLIExtra_FinalizeDrill_FailStatus(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	report := restoredrill.DrillReport{
		Status:        restoredrill.StatusFail,
		SchemaVersion: 10,
		DurationMS:    300,
	}
	err := finalizeDrill(context.Background(), "", report, false, "text", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "FAILED")
}

func TestCLIExtra_FinalizeDrill_WarnWithWarnExit(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	report := restoredrill.DrillReport{
		Status:        restoredrill.StatusWarn,
		SchemaVersion: 10,
		DurationMS:    300,
	}
	err := finalizeDrill(context.Background(), "", report, false, "text", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "WARNINGS")
}

func TestCLIExtra_FinalizeDrill_PassNoError(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	report := restoredrill.DrillReport{
		Status:        restoredrill.StatusPass,
		SchemaVersion: 10,
		DurationMS:    300,
	}
	err := finalizeDrill(context.Background(), "", report, false, "text", false)
	require.NoError(t, err)
}

func TestCLIExtra_FinalizeDrill_JSONFail(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	report := restoredrill.DrillReport{
		Status:        restoredrill.StatusFail,
		SchemaVersion: 10,
		DurationMS:    300,
	}
	err := finalizeDrill(context.Background(), "", report, false, "json", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "FAILED")
}

// ── printRestoreDrillReport — minimal report ─────────────────────────────────

func TestCLIExtra_PrintDrillReport_NoChecks(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	rep := restoredrill.DrillReport{
		Status:        restoredrill.StatusPass,
		SchemaVersion: 5,
		DurationMS:    100,
	}
	// Must not panic with an empty Checks slice.
	printRestoreDrillReport(rep)
}

// ── runLLM — dispatch edge cases ─────────────────────────────────────────────

func TestCLIExtra_RunLLM_ChannelsListReachesConfig(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runLLM([]string{"channels", "list"})
	require.Error(t, err)
}

func TestCLIExtra_RunLLM_AbilitiesListWithChannel(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runLLM([]string{"abilities", "list", "--channel", cliExtraTestUUID})
	require.Error(t, err)
}

func TestCLIExtra_RunLLM_RoutesUpsertReachesConfig(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runLLM([]string{"routes", "upsert", "--logical-model", "gpt-4", "--purpose", "enrich"})
	require.Error(t, err)
}

func TestCLIExtra_RunLLM_RoutesDeleteReachesConfig(t *testing.T) {
	cliExtraSetNoConfig(t)
	err := runLLM([]string{"routes", "delete", "--purpose", "enrich"})
	require.Error(t, err)
}

// ── runSecrets — dispatch verb coverage ──────────────────────────────────────

func TestCLIExtra_RunSecrets_NoArgs(t *testing.T) {
	err := runSecrets(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "generate-keyset")
}

func TestCLIExtra_RunSecrets_EmptyArgs(t *testing.T) {
	err := runSecrets([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "generate-keyset")
}

// ── runRestoreDrillVerifyBackup — flag parse ─────────────────────────────────

func TestCLIExtra_DrillVerifyBackup_FlagParseError(t *testing.T) {
	err := runRestoreDrillVerifyBackup([]string{"--unknown-flag"})
	require.Error(t, err)
}

func TestCLIExtra_DrillVerifyBackup_NoArgs(t *testing.T) {
	err := runRestoreDrillVerifyBackup([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "verify-backup")
}

// ── withRecoveryOpts — individual field errors ───────────────────────────────

func TestCLIExtra_WithRecoveryOpts_BadRestoreDuration(t *testing.T) {
	_, err := withRecoveryOpts(restoredrill.Options{}, "", "bad", "", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--restore-duration")
}

func TestCLIExtra_WithRecoveryOpts_BadRPOTarget(t *testing.T) {
	_, err := withRecoveryOpts(restoredrill.Options{}, "", "", "bad", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--rpo-target")
}

func TestCLIExtra_WithRecoveryOpts_BadRTOTarget(t *testing.T) {
	_, err := withRecoveryOpts(restoredrill.Options{}, "", "", "", "bad")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--rto-target")
}

func TestCLIExtra_WithRecoveryOpts_AllValid(t *testing.T) {
	opts, err := withRecoveryOpts(restoredrill.Options{},
		"2026-06-27T00:00:00Z", "5m30s", "24h", "30m")
	require.NoError(t, err)
	require.NotNil(t, opts.BackupTakenAt)
	require.NotNil(t, opts.RestoreDuration)
	require.NotZero(t, opts.RPOTarget)
	require.NotZero(t, opts.RTOTarget)
}

// ── optDur — coverage ────────────────────────────────────────────────────────

func TestCLIExtra_OptDur_NilReturns(t *testing.T) {
	require.Equal(t, "-", optDur(nil))
}

func TestCLIExtra_OptDur_ValidValue(t *testing.T) {
	v := int64(3600)
	got := optDur(&v) // ptrext:allow test-deref
	require.Equal(t, "1h0m0s", got)
}

// ── drillIcon — coverage ─────────────────────────────────────────────────────

func TestCLIExtra_DrillIcon_Skip(t *testing.T) {
	require.Equal(t, "-", drillIcon(restoredrill.StatusSkip))
}

func TestCLIExtra_DrillIcon_UnknownStatus(t *testing.T) {
	require.Equal(t, "-", drillIcon(restoredrill.Status("bogus")))
}
