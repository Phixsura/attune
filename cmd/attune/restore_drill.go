// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/restoredrill"
)

func init() {
	subcommands["restore-drill"] = runRestoreDrillCmd
}

func runRestoreDrillCmd(args []string) error {
	if len(args) == 0 {
		return printRestoreDrillUsage()
	}
	switch args[0] {
	case "run":
		return runRestoreDrillRun(args[1:])
	case "status":
		return runRestoreDrillStatus(args[1:])
	case "history":
		return runRestoreDrillHistory(args[1:])
	case "verify-backup":
		return runRestoreDrillVerifyBackup(args[1:])
	case "-h", "--help", "help":
		return printRestoreDrillUsage()
	default:
		return fmt.Errorf("unknown subcommand: %s\n\nRun 'attune restore-drill help' for usage", args[0])
	}
}

func printRestoreDrillUsage() error {
	fmt.Fprint(os.Stderr, `attune restore-drill

Verify an already-restored database is recoverable: schema/migration state,
row counts, pgvector, and decryptability of managed secrets. Targets a
throwaway restored database — never production traffic.

Usage:
  attune restore-drill run --target <url> [--baseline-url <url>] [--record] [...]
  attune restore-drill run --restore-from <file> --admin-url <url> [--record] [...]
  attune restore-drill status [--format text|json]
  attune restore-drill history [--limit N] [--format text|json]
  attune restore-drill verify-backup <pg_basebackup-dir>

Flags (run):
  --target           Restored (throwaway) database URL to verify
  --restore-from     Backup file to restore into an ephemeral DB, then verify
  --admin-url        Postgres admin URL for ephemeral provisioning (with --restore-from)
  --restore-tool     Restore tool with --restore-from: psql (default) or pg_restore
  --baseline-url     Live database URL for row-count comparison (optional)
  --backup-ref       Operator label for the backup under test (audit evidence)
  --backup-taken-at  When the backup was taken (RFC3339) — yields the RPO
  --restore-duration Measured restore time, e.g. 5m30s — the RTO
  --rpo-target       RPO SLA target, e.g. 24h — warns if exceeded
  --rto-target       RTO SLA target, e.g. 30m — warns if exceeded
  --record           Record the result to the production restore_drill_runs table
  --deep             Also run index validity + amcheck B-Tree structural verification
  --warn-exit        Exit non-zero on warnings, not only failures
  --format           Output format: text (default) or json
`)
	return nil
}

