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
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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

	// 1. Ensure tracker table exists (base schema, new columns added by 070)
	if _, err := conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			version INT PRIMARY KEY,
			filename TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`, trackerTable)); err != nil {
		logext.Errorf(ctx, "[%s] create tracker failed,err:%+v", where, err.Error())
		return fmt.Errorf("create %s: %w", trackerTable, err)
	}

	// 2. Load embedded migrations
	names, err := LoadMigrationNames()
	if err != nil {
		logext.Errorf(ctx, "[%s] load migration names failed,err:%+v", where, err.Error())
		return err
	}

	// 3. Detect duplicate prefixes (fail before any apply)
	if err := DetectDuplicatePrefixes(names); err != nil {
		logext.Errorf(ctx, "[%s] duplicate prefixes,err:%+v", where, err.Error())
		return err
	}

	// 4. Verify checksums of already-applied migrations
	if err := VerifyChecksumsConn(ctx, conn); err != nil {
		logext.Errorf(ctx, "[%s] checksum verification failed,err:%+v", where, err.Error())
		return err
	}

	// 5. Apply pending migrations
	// Track whether extended columns exist (checksum/duration_ms/applied_by).
	// Pre-070 databases don't have them; 070 adds them.
	hasExtendedCols := hasExtendedColumns(ctx, conn)

	for i, name := range names {
		version := i + 1
		var applied bool
		if err := conn.QueryRow(
			ctx,
			fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE version=$1)", trackerTable),
			version,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if applied {
			continue
		}

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

		// After applying 070, extended columns become available
		if !hasExtendedCols {
			hasExtendedCols = hasExtendedColumns(ctx, conn)
		}

		duration := time.Since(start)
		logext.Infof(ctx, "[%s] applied,version:%d,file:%s,duration:%v", where, version, name, duration)
	}
	logext.Infof(ctx, "[%s] OK", where)
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
func applyMigrationNoTx(ctx context.Context, conn *pgxpool.Conn, version int, name string, body []byte, checksum string, start time.Time, hasExtendedCols bool) error {
	const where = "database.applyMigrationNoTx"
	if _, err := conn.Exec(ctx, string(body)); err != nil {
		logext.Errorf(ctx, "[%s] apply failed,file:%s,err:%+v", where, name, err.Error())
		return fmt.Errorf("apply %s (no-tx): %w", name, err)
	}
	duration := time.Since(start)
	var recordErr error
	if hasExtendedCols {
		_, recordErr = conn.Exec(ctx, recordMigrationSQL(), version, name, checksum, int(duration.Milliseconds()), Version)
	} else {
		_, recordErr = conn.Exec(ctx, recordMigrationLegacySQL(), version, name)
	}
	if recordErr != nil {
		logext.Errorf(ctx, "[%s] record failed,file:%s,err:%+v", where, name, recordErr.Error())
		return fmt.Errorf("record %s (no-tx): %w", name, recordErr)
	}
	return nil
}

// recordMigrationSQL returns the INSERT statement for recording a migration.
// Uses the extended schema with checksum/duration/applied_by columns.
func recordMigrationSQL() string {
	return fmt.Sprintf(`
		INSERT INTO %s (version, filename, checksum, duration_ms, applied_by)
		VALUES ($1, $2, COALESCE($3, ''), $4, COALESCE($5, ''))
		ON CONFLICT (version) DO UPDATE SET
			checksum = COALESCE(EXCLUDED.checksum, ''),
			duration_ms = EXCLUDED.duration_ms,
			applied_by = COALESCE(EXCLUDED.applied_by, '')
	`, trackerTable)
}

// recordMigrationLegacySQL returns the INSERT statement for pre-070 databases
// that don't have the extended columns.
func recordMigrationLegacySQL() string {
	return fmt.Sprintf("INSERT INTO %s (version, filename) VALUES ($1, $2)", trackerTable)
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
