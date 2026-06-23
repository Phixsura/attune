package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func init() {
	subcommands["migrations"] = runMigrationsCmd
}

func runMigrationsCmd(args []string) error {
	if len(args) == 0 {
		return printMigrationsUsage()
	}

	switch args[0] {
	case "status":
		return runMigrationsStatus(args[1:])
	case "verify":
		return runMigrationsVerify(args[1:])
	case "dry-run":
		return runMigrationsDryRun(args[1:])
	case "repair":
		return runMigrationsRepair(args[1:])
	case "baseline":
		return runMigrationsBaseline(args[1:])
	case "-h", "--help", "help":
		return printMigrationsUsage()
	default:
		return fmt.Errorf("unknown subcommand: %s\n\nRun 'attune migrations help' for usage", args[0])
	}
}

func printMigrationsUsage() error {
	fmt.Fprint(os.Stderr, `attune migrations

Usage:
  attune migrations status [--format text|json] [--pending]  Show migration status
  attune migrations verify [--format text|json]              Verify checksums and naming
  attune migrations dry-run                                  Preview pending SQL
  attune migrations repair --version N [--force]             Repair dirty/failed migration
  attune migrations baseline --version N [--force]           Baseline existing database

Flags:
  --format    Output format: text (default) or json
  --pending   Show only pending migrations (status only)
`)
	return nil
}

func runMigrationsStatus(args []string) error {
	fs := flag.NewFlagSet("migrations status", flag.ContinueOnError)
	format := fs.String("format", "text", "Output format: text or json")
	pendingOnly := fs.Bool("pending", false, "Show only pending migrations")
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

	status, err := database.GetMigrationStatus(ctx, pool)
	if err != nil {
		return fmt.Errorf("get migration status: %w", err)
	}

	if ptrext.Indirect(format) == "json" {
		return outputJSON(status)
	}
	return printMigrationStatusText(status, ptrext.Indirect(pendingOnly))
}

func connectDatabase(ctx context.Context) (*pgxpool.Pool, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return pool, nil
}

func outputJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func printMigrationStatusText(status database.MigrationStatus, pendingOnly bool) error {
	fmt.Printf("Migration Status (%d total, %d applied, %d pending)\n\n",
		status.Total, status.Applied, status.Pending)

	if !pendingOnly && status.Applied > 0 {
		printAppliedMigrations(status.Migrations)
	}

	if status.Pending > 0 {
		printPendingMigrations(status.Migrations)
	}

	printChecksumStatus(status.Checksums)
	printDuplicateStatus(status.Duplicates)
	return nil
}

func printAppliedMigrations(migrations []database.MigrationDetail) {
	fmt.Println("Applied:")
	for _, m := range migrations {
		if m.Status != "applied" {
			continue
		}
		printAppliedMigration(m)
	}
	fmt.Println()
}

func printAppliedMigration(m database.MigrationDetail) {
	appliedAt := formatAppliedAt(m.AppliedAt)
	duration := formatDuration(m.DurationMs)
	appliedBy := ptrext.IndirectOr(m.AppliedBy, "")
	filename := truncateFilename(m.Filename, 45)

	fmt.Printf("  %03d %-45s  %s  %6s  %s\n",
		m.Version, filename, appliedAt, duration, appliedBy)
}