func runRestoreDrillRun(args []string) error {
	fs := flag.NewFlagSet("restore-drill run", flag.ContinueOnError)
	target := fs.String("target", "", "Restored (throwaway) database URL to verify (required)")
	baselineURL := fs.String("baseline-url", "", "Live database URL for row-count comparison (optional)")
	backupRef := fs.String("backup-ref", "", "Operator label for the backup under test")
	record := fs.Bool("record", false, "Record the result to production restore_drill_runs")
	deep := fs.Bool("deep", false, "Run the deep tier (index validity + amcheck B-Tree structural verification)")
	warnExit := fs.Bool("warn-exit", false, "Exit non-zero on warnings, not only failures")
	backupTakenAt := fs.String("backup-taken-at", "", "When the backup was taken (RFC3339) — yields the RPO")
	restoreDuration := fs.String("restore-duration", "", "Measured restore time, e.g. 5m30s — the RTO")
	rpoTarget := fs.String("rpo-target", "", "RPO SLA target, e.g. 24h — warns if exceeded")
	rtoTarget := fs.String("rto-target", "", "RTO SLA target, e.g. 30m — warns if exceeded")
	restoreFrom := fs.String("restore-from", "", "Backup file to restore into an ephemeral DB (alternative to --target)")
	adminURL := fs.String("admin-url", "", "Postgres admin URL for ephemeral provisioning (with --restore-from)")
	restoreTool := fs.String("restore-tool", "psql", "Restore tool with --restore-from: psql or pg_restore")
	format := fs.String("format", "text", "Output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	useOrchestration := ptrext.Indirect(restoreFrom) != ""
	if useOrchestration == (ptrext.Indirect(target) != "") {
		return fmt.Errorf("provide exactly one of --target (verify a restored DB) or --restore-from (restore + verify)")
	}
	if useOrchestration && ptrext.Indirect(adminURL) == "" {
		return fmt.Errorf("--admin-url is required with --restore-from")
	}
	if useOrchestration {
		if t := ptrext.Indirect(restoreTool); t != "psql" && t != "pg_restore" {
			return fmt.Errorf("--restore-tool must be psql or pg_restore, got %q", t)
		}
	}

	// Generous: the orchestration path restores a full backup before verifying.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	store, err := secretstore.NewTinkStoreFromJSONWithLegacy(cfg.Secrets.TinkKeyset, cfg.Secrets.LegacyInboundMasterKey)
	if err != nil {
		return fmt.Errorf("load Tink keyset: %w", err)
	}

	baseline, baselineUnval, baselineExts, err := gatherDrillBaseline(ctx, ptrext.Indirect(baselineURL))
	if err != nil {
		return err
	}

	opts := restoredrill.Options{
		BackupRef:           ptrext.Indirect(backupRef),
		Baseline:            baseline,
		BaselineUnvalidated: baselineUnval,
		BaselineExtensions:  baselineExts,
		Deep:                ptrext.Indirect(deep),
	}
	opts, err = withRecoveryOpts(opts, ptrext.Indirect(backupTakenAt), ptrext.Indirect(restoreDuration),
		ptrext.Indirect(rpoTarget), ptrext.Indirect(rtoTarget))
	if err != nil {
		return err
	}

	logext.Infof(ctx, "[restore-drill] start,orchestrate:%t,deep:%t,record:%t,baseline:%t",
		useOrchestration, ptrext.Indirect(deep), ptrext.Indirect(record), baseline != nil)

	report, err := runDrill(ctx, useOrchestration, ptrext.Indirect(adminURL), ptrext.Indirect(restoreFrom),
		ptrext.Indirect(restoreTool), ptrext.Indirect(target), store, opts)
	if err != nil {
		return fmt.Errorf("restore drill: %w", err)
	}

	logext.Infof(ctx, "[restore-drill] done,status:%s,duration_ms:%d,schema_version:%d",
		report.Status, report.DurationMS, report.SchemaVersion)

	return finalizeDrill(ctx, cfg.DatabaseURL, report,
		ptrext.Indirect(record), ptrext.Indirect(format), ptrext.Indirect(warnExit))
}

// gatherDrillBaseline pulls the live-DB baselines the row_counts / constraints /
// extensions checks compare against. Returns all-nil when no baseline URL given.
func gatherDrillBaseline(ctx context.Context, baselineURL string) (map[string]int64, *int, []string, error) {
	if baselineURL == "" {
		return nil, nil, nil, nil
	}
	basePool, err := database.NewPool(ctx, baselineURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("connect to baseline database: %w", err)
	}
	defer basePool.Close()
	counts, err := restoredrill.BaselineCounts(ctx, basePool)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("gather baseline counts: %w", err)
	}
	unval, err := restoredrill.BaselineUnvalidatedCount(ctx, basePool)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("gather baseline constraint count: %w", err)
	}
	exts, err := restoredrill.BaselineExtensions(ctx, basePool)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("gather baseline extensions: %w", err)
	}
	return counts, ptrext.Of(unval), exts, nil
}

// runDrill verifies an operator-restored DB (--target) or performs the restore
// itself (--restore-from) and verifies that.
func runDrill(ctx context.Context, useOrchestration bool, adminURL, restoreFrom, restoreTool, target string, store *secretstore.TinkStore, opts restoredrill.Options) (restoredrill.DrillReport, error) {
	if useOrchestration {
		return restoredrill.RestoreAndDrill(ctx, adminURL, restoreFrom, restoreTool, store, opts)
	}
	targetPool, err := database.NewPool(ctx, target)
	if err != nil {
		return restoredrill.DrillReport{}, fmt.Errorf("connect to target database: %w", err)
	}
	defer targetPool.Close()
	return restoredrill.Run(ctx, targetPool, store, time.Now(), opts), nil
}

// finalizeDrill prints the result FIRST (so a recording failure can never
// swallow the verdict the operator needs to see), optionally records it, then
// maps the verdict to an exit code. The drill verdict always dominates the exit.
func finalizeDrill(ctx context.Context, prodURL string, report restoredrill.DrillReport, doRecord bool, format string, warnExit bool) error {
	if format == "json" {
		if err := outputJSON(report); err != nil {
			return err
		}
	} else {
		printRestoreDrillReport(report)
	}
	if doRecord {
		if err := recordDrill(ctx, prodURL, report); err != nil {
			if report.Status == restoredrill.StatusFail {
				return fmt.Errorf("restore drill FAILED (and recording it also failed: %w)", err)
			}
			return err
		}
	}
	switch report.Status {
	case restoredrill.StatusFail:
		return fmt.Errorf("restore drill FAILED")
	case restoredrill.StatusWarn:
		if warnExit {
			return fmt.Errorf("restore drill passed with WARNINGS")
		}
	}
	return nil
}

func recordDrill(ctx context.Context, prodURL string, report restoredrill.DrillReport) error {
	prodPool, err := database.NewPool(ctx, prodURL)
	if err != nil {
		return fmt.Errorf("connect to production database for recording: %w", err)
	}
	defer prodPool.Close()
	if err := restoredrill.Record(ctx, prodPool, report); err != nil {
		return fmt.Errorf("record drill result: %w", err)
	}
	return nil
}

