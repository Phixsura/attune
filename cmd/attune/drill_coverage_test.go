// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/restoredrill"
)

// ── runRestoreDrillCmd dispatch ───────────────────────────────────────────────

func TestRunRestoreDrillCmd_StatusDispatch(t *testing.T) {
	setNoConfig(t)
	err := runRestoreDrillCmd([]string{"status"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

func TestRunRestoreDrillCmd_HistoryDispatch(t *testing.T) {
	setNoConfig(t)
	err := runRestoreDrillCmd([]string{"history"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

func TestRunRestoreDrillCmd_VerifyBackupNoArgs(t *testing.T) {
	err := runRestoreDrillCmd([]string{"verify-backup"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "verify-backup")
}

func TestRunRestoreDrillCmd_VerifyBackupBadDir(t *testing.T) {
	err := runRestoreDrillCmd([]string{"verify-backup", "/nonexistent-backup-dir-for-tests"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "backup artifact verification FAILED")
}

// ── runRestoreDrillRun flag validation ───────────────────────────────────────

func TestRunRestoreDrillRun_FlagParse(t *testing.T) {
	err := runRestoreDrillRun([]string{"--unknown-flag"})
	require.Error(t, err)
}

func TestRunRestoreDrillRun_NeitherTargetNorRestore(t *testing.T) {
	// Neither --target nor --restore-from: useOrchestration==false, target=="",
	// so useOrchestration == (target != "") is false == false => true => error.
	err := runRestoreDrillRun([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "provide exactly one of")
}

func TestRunRestoreDrillRun_BothTargetAndRestore(t *testing.T) {
	// Both --target and --restore-from set: useOrchestration==true, target!="",
	// so useOrchestration == (target != "") is true == true => true => error.
	err := runRestoreDrillRun([]string{"--target", "postgres://localhost/restored", "--restore-from", "backup.dump"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "provide exactly one of")
}

func TestRunRestoreDrillRun_RestoreFromNoAdmin(t *testing.T) {
	err := runRestoreDrillRun([]string{"--restore-from", "backup.dump"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--admin-url is required")
}

func TestRunRestoreDrillRun_BadRestoreTool(t *testing.T) {
	err := runRestoreDrillRun([]string{
		"--restore-from", "backup.dump",
		"--admin-url", "postgres://admin@localhost/postgres",
		"--restore-tool", "badtool",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--restore-tool must be psql or pg_restore")
	require.Contains(t, err.Error(), "badtool")
}

// ── runRestoreDrillStatus ─────────────────────────────────────────────────────

func TestRunRestoreDrillStatus_FlagParse(t *testing.T) {
	err := runRestoreDrillStatus([]string{"--unknown-flag"})
	require.Error(t, err)
}

func TestRunRestoreDrillStatus_ConfigFail(t *testing.T) {
	setNoConfig(t)
	err := runRestoreDrillStatus([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

// ── runRestoreDrillHistory ────────────────────────────────────────────────────

func TestRunRestoreDrillHistory_FlagParse(t *testing.T) {
	err := runRestoreDrillHistory([]string{"--unknown-flag"})
	require.Error(t, err)
}

func TestRunRestoreDrillHistory_ConfigFail(t *testing.T) {
	setNoConfig(t)
	err := runRestoreDrillHistory([]string{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "load config")
}

// ── printRestoreDrillReport field coverage ────────────────────────────────────

func TestPrintRestoreDrillReport_WithRPOandRTO(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	rpo := int64(3600) // 1 hour
	rto := int64(1800) // 30 minutes
	rep := restoredrill.DrillReport{
		Status:        restoredrill.StatusPass,
		SchemaVersion: 10,
		DurationMS:    500,
		BackupRef:     "backup-2026-06-27",
		RPOSeconds:    ptrext.Of(rpo),
		RTOSeconds:    ptrext.Of(rto),
		Checks: []restoredrill.CheckResult{
			{Name: "connectivity", Status: restoredrill.StatusPass, Message: "connected"},
		},
	}
	// Must not panic; exercises the RPOSeconds, RTOSeconds, and BackupRef branches.
	printRestoreDrillReport(rep)
}

func TestPrintRestoreDrillReport_WithChecks(t *testing.T) {
	// No t.Parallel(): writes to os.Stdout.
	rep := restoredrill.DrillReport{
		Status:        restoredrill.StatusWarn,
		SchemaVersion: 5,
		DurationMS:    200,
		Checks: []restoredrill.CheckResult{
			{Name: "schema", Status: restoredrill.StatusPass, Message: "82 applied, 0 pending"},
			{Name: "row_counts", Status: restoredrill.StatusWarn, Message: "table feedback: 0 vs baseline 100"},
			{Name: "decryptability", Status: restoredrill.StatusFail, Message: "1 key failed"},
			{Name: "deep", Status: restoredrill.StatusSkip, Message: "skipped"},
		},
	}
	// Must not panic; exercises all four Status values via drillIcon.
	printRestoreDrillReport(rep)
}

// ── finalizeDrill — WarnNoExit path ──────────────────────────────────────────

func TestFinalizeDrill_WarnNoExit(t *testing.T) {
	// No t.Parallel(): finalizeDrill writes to os.Stdout.
	report := restoredrill.DrillReport{
		Status: restoredrill.StatusWarn,
		Checks: []restoredrill.CheckResult{
			{Name: "row_counts", Status: restoredrill.StatusWarn, Message: "minor drift"},
		},
	}
	// warnExit=false: StatusWarn must not return an error.
	err := finalizeDrill(context.Background(), "", report, false, "text", false)
	require.NoError(t, err)
}

func TestGatherDrillBaselineRejectsInvalidDatabaseURL(t *testing.T) {
	t.Parallel()

	_, _, _, err := gatherDrillBaseline(context.Background(), "://bad-database-url")

	require.Error(t, err)
	require.Contains(t, err.Error(), "connect to baseline database")
}

func TestRunDrillRejectsInvalidTargetDatabaseURL(t *testing.T) {
	t.Parallel()

	_, err := runDrill(context.Background(), false, "", "", "", "://bad-database-url", nil, restoredrill.Options{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "connect to target database")
}

func TestRecordDrillRejectsInvalidProductionDatabaseURL(t *testing.T) {
	t.Parallel()

	err := recordDrill(context.Background(), "://bad-database-url", restoredrill.DrillReport{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "connect to production database")
}
