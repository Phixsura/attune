// Package database runs attune's own schema migrations against its
// dedicated PostgreSQL database .
//
// History: pre- attune shared the main backend's PG and migrations
// 001..003 were no-op + ALTER-style increments. the 2026-Q2 split moved attune to
// its own DB; migration 001 was rewritten to build the full schema from
// scratch (tenants / user_feedback / external_api_keys). 002..005 are
// kept verbatim and remain idempotent (IF NOT EXISTS / IF EXISTS), so a
// fresh DB applies them as no-ops after 001 and the rare legacy shared-DB
// install can still run them safely.
//
// The tracker table `schema_migrations_feedback` is the attune-local
// version-bookkeeping table — name preserved so existing prod tracker
// rows survive a redeploy.
//
// #150: Migration integrity tracking adds checksum verification, duplicate
// prefix detection, and execution metadata (duration, binary version).
package database

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
)

// Version is the binary version, set via ldflags:
//
//	-X 'github.com/Phixsura/attune/internal/infra/database.Version=v0.9.0'
//
// Used for audit trail in migration records.
var Version = "unknown"

// noTxDirective marks a migration that must run OUTSIDE a transaction (e.g.
// CREATE INDEX CONCURRENTLY, which Postgres forbids inside a transaction
// block). Such a migration MUST be a single statement and MUST be idempotent
// (IF NOT EXISTS), because a non-transactional apply is not atomic with its
// tracker insert — a crash between them re-runs the body on the next deploy.
const noTxDirective = "migrate:no-transaction"

//go:embed migrations/*.sql
var migrationFS embed.FS

const trackerTable = "schema_migrations_feedback"

// migrationLockKey is a stable pg_advisory_lock key for Attune schema changes.
// It serializes startup migrations across replicas and deploy mechanisms.
const migrationLockKey int64 = 0x7AEC0ADBA51C042

// RunMigrations applies any unapplied migrations from migrations/*.sql in
// lexicographic order. Each file is wrapped in its own transaction.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := acquireMigrationLock(ctx, pool)
	if err != nil {
		return err
	}
	defer releaseMigrationLock(conn)
	return runMigrationsLocked(ctx, conn)
}

func acquireMigrationLock(ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Conn, error) {
	const where = "database.acquireMigrationLock"
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire migration connection: %w", err)
	}
	// The pool sets a 30s statement_timeout default (database.NewPool, #64), but
	// migrations can legitimately run long (large backfills, index builds). Clear
	// it BEFORE acquiring the advisory lock: a CONCURRENTLY index build on one
	// replica can hold the lock for minutes, and the lock wait itself is a
	// statement — under the 30s default a second replica's pg_advisory_lock would
	// time out and fail startup instead of waiting its turn.
	if _, err := conn.Exec(ctx, `SET statement_timeout = 0`); err != nil {
		conn.Release()
		logext.Errorf(ctx, "[%s] clear statement_timeout failed,err:%+v", where, err.Error())
		return nil, fmt.Errorf("clear migration statement_timeout: %w", err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockKey); err != nil {
		conn.Release()
		logext.Errorf(ctx, "[%s] lock failed,err:%+v", where, err.Error())
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	logext.Infof(ctx, "[%s] OK", where)
	return conn, nil
}

func releaseMigrationLock(conn *pgxpool.Conn) {
	const where = "database.releaseMigrationLock"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var unlocked bool
	err := conn.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, migrationLockKey).Scan(&unlocked)
	if err != nil || !unlocked {
		logext.Warnf(ctx, "[%s] unlock failed,unlocked:%t,err:%+v", where, unlocked, err)
		_ = conn.Conn().Close(context.Background())
	}
	conn.Release()
}

func runMigrationsLocked(ctx context.Context, conn *pgxpool.Conn) error {
	const where = "database.RunMigrations"
	logext.Infof(ctx, "[%s] start", where)

	if err := ensureTrackerTable(ctx, conn); err != nil {
		return err
	}

	names, err := LoadMigrationNames()
	if err != nil {
		logext.Errorf(ctx, "[%s] load migration names failed,err:%+v", where, err.Error())
		return err
	}

	if err := runPreflightChecks(ctx, conn, names); err != nil {
		return err
	}

	if err := applyPendingMigrations(ctx, conn, names); err != nil {
		return err
	}

	logext.Infof(ctx, "[%s] OK", where)
	return nil
}

func ensureTrackerTable(ctx context.Context, conn *pgxpool.Conn) error {
	_, err := conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version INT PRIMARY KEY,
			filename TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, trackerTable))
	if err != nil {
		return fmt.Errorf("create %s: %w", trackerTable, err)
	}
	return nil
}

