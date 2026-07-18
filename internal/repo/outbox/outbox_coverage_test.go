// SPDX-License-Identifier: Apache-2.0

package outbox

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)

	expectRepoErr(t, "ClaimBatch", func() error {
		_, err := r.ClaimBatch(ctx, 10, "worker-1")
		return err
	})
	expectRepoErr(t, "MarkDelivered", func() error {
		_, err := r.MarkDelivered(ctx, 1, "worker-1")
		return err
	})
	expectRepoErr(t, "MarkFailed", func() error {
		_, err := r.MarkFailed(ctx, 1, "worker-1", "failed", "network", 502, time.Second)
		return err
	})
	expectRepoErr(t, "MarkDead", func() error {
		_, err := r.MarkDead(ctx, 1, "worker-1", "dead", "terminal", 400)
		return err
	})
	expectRepoErr(t, "PruneStalePending", func() error {
		_, err := r.PruneStalePending(ctx, time.Now().Add(-time.Hour), "stale")
		return err
	})
	expectRepoErr(t, "RefreshClaims", func() error {
		_, err := r.RefreshClaims(ctx, []int64{1, 2}, "worker-1")
		return err
	})
	if n, err := r.RefreshClaims(ctx, nil, "worker-1"); err != nil || n != 0 {
		t.Fatalf("RefreshClaims(empty) = %d, %v; want 0, nil", n, err)
	}
	expectRepoErr(t, "ResetStaleClaims", func() error {
		_, err := r.ResetStaleClaims(ctx)
		return err
	})
	expectRepoErr(t, "OldestPendingAge", func() error {
		_, err := r.OldestPendingAge(ctx)
		return err
	})
	expectRepoErr(t, "DeadCount", func() error {
		_, err := r.DeadCount(ctx)
		return err
	})
	expectRepoErr(t, "ListByStatus", func() error {
		_, err := r.ListByStatus(ctx, "tenant-1", []string{OutboxStatusDead}, 0, 0)
		return err
	})
	expectRepoErr(t, "GetByID", func() error {
		_, err := r.GetByID(ctx, "tenant-1", 1)
		return err
	})
	expectRepoErr(t, "RetryOne", func() error {
		_, err := r.RetryOne(ctx, "tenant-1", 1, "user-1", nil)
		return err
	})
}

func newUnreachableRepo(t *testing.T) *OutboxRepo {
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
	return NewOutbox(pool)
}

func expectRepoErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
