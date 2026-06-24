// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Record appends a drill report to restore_drill_runs on the production pool so
// the readiness surface and audit history can read it.
func Record(ctx context.Context, prod *pgxpool.Pool, rep DrillReport) error {
	payload, err := json.Marshal(rep)
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	// Fail closed: a status outside the enum would either violate the table's
	// CHECK (opaque error) or, worse, corrupt this append-only audit ledger.
	// Coerce an unrecognized/unset status to "fail" so a malformed report is
	// recorded as a failure, never as a silently-acceptable row.
	status := string(rep.Status)
	switch rep.Status {
	case StatusPass, StatusWarn, StatusFail, StatusSkip:
	default:
		status = string(StatusFail)
	}
	_, err = prod.Exec(ctx, `
		INSERT INTO restore_drill_runs
			(ran_at, status, backup_ref, schema_version, duration_ms, rpo_seconds, rto_seconds, report, attune_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		rep.StartedAt, status, rep.BackupRef, rep.SchemaVersion,
		rep.DurationMS, rep.RPOSeconds, rep.RTOSeconds, payload, rep.AttuneVersion)
	if err != nil {
		return fmt.Errorf("insert restore_drill_runs: %w", err)
	}
	return nil
}

// HistoryRow is one row of the drill trend, newest first.
type HistoryRow struct {
	RanAt      time.Time `json:"ran_at"`
	Status     Status    `json:"status"`
	BackupRef  string    `json:"backup_ref"`
	DurationMS int64     `json:"duration_ms"`
	RPOSeconds *int64    `json:"rpo_seconds,omitempty"`
	RTOSeconds *int64    `json:"rto_seconds,omitempty"`
}

// History returns the most recent drills, newest first, for trend inspection.
func History(ctx context.Context, prod *pgxpool.Pool, limit int) ([]HistoryRow, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := prod.Query(ctx, `
		SELECT ran_at, status, backup_ref, duration_ms, rpo_seconds, rto_seconds
		FROM restore_drill_runs
		ORDER BY ran_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("query restore drill history: %w", err)
	}
	defer rows.Close()
	var out []HistoryRow
	for rows.Next() {
		var h HistoryRow
		var status string
		if err := rows.Scan(&h.RanAt, &status, &h.BackupRef, &h.DurationMS, &h.RPOSeconds, &h.RTOSeconds); err != nil {
			return nil, fmt.Errorf("scan restore drill history: %w", err)
		}
		h.Status = Status(status)
		out = append(out, h)
	}
	return out, rows.Err()
}

// LastRun is the most recent recorded drill, for the readiness check and the
// status command.
type LastRun struct {
	RanAt      time.Time       `json:"ran_at"`
	Status     Status          `json:"status"`
	BackupRef  string          `json:"backup_ref"`
	DurationMS int64           `json:"duration_ms"`
	Report     json.RawMessage `json:"report"`
}

// ReadLast returns the latest recorded drill, or ok=false if none exist.
func ReadLast(ctx context.Context, prod *pgxpool.Pool) (LastRun, bool, error) {
	var lr LastRun
	var status string
	err := prod.QueryRow(ctx, `
		SELECT ran_at, status, backup_ref, duration_ms, report
		FROM restore_drill_runs
		ORDER BY ran_at DESC
		LIMIT 1`).Scan(&lr.RanAt, &status, &lr.BackupRef, &lr.DurationMS, &lr.Report)
	if errors.Is(err, pgx.ErrNoRows) {
		return LastRun{}, false, nil
	}
	if err != nil {
		return LastRun{}, false, fmt.Errorf("read last restore drill: %w", err)
	}
	lr.Status = Status(status)
	return lr, true, nil
}