func formatAppliedAt(appliedAt *string) string {
	if appliedAt == nil {
		return ""
	}
	t, err := time.Parse(time.RFC3339, ptrext.Indirect(appliedAt))
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

func formatDuration(durationMs *int) string {
	if durationMs == nil {
		return ""
	}
	return fmt.Sprintf("%dms", ptrext.Indirect(durationMs))
}

func truncateFilename(filename string, maxLen int) string {
	if len(filename) <= maxLen {
		return filename
	}
	return filename[:maxLen-3] + "..."
}

func printPendingMigrations(migrations []database.MigrationDetail) {
	fmt.Println("Pending:")
	for _, m := range migrations {
		if m.Status == "pending" {
			fmt.Printf("  %03d %s\n", m.Version, m.Filename)
		}
	}
	fmt.Println()
}

func printChecksumStatus(checksums database.ChecksumStatus) {
	if checksums.Total == 0 {
		return
	}
	fmt.Printf("Checksums: %d/%d verified\n", checksums.Verified, checksums.Total)
	for _, d := range checksums.Drifted {
		fmt.Printf("  DRIFTED: %03d %s (stored: %s, computed: %s)\n",
			d.Version, d.Filename, d.Stored, d.Computed)
	}
}

func printDuplicateStatus(duplicates []database.DuplicatePrefix) {
	if len(duplicates) == 0 {
		fmt.Println("Duplicates: none")
		return
	}
	fmt.Println("Duplicates: DETECTED")
	for _, d := range duplicates {
		fmt.Printf("  %03d: %s\n", d.Prefix, strings.Join(d.Files, ", "))
	}
}

func runMigrationsVerify(args []string) error {
	fs := flag.NewFlagSet("migrations verify", flag.ContinueOnError)
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

	results := runVerificationChecks(ctx, pool)

	if ptrext.Indirect(format) == "json" {
		return outputJSON(results)
	}
	return printVerificationResults(results)
}

type verificationResults struct {
	Duplicates      bool   `json:"duplicates"`
	DuplicatesError string `json:"duplicates_error,omitempty"`
	Checksums       bool   `json:"checksums"`
	ChecksumsError  string `json:"checksums_error,omitempty"`
	NoTx            bool   `json:"no_tx"`
	NoTxError       string `json:"no_tx_error,omitempty"`
	Passed          bool   `json:"passed"`
}

func runVerificationChecks(ctx context.Context, pool *pgxpool.Pool) verificationResults {
	names, _ := database.LoadMigrationNames()

	dupErr := database.DetectDuplicatePrefixes(names)
	checksumErr := database.VerifyChecksums(ctx, pool)
	noTxErr := database.VerifyNoTxDirectives(names)

	results := verificationResults{
		Duplicates: dupErr == nil,
		Checksums:  checksumErr == nil,
		NoTx:       noTxErr == nil,
		Passed:     dupErr == nil && checksumErr == nil && noTxErr == nil,
	}
	if dupErr != nil {
		results.DuplicatesError = dupErr.Error()
	}
	if checksumErr != nil {
		results.ChecksumsError = checksumErr.Error()
	}
	if noTxErr != nil {
		results.NoTxError = noTxErr.Error()
	}
	return results
}

func printVerificationResults(results verificationResults) error {
	fmt.Println("Verifying migrations...")

	printCheckResult("Duplicates", results.Duplicates, results.DuplicatesError)
	printCheckResult("Checksums", results.Checksums, results.ChecksumsError)
	printCheckResult("No-tx directives", results.NoTx, results.NoTxError)

	if !results.Passed {
		fmt.Println("\nVerification FAILED.")
		os.Exit(1)
	}
	fmt.Println("\nAll checks passed.")
	return nil
}

func printCheckResult(name string, passed bool, errMsg string) {
	if passed {
		fmt.Printf("  %s: OK\n", name)
		return
	}
	fmt.Printf("  %s: FAIL\n", name)
	for _, line := range strings.Split(errMsg, "\n") {
		fmt.Printf("    %s\n", line)
	}
}

func runMigrationsDryRun(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := connectDatabase(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	pending, err := database.GetPendingMigrations(ctx, pool)
	if err != nil {
		return fmt.Errorf("get pending migrations: %w", err)
	}

	if len(pending) == 0 {
		fmt.Println("No pending migrations.")
		return nil
	}

	fmt.Printf("Pending migrations (%d):\n\n", len(pending))
	for _, m := range pending {
		printPendingMigrationSQL(m)
	}
	fmt.Println("No changes applied. Run 'attune server' to apply.")
	return nil
}

func printPendingMigrationSQL(m database.MigrationFile) {
	noTxMarker := ""
	if m.NoTx {
		noTxMarker = " [no-transaction]"
	}
	fmt.Printf("-- %03d %s%s\n", m.Version, m.Name, noTxMarker)
	fmt.Printf("-- checksum: %s\n", m.Checksum)
	fmt.Println(string(m.Body))
	fmt.Println()
}

func confirmPrompt(question string) bool {
	fmt.Printf("%s [y/N] ", question)
	var answer string
	if _, err := fmt.Scanln(&answer); err != nil {
		return false
	}
	return answer == "y" || answer == "Y"
}

type repairOpts struct {
	version int
	force   bool
}

func parseRepairFlags(args []string) (repairOpts, error) {
	fs := flag.NewFlagSet("migrations repair", flag.ContinueOnError)
	version := fs.Int("version", 0, "Migration version to repair (required)")
	force := fs.Bool("force", false, "Skip confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return repairOpts{}, err
	}
	return repairOpts{version: ptrext.Indirect(version), force: ptrext.Indirect(force)}, nil
}

func runMigrationsRepair(args []string) error {
	opts, err := parseRepairFlags(args)
	if err != nil {
		return err
	}
	if opts.version == 0 {
		return fmt.Errorf("--version is required\n\nUsage: attune migrations repair --version N [--force]")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := connectDatabase(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	filename, success, err := queryMigrationState(ctx, pool, opts.version)
	if err != nil {
		return err
	}

	if !confirmRepair(opts.version, filename, success, opts.force) {
		return nil
	}

	return executeRepair(ctx, pool, opts.version)
}

func queryMigrationState(ctx context.Context, pool *pgxpool.Pool, version int) (string, bool, error) {
	var filename string
	var success bool
	err := pool.QueryRow(ctx, `
		SELECT filename, success FROM schema_migrations_feedback WHERE version = $1
	`, version).Scan(&filename, &success)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return "", false, fmt.Errorf("migration version %d not found in tracker", version)
		}
		return "", false, fmt.Errorf("query migration: %w", err)
	}
	return filename, success, nil
}

func confirmRepair(version int, filename string, success, force bool) bool {
	if success {
		fmt.Printf("Migration %d (%s) is already marked successful.\n", version, filename)
		if !force {
			return confirmPrompt("Recalculate checksum anyway?")
		}
	} else {
		fmt.Printf("Migration %d (%s) is in DIRTY state (success=FALSE).\n", version, filename)
		if !force {
			return confirmPrompt("Mark as successful and recalculate checksum?")
		}
	}
	return true
}

func executeRepair(ctx context.Context, pool *pgxpool.Pool, version int) error {
	names, err := database.LoadMigrationNames()
	if err != nil {
		return fmt.Errorf("load migration names: %w", err)
	}

	if version < 1 || version > len(names) {
		return fmt.Errorf("version %d out of range (1-%d)", version, len(names))
	}

	name := names[version-1]
	checksum, err := database.ComputeChecksum(name)
	if err != nil {
		return fmt.Errorf("compute checksum: %w", err)
	}

	_, err = pool.Exec(ctx, `
		UPDATE schema_migrations_feedback
		SET success = TRUE, checksum = $2, applied_by = COALESCE(NULLIF(applied_by, ''), $3)
		WHERE version = $1
	`, version, checksum, database.Version)
	if err != nil {
		return fmt.Errorf("update migration: %w", err)
	}

	fmt.Printf("Repaired migration %d:\n", version)
	fmt.Printf("  success:  TRUE\n")
	fmt.Printf("  checksum: %s\n", checksum)
	return nil
}

type baselineOpts struct {
	version int
	force   bool
}

func parseBaselineFlags(args []string) (baselineOpts, error) {
	fs := flag.NewFlagSet("migrations baseline", flag.ContinueOnError)
	version := fs.Int("version", 0, "Baseline version (marks 1..N as applied)")
	force := fs.Bool("force", false, "Skip confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return baselineOpts{}, err
	}
	return baselineOpts{version: ptrext.Indirect(version), force: ptrext.Indirect(force)}, nil
}

func runMigrationsBaseline(args []string) error {
	opts, err := parseBaselineFlags(args)
	if err != nil {
		return err
	}
	if opts.version == 0 {
		return fmt.Errorf("--version is required\n\nUsage: attune migrations baseline --version N [--force]")
	}

	names, err := database.LoadMigrationNames()
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	if opts.version < 1 || opts.version > len(names) {
		return fmt.Errorf("version %d out of range (1-%d)", opts.version, len(names))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := connectDatabase(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	existing, err := countExistingMigrations(ctx, pool)
	if err != nil {
		return err
	}

	if existing > 0 {
		return fmt.Errorf("database already has %d migrations tracked; baseline only works on fresh databases", existing)
	}

	if !opts.force {
		fmt.Printf("This will mark migrations 1-%d as applied without running them.\n", opts.version)
		fmt.Printf("Only use this for adopting an existing database that was migrated externally.\n\n")
		if !confirmPrompt(fmt.Sprintf("Baseline to version %d?", opts.version)) {
			return nil
		}
	}

	return executeBaseline(ctx, pool, names, opts.version)
}

func countExistingMigrations(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var count int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM schema_migrations_feedback
	`).Scan(&count)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return 0, nil
		}
		return 0, fmt.Errorf("count migrations: %w", err)
	}
	return count, nil
}

func executeBaseline(ctx context.Context, pool *pgxpool.Pool, names []string, targetVersion int) error {
	// Ensure tracker table exists with extended columns
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations_feedback (
			version INT PRIMARY KEY,
			filename TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			checksum TEXT NOT NULL DEFAULT '',
			duration_ms INT NOT NULL DEFAULT 0,
			applied_by TEXT NOT NULL DEFAULT '',
			success BOOLEAN NOT NULL DEFAULT TRUE
		)
	`)
	if err != nil {
		return fmt.Errorf("create tracker: %w", err)
	}

	for i := 0; i < targetVersion; i++ {
		version := i + 1
		name := names[i]
		checksum, err := database.ComputeChecksum(name)
		if err != nil {
			return fmt.Errorf("compute checksum for %s: %w", name, err)
		}

		_, err = pool.Exec(ctx, `
			INSERT INTO schema_migrations_feedback (version, filename, checksum, duration_ms, applied_by, success)
			VALUES ($1, $2, $3, 0, $4, TRUE)
			ON CONFLICT (version) DO NOTHING
		`, version, name, checksum, "baseline:"+database.Version)
		if err != nil {
			return fmt.Errorf("insert baseline %d: %w", version, err)
		}
	}

	fmt.Printf("Baselined %d migrations (1-%d).\n", targetVersion, targetVersion)
	fmt.Printf("The database is now ready for normal migration management.\n")
	return nil
}
