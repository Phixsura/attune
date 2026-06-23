package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// MigrationDetail represents status information for a single migration.
type MigrationDetail struct {
	Version    int     `json:"version"`
	Filename   string  `json:"filename"`
	Status     string  `json:"status"` // "applied", "pending", "missing"
	AppliedAt  *string `json:"applied_at,omitempty"`
	DurationMs *int    `json:"duration_ms,omitempty"`
	AppliedBy  *string `json:"applied_by,omitempty"`
	Checksum   *string `json:"checksum,omitempty"`
}

// MigrationStatus represents the combined state for CLI/API output.
type MigrationStatus struct {
	Total      int               `json:"total"`
	Applied    int               `json:"applied"`
	Pending    int               `json:"pending"`
	Migrations []MigrationDetail `json:"migrations"`
	Checksums  ChecksumStatus    `json:"checksums"`
	Duplicates []DuplicatePrefix `json:"duplicates"`
	Elapsed    string            `json:"elapsed"`
}

// MigrationFile represents an embedded migration file for dry-run.
type MigrationFile struct {
	Name     string
	Version  int
	Body     []byte
	Checksum string
	NoTx     bool
}

// GetMigrationStatus returns the full migration status for CLI/API.
func GetMigrationStatus(ctx context.Context, pool *pgxpool.Pool) (MigrationStatus, error) {
	start := time.Now()

	names, err := LoadMigrationNames()
	if err != nil {
		return MigrationStatus{}, err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return MigrationStatus{}, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	appliedMap, err := queryAppliedMigrations(ctx, conn)
	if err != nil {
		return MigrationStatus{}, err
	}

	status := buildMigrationStatus(names, appliedMap)
	status.Checksums, _ = GetChecksumStatus(ctx, pool)
	status.Elapsed = time.Since(start).Truncate(time.Millisecond).String()
	return status, nil
}

func queryAppliedMigrations(ctx context.Context, conn *pgxpool.Conn) (map[int]MigrationDetail, error) {
	hasExtended := hasExtendedColumns(ctx, conn)
	query := buildMigrationQuery(hasExtended)

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query migrations: %w", err)
	}
	defer rows.Close()

	appliedMap := make(map[int]MigrationDetail)
	for rows.Next() {
		d, err := scanMigrationRow(rows, hasExtended)
		if err != nil {
			return nil, err
		}
		appliedMap[d.Version] = d
	}
	return appliedMap, rows.Err()
}

func buildMigrationQuery(hasExtended bool) string {
	if hasExtended {
		return `SELECT version, filename, applied_at, checksum, duration_ms, applied_by
		        FROM schema_migrations_feedback ORDER BY version`
	}
	return `SELECT version, filename, applied_at FROM schema_migrations_feedback ORDER BY version`
}

func scanMigrationRow(rows pgx.Rows, hasExtended bool) (MigrationDetail, error) {
	var d MigrationDetail
	var appliedAt time.Time

	if hasExtended {
		var checksum, appliedBy *string
		var durationMs *int
		if err := rows.Scan(&d.Version, &d.Filename, &appliedAt, &checksum, &durationMs, &appliedBy); err != nil {
			return d, fmt.Errorf("scan row: %w", err)
		}
		if checksum != nil && ptrext.Indirect(checksum) != "" {
			d.Checksum = checksum
		}
		d.DurationMs = durationMs
		if appliedBy != nil && ptrext.Indirect(appliedBy) != "" {
			d.AppliedBy = appliedBy
		}
	} else {
		if err := rows.Scan(&d.Version, &d.Filename, &appliedAt); err != nil {
			return d, fmt.Errorf("scan row: %w", err)
		}
	}

	ts := appliedAt.Format(time.RFC3339)
	d.AppliedAt = ptrext.Of(ts)
	d.Status = "applied"
	return d, nil
}

func buildMigrationStatus(names []string, appliedMap map[int]MigrationDetail) MigrationStatus {
	status := MigrationStatus{
		Total:      len(names),
		Migrations: make([]MigrationDetail, 0, len(names)),
	}

	for i, name := range names {
		version := i + 1
		if d, ok := appliedMap[version]; ok {
			status.Migrations = append(status.Migrations, d)
			status.Applied++
		} else {
			status.Migrations = append(status.Migrations, MigrationDetail{
				Version:  version,
				Filename: name,
				Status:   "pending",
			})
			status.Pending++
		}
	}

	if err := DetectDuplicatePrefixes(names); err != nil {
		var dupErr ErrDuplicatePrefix
		if errors.As(err, &dupErr) {
			status.Duplicates = dupErr.Duplicates
		}
	}
	return status
}

// GetPendingMigrations returns the list of pending migrations with their content.
func GetPendingMigrations(ctx context.Context, pool *pgxpool.Pool) ([]MigrationFile, error) {
	names, err := LoadMigrationNames()
	if err != nil {
		return nil, err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	appliedSet, err := queryAppliedVersions(ctx, conn)
	if err != nil {
		return nil, err
	}

	return buildPendingMigrations(names, appliedSet)
}

func queryAppliedVersions(ctx context.Context, conn *pgxpool.Conn) (map[int]bool, error) {
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations_feedback`)
	if err != nil {
		return nil, fmt.Errorf("query applied: %w", err)
	}
	defer rows.Close()

	appliedSet := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		appliedSet[version] = true
	}
	return appliedSet, rows.Err()
}

func buildPendingMigrations(names []string, appliedSet map[int]bool) ([]MigrationFile, error) {
	var pending []MigrationFile
	for i, name := range names {
		version := i + 1
		if appliedSet[version] {
			continue
		}

		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}

		pending = append(pending, MigrationFile{
			Name:     name,
			Version:  version,
			Body:     body,
			Checksum: Checksum(body),
			NoTx:     isNoTxMigration(body),
		})
	}
	return pending, nil
}
