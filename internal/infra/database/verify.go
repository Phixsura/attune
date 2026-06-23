package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ChecksumDrift represents a mismatch between stored and computed checksum.
type ChecksumDrift struct {
	Version  int    `json:"version"`
	Filename string `json:"filename"`
	Stored   string `json:"stored"`   // Truncated for display
	Computed string `json:"computed"` // Truncated for display
}

// ErrChecksumDrift is returned when an applied migration's checksum doesn't match.
type ErrChecksumDrift struct {
	Drifted []ChecksumDrift
}

func (e ErrChecksumDrift) Error() string {
	var sb strings.Builder
	sb.WriteString("migration checksum drift detected:\n")
	for _, d := range e.Drifted {
		fmt.Fprintf(&sb, "  %03d %s\n      stored:   %s\n      computed: %s\n",
			d.Version, d.Filename, d.Stored, d.Computed)
	}
	sb.WriteString("\nPossible causes:\n")
	sb.WriteString("  - Migration file was edited after being applied\n")
	sb.WriteString("  - Binary was built from different source than what's in the database\n")
	sb.WriteString("\nRecovery options:\n")
	sb.WriteString("  - Restore original migration file from git history\n")
	sb.WriteString("  - If change was intentional, update stored checksum (see docs/private-deploy.md)\n")
	return sb.String()
}

// ErrMissingFile is returned when an applied migration's file is missing from the binary.
type ErrMissingFile struct {
	Version  int
	Filename string
}

func (e ErrMissingFile) Error() string {
	return fmt.Sprintf("migration %03d (%s) was applied but file is missing from binary\n\n"+
		"Possible causes:\n"+
		"  - Migration file was deleted from source\n"+
		"  - Binary was built from incompatible branch\n\n"+
		"Recovery: restore the migration file or rebuild from correct source.",
		e.Version, e.Filename)
}

// VerifyChecksums compares embedded files against stored checksums.
// Only verifies rows with non-empty checksum (skips legacy rows).
// Returns nil if all checksums match or no checksums are stored.
func VerifyChecksums(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	return VerifyChecksumsConn(ctx, conn)
}

// VerifyChecksumsConn verifies checksums using an existing connection.
func VerifyChecksumsConn(ctx context.Context, conn *pgxpool.Conn) error {
	// Check if checksum column exists (for pre-070 databases)
	var hasColumn bool
	err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'schema_migrations_feedback' AND column_name = 'checksum'
		)`).Scan(&hasColumn)
	if err != nil {
		return fmt.Errorf("check checksum column: %w", err)
	}
	if !hasColumn {
		// Pre-070 database, no checksums to verify
		return nil
	}

	rows, err := conn.Query(ctx,
		`SELECT version, filename, checksum FROM schema_migrations_feedback
         WHERE checksum != '' ORDER BY version`)
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()

	var drifted []ChecksumDrift
	for rows.Next() {
		var version int
		var filename, storedChecksum string
		if err := rows.Scan(&version, &filename, &storedChecksum); err != nil {
			return fmt.Errorf("scan migration row: %w", err)
		}

		body, err := migrationFS.ReadFile("migrations/" + filename)
		if err != nil {
			return ErrMissingFile{Version: version, Filename: filename}
		}

		computed := Checksum(body)
		if computed != storedChecksum {
			drifted = append(drifted, ChecksumDrift{
				Version:  version,
				Filename: filename,
				Stored:   ChecksumShort(storedChecksum),
				Computed: ChecksumShort(computed),
			})
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate migration rows: %w", err)
	}

	if len(drifted) > 0 {
		return ErrChecksumDrift{Drifted: drifted}
	}
	return nil
}

// ChecksumStatus represents the result of checksum verification.
type ChecksumStatus struct {
	Verified int             `json:"verified"`
	Total    int             `json:"total"`
	Drifted  []ChecksumDrift `json:"drifted,omitempty"`
}

// GetChecksumStatus returns detailed checksum verification status.
func GetChecksumStatus(ctx context.Context, pool *pgxpool.Pool) (ChecksumStatus, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return ChecksumStatus{}, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	// Check if checksum column exists
	var hasColumn bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'schema_migrations_feedback' AND column_name = 'checksum'
		)`).Scan(&hasColumn)
	if err != nil {
		return ChecksumStatus{}, fmt.Errorf("check checksum column: %w", err)
	}
	if !hasColumn {
		return ChecksumStatus{}, nil
	}

	rows, err := conn.Query(ctx,
		`SELECT version, filename, checksum FROM schema_migrations_feedback
         WHERE checksum != '' ORDER BY version`)
	if err != nil {
		return ChecksumStatus{}, fmt.Errorf("query migrations: %w", err)
	}
	defer rows.Close()

	var status ChecksumStatus
	for rows.Next() {
		var version int
		var filename, storedChecksum string
		if err := rows.Scan(&version, &filename, &storedChecksum); err != nil {
			return ChecksumStatus{}, fmt.Errorf("scan row: %w", err)
		}

		status.Total++
		body, err := migrationFS.ReadFile("migrations/" + filename)
		if err != nil {
			status.Drifted = append(status.Drifted, ChecksumDrift{
				Version:  version,
				Filename: filename,
				Stored:   ChecksumShort(storedChecksum),
				Computed: "(missing file)",
			})
			continue
		}

		computed := Checksum(body)
		if computed == storedChecksum {
			status.Verified++
		} else {
			status.Drifted = append(status.Drifted, ChecksumDrift{
				Version:  version,
				Filename: filename,
				Stored:   ChecksumShort(storedChecksum),
				Computed: ChecksumShort(computed),
			})
		}
	}

	return status, rows.Err()
}
