// SPDX-License-Identifier: Apache-2.0

package restoredrill

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRecordErrorBranches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := newUnreachableRestoreDrillPool(t)
	report := DrillReport{
		Status:     StatusPass,
		StartedAt:  time.Now(),
		BackupRef:  "backup-1",
		DurationMS: 25,
		Checks:     []CheckResult{{Name: "connectivity", Status: StatusPass}},
	}

	err := Record(ctx, pool, DrillReport{
		Status:    StatusPass,
		StartedAt: time.Now(),
		Checks: []CheckResult{{
			Name:   "bad-detail",
			Status: StatusFail,
			Detail: func() {},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "marshal report") {
		t.Fatalf("Record marshal error = %v, want marshal report", err)
	}

	err = Record(ctx, pool, report)
	if err == nil || !strings.Contains(err.Error(), "insert restore_drill_runs") {
		t.Fatalf("Record insert error = %v, want insert restore_drill_runs", err)
	}
}

func TestHistoryQueryError(t *testing.T) {
	t.Parallel()

	_, err := History(context.Background(), newUnreachableRestoreDrillPool(t), 0)
	if err == nil || !strings.Contains(err.Error(), "query restore drill history") {
		t.Fatalf("History error = %v, want query restore drill history", err)
	}
}

func newUnreachableRestoreDrillPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