func runRestoreDrillStatus(args []string) error {
	fs := flag.NewFlagSet("restore-drill status", flag.ContinueOnError)
	format := fs.String("format", "text", "Output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := connectDatabase(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	last, ok, err := restoredrill.ReadLast(ctx, pool)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println("No restore drills recorded yet. Run 'attune restore-drill run --target <url> --record'.")
		return nil
	}

	if ptrext.Indirect(format) == "json" {
		return outputJSON(last)
	}
	fmt.Printf("Last restore drill: %s\n  status=%s  backup_ref=%q  duration=%dms\n",
		last.RanAt.Format(time.RFC3339), strings.ToUpper(string(last.Status)), last.BackupRef, last.DurationMS)
	return nil
}

// withRecoveryOpts parses the RPO/RTO flag strings into opts and returns the
// updated value. By-value so the caller never address-of's a local.
func withRecoveryOpts(opts restoredrill.Options, backupTakenAt, restoreDuration, rpoTarget, rtoTarget string) (restoredrill.Options, error) {
	if backupTakenAt != "" {
		t, err := time.Parse(time.RFC3339, backupTakenAt)
		if err != nil {
			return opts, fmt.Errorf("--backup-taken-at: %w", err)
		}
		opts.BackupTakenAt = ptrext.Of(t)
	}
	if restoreDuration != "" {
		d, err := time.ParseDuration(restoreDuration)
		if err != nil {
			return opts, fmt.Errorf("--restore-duration: %w", err)
		}
		opts.RestoreDuration = ptrext.Of(d)
	}
	if rpoTarget != "" {
		d, err := time.ParseDuration(rpoTarget)
		if err != nil {
			return opts, fmt.Errorf("--rpo-target: %w", err)
		}
		opts.RPOTarget = d
	}
	if rtoTarget != "" {
		d, err := time.ParseDuration(rtoTarget)
		if err != nil {
			return opts, fmt.Errorf("--rto-target: %w", err)
		}
		opts.RTOTarget = d
	}
	return opts, nil
}

func runRestoreDrillHistory(args []string) error {
	fs := flag.NewFlagSet("restore-drill history", flag.ContinueOnError)
	limit := fs.Int("limit", 20, "Number of recent drills to show")
	format := fs.String("format", "text", "Output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := connectDatabase(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	rows, err := restoredrill.History(ctx, pool, ptrext.Indirect(limit))
	if err != nil {
		return err
	}
	if ptrext.Indirect(format) == "json" {
		return outputJSON(rows)
	}
	if len(rows) == 0 {
		fmt.Println("No restore drills recorded yet.")
		return nil
	}
	fmt.Printf("%-20s  %-7s  %-9s  %-9s  %s\n", "RAN AT", "STATUS", "RTO", "RPO", "BACKUP REF")
	for _, h := range rows {
		fmt.Printf("%-20s  %-7s  %-9s  %-9s  %s\n",
			h.RanAt.Format("2006-01-02 15:04:05"),
			strings.ToUpper(string(h.Status)),
			optDur(h.RTOSeconds), optDur(h.RPOSeconds), h.BackupRef)
	}
	return nil
}

func runRestoreDrillVerifyBackup(args []string) error {
	fs := flag.NewFlagSet("restore-drill verify-backup", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: attune restore-drill verify-backup <pg_basebackup-dir>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	dir := fs.Arg(0)
	if err := restoredrill.VerifyBackupArtifact(ctx, dir); err != nil {
		return fmt.Errorf("backup artifact verification FAILED: %w", err)
	}
	fmt.Printf("backup artifact at %q verified OK\n", dir)
	return nil
}

func optDur(secs *int64) string {
	if secs == nil {
		return "-"
	}
	return (time.Duration(ptrext.Indirect(secs)) * time.Second).String()
}

func printRestoreDrillReport(rep restoredrill.DrillReport) {
	fmt.Print("\nattune restore drill — recoverability report\n\n")
	for _, c := range rep.Checks {
		fmt.Printf("  %s %-20s %s\n", drillIcon(c.Status), c.Name, c.Message)
	}
	fmt.Printf("\nOverall: %s  (schema v%d, verify %dms",
		strings.ToUpper(string(rep.Status)), rep.SchemaVersion, rep.DurationMS)
	if rep.RPOSeconds != nil {
		fmt.Printf(", RPO %s", optDur(rep.RPOSeconds))
	}
	if rep.RTOSeconds != nil {
		fmt.Printf(", RTO %s", optDur(rep.RTOSeconds))
	}
	if rep.BackupRef != "" {
		fmt.Printf(", backup %q", rep.BackupRef)
	}
	fmt.Print(")\n")
}

func drillIcon(s restoredrill.Status) string {
	switch s {
	case restoredrill.StatusPass:
		return "✓"
	case restoredrill.StatusWarn:
		return "!"
	case restoredrill.StatusFail:
		return "✗"
	default:
		return "-"
	}
}