func runPreflightChecks(ctx context.Context, conn *pgxpool.Conn, names []string) error {
	const where = "database.RunMigrations"

	if err := DetectDuplicatePrefixes(names); err != nil {
		logext.Errorf(ctx, "[%s] duplicate prefixes,err:%+v", where, err.Error())
		return err
	}

	if err := VerifyChecksumsConn(ctx, conn); err != nil {
		logext.Errorf(ctx, "[%s] checksum verification failed,err:%+v", where, err.Error())
		return err
	}

	if err := VerifyManifestHashConn(ctx, conn); err != nil {
		logext.Errorf(ctx, "[%s] manifest hash verification failed,err:%+v", where, err.Error())
		return err
	}

	if hasExtendedColumns(ctx, conn) {
		if err := detectDirtyMigrations(ctx, conn); err != nil {
			logext.Errorf(ctx, "[%s] dirty migration detected,err:%+v", where, err.Error())
			return err
		}
	}
	return nil
}

func applyPendingMigrations(ctx context.Context, conn *pgxpool.Conn, names []string) error {
	const where = "database.RunMigrations"
	hasExtendedCols := hasExtendedColumns(ctx, conn)

	// Count pending migrations for the gauge.
	pendingCount := 0
	for i := range names {
		version := i + 1
		applied, err := isMigrationApplied(ctx, conn, version)
		if err != nil {
			return err
		}
		if !applied {
			pendingCount++
		}
	}
	metrics.MigrationPending.Set(float64(pendingCount))

	for i, name := range names {
		version := i + 1
		applied, err := isMigrationApplied(ctx, conn, version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		if err := applySingleMigration(ctx, conn, version, name, hasExtendedCols); err != nil {
			return err
		}

		// After applying 070, extended columns become available.
		if !hasExtendedCols && hasExtendedColumns(ctx, conn) {
			hasExtendedCols = true
			if err := backfillChecksums(ctx, conn, names); err != nil {
				logext.Errorf(ctx, "[%s] checksum backfill failed,err:%+v", where, err.Error())
				return fmt.Errorf("backfill checksums: %w", err)
			}
		}
	}

	// All migrations applied; set pending to 0.
	metrics.MigrationPending.Set(0)

	// Store manifest hash after all migrations are applied.
	// This creates or updates the hash for reordering detection on next startup.
	if hasManifestTable(ctx, conn) {
		if err := storeManifestHashSafe(ctx, conn, names); err != nil {
			return err
		}
	}
	return nil
}

func storeManifestHashSafe(ctx context.Context, conn *pgxpool.Conn, names []string) error {
	const where = "database.RunMigrations"
	manifestHash, err := ManifestHash(names)
	if err != nil {
		logext.Errorf(ctx, "[%s] compute manifest hash failed,err:%+v", where, err.Error())
		return fmt.Errorf("compute manifest hash: %w", err)
	}
	if err := StoreManifestHash(ctx, conn, manifestHash); err != nil {
		logext.Errorf(ctx, "[%s] store manifest hash failed,err:%+v", where, err.Error())
		return fmt.Errorf("store manifest hash: %w", err)
	}
	logext.Infof(ctx, "[%s] manifest hash stored,hash:%s", where, ChecksumShort(manifestHash))
	return nil
}

func isMigrationApplied(ctx context.Context, conn *pgxpool.Conn, version int) (bool, error) {
	var applied bool
	err := conn.QueryRow(ctx,
		fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE version=$1)", trackerTable),
		version).Scan(&applied)
	if err != nil {
		return false, fmt.Errorf("check migration %d: %w", version, err)
	}
	return applied, nil
}

func applySingleMigration(ctx context.Context, conn *pgxpool.Conn, version int, name string, hasExtendedCols bool) error {
	const where = "database.RunMigrations"

	body, err := migrationFS.ReadFile("migrations/" + name)
	if err != nil {
		logext.Errorf(ctx, "[%s] read file failed,file:%s,err:%+v", where, name, err.Error())
		return fmt.Errorf("read %s: %w", name, err)
	}

	checksum := Checksum(body)
	start := time.Now()

	if isNoTxMigration(body) {
		if err := applyMigrationNoTx(ctx, conn, version, name, body, checksum, start, hasExtendedCols); err != nil {
			return err
		}
	} else if err := applyMigrationTx(ctx, conn, version, name, body, checksum, start, hasExtendedCols); err != nil {
		return err
	}

	duration := time.Since(start)
	versionStr := strconv.Itoa(version)
	metrics.MigrationAppliedTotal.WithLabelValues(versionStr, name).Inc()
	metrics.MigrationApplyDuration.WithLabelValues(versionStr).Observe(duration.Seconds())

	logext.Infof(ctx, "[%s] applied,version:%d,file:%s,duration:%v", where, version, name, duration)
	return nil
}

// hasExtendedColumns checks if the tracker table has the checksum column.
func hasExtendedColumns(ctx context.Context, conn *pgxpool.Conn) bool {
	var exists bool
	err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'schema_migrations_feedback' AND column_name = 'checksum'
		)`).Scan(&exists)
	return err == nil && exists
}

// ErrDirtyMigration is returned when a migration started but didn't complete.
type ErrDirtyMigration struct {
	Version  int
	Filename string
}

func (e ErrDirtyMigration) Error() string {
	return fmt.Sprintf("dirty migration detected: version %d (%s) started but did not complete\n\n"+
		"This indicates a previous migration attempt crashed or was interrupted.\n\n"+
		"Recovery options:\n"+
		"  1. If the migration partially applied, manually verify database state\n"+
		"  2. Run 'attune migrations repair --version %d' to mark it resolved\n"+
		"  3. If safe to retry, delete the row: DELETE FROM schema_migrations_feedback WHERE version = %d",
		e.Version, e.Filename, e.Version, e.Version)
}

// detectDirtyMigrations checks for migrations that started but didn't complete.
func detectDirtyMigrations(ctx context.Context, conn *pgxpool.Conn) error {
	var version int
	var filename string
	err := conn.QueryRow(ctx, fmt.Sprintf(`
		SELECT version, filename FROM %s WHERE success = FALSE LIMIT 1
	`, trackerTable)).Scan(&version, &filename)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("check dirty migrations: %w", err)
	}
	return ErrDirtyMigration{Version: version, Filename: filename}
}

// DetectDirtyMigrations checks for migrations in dirty state (success=FALSE).
// Returns ErrDirtyMigration if found, nil otherwise.
// Skips check if the database lacks extended columns (pre-070).
func DetectDirtyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if !hasExtendedColumns(ctx, conn) {
		// Pre-070 database, no success column to check
		return nil
	}

	return detectDirtyMigrations(ctx, conn)
}

// applyMigrationTx runs a migration and records it atomically in one transaction.
func applyMigrationTx(ctx context.Context, conn *pgxpool.Conn, version int, name string, body []byte, checksum string, start time.Time, hasExtendedCols bool) error {
	const where = "database.applyMigrationTx"
	tx, err := conn.Begin(ctx)
	if err != nil {
		logext.Errorf(ctx, "[%s] begin tx failed,version:%d,err:%+v", where, version, err.Error())
		return fmt.Errorf("begin tx for %d: %w", version, err)
	}
	if _, err := tx.Exec(ctx, string(body)); err != nil {
		_ = tx.Rollback(ctx)
		logext.Errorf(ctx, "[%s] apply failed,file:%s,err:%+v", where, name, err.Error())
		return fmt.Errorf("apply %s: %w", name, err)
	}
	duration := time.Since(start)
	var recordErr error
	if hasExtendedCols {
		_, recordErr = tx.Exec(ctx, recordMigrationSQL(), version, name, checksum, int(duration.Milliseconds()), Version)
	} else {
		_, recordErr = tx.Exec(ctx, recordMigrationLegacySQL(), version, name)
	}
	if recordErr != nil {
		_ = tx.Rollback(ctx)
		logext.Errorf(ctx, "[%s] record failed,file:%s,err:%+v", where, name, recordErr.Error())
		return fmt.Errorf("record %s: %w", name, recordErr)
	}
	if err := tx.Commit(ctx); err != nil {
		logext.Errorf(ctx, "[%s] commit failed,version:%d,err:%+v", where, version, err.Error())
		return fmt.Errorf("commit %d: %w", version, err)
	}
	return nil
}

// applyMigrationNoTx runs a single-statement migration OUTSIDE a transaction
// (for CREATE INDEX CONCURRENTLY et al.) then records it. The two steps are not
// atomic, so the body must be idempotent (see noTxDirective). conn.Exec with no
// args uses the simple protocol, which does not open an implicit transaction
// block — required for CONCURRENTLY.
//
// Uses dirty marker pattern: INSERT success=FALSE before apply, UPDATE to TRUE after.
// On crash between apply and update, detectDirtyMigrations() catches it on next startup.
func applyMigrationNoTx(ctx context.Context, conn *pgxpool.Conn, version int, name string, body []byte, checksum string, start time.Time, hasExtendedCols bool) error {
	const where = "database.applyMigrationNoTx"

	// Step 1: Mark migration as started (success=FALSE) - dirty marker
	if hasExtendedCols {
		_, err := conn.Exec(ctx, markMigrationStartedSQL(), version, name, checksum, Version)
		if err != nil {
			logext.Errorf(ctx, "[%s] mark started failed,file:%s,err:%+v", where, name, err.Error())
			return fmt.Errorf("mark started %s: %w", name, err)
		}
	}

	// Step 2: Apply migration
	if _, err := conn.Exec(ctx, string(body)); err != nil {
		logext.Errorf(ctx, "[%s] apply failed,file:%s,err:%+v", where, name, err.Error())
		return fmt.Errorf("apply %s (no-tx): %w", name, err)
	}

	// Step 3: Mark migration as complete (success=TRUE)
	duration := time.Since(start)
	if hasExtendedCols {
		_, err := conn.Exec(ctx, markMigrationCompleteSQL(), int(duration.Milliseconds()), version)
		if err != nil {
			logext.Errorf(ctx, "[%s] mark complete failed,file:%s,err:%+v", where, name, err.Error())
			return fmt.Errorf("mark complete %s: %w", name, err)
		}
	} else {
		_, err := conn.Exec(ctx, recordMigrationLegacySQL(), version, name)
		if err != nil {
			logext.Errorf(ctx, "[%s] record failed,file:%s,err:%+v", where, name, err.Error())
			return fmt.Errorf("record %s (no-tx): %w", name, err)
		}
	}
	return nil
}

// recordMigrationSQL returns the INSERT statement for recording a migration.
// Uses the extended schema with checksum/duration/applied_by/success columns.
// Sets success=TRUE since this is used for transactional migrations (atomic).
func recordMigrationSQL() string {
	return fmt.Sprintf(`
		INSERT INTO %s (version, filename, checksum, duration_ms, applied_by, success)
		VALUES ($1, $2, COALESCE($3, ''), $4, COALESCE($5, ''), TRUE)
		ON CONFLICT (version) DO UPDATE SET
			checksum = COALESCE(EXCLUDED.checksum, ''),
			duration_ms = EXCLUDED.duration_ms,
			applied_by = COALESCE(EXCLUDED.applied_by, ''),
			success = TRUE
	`, trackerTable)
}

// markMigrationStartedSQL inserts a dirty marker (success=FALSE) before applying.
// Used for no-tx migrations where apply and record are not atomic.
func markMigrationStartedSQL() string {
	return fmt.Sprintf(`
		INSERT INTO %s (version, filename, checksum, applied_by, success)
		VALUES ($1, $2, COALESCE($3, ''), COALESCE($4, ''), FALSE)
		ON CONFLICT (version) DO UPDATE SET success = FALSE
	`, trackerTable)
}

// markMigrationCompleteSQL updates a migration to success=TRUE after apply.
func markMigrationCompleteSQL() string {
	return fmt.Sprintf(`
		UPDATE %s SET success = TRUE, duration_ms = $1 WHERE version = $2
	`, trackerTable)
}

// recordMigrationLegacySQL returns the INSERT statement for pre-070 databases
// that don't have the extended columns.
func recordMigrationLegacySQL() string {
	return fmt.Sprintf("INSERT INTO %s (version, filename) VALUES ($1, $2)", trackerTable)
}

// backfillChecksums updates all applied migrations that have empty checksum.
// Called once after 070 is applied to populate checksums for 001-070.
func backfillChecksums(ctx context.Context, conn *pgxpool.Conn, names []string) error {
	const where = "database.backfillChecksums"
	for i, name := range names {
		version := i + 1
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			continue
		}
		checksum := Checksum(body)
		_, err = conn.Exec(ctx, fmt.Sprintf(`
			UPDATE %s SET checksum = $2, applied_by = COALESCE(NULLIF(applied_by, ''), $3)
			WHERE version = $1 AND checksum = ''
		`, trackerTable), version, checksum, Version)
		if err != nil {
			return fmt.Errorf("backfill version %d: %w", version, err)
		}
	}
	logext.Infof(ctx, "[%s] OK,count:%d", where, len(names))
	return nil
}

// MigrationCount returns the number of embedded migration SQL files.
func MigrationCount() int {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

// isNoTxMigration reports whether a migration must run outside a transaction.
// The directive must be on the FIRST line (as a SQL comment) so a stray mention
// of the string inside another migration's body can't silently flip it.
func isNoTxMigration(body []byte) bool {
	first := body
	if i := bytes.IndexByte(body, '\n'); i >= 0 {
		first = body[:i]
	}
	return bytes.Contains(first, []byte(noTxDirective))
}
