// SPDX-License-Identifier: Apache-2.0

package taskoutbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestQueueMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	q := newUnreachableQueue(t)

	expectRepoErr(t, "TryClaim", func() error {
		_, err := q.TryClaim(ctx, time.Minute)
		return err
	})
	expectRepoErr(t, "TryClaimWithOwner", func() error {
		_, err := q.TryClaimWithOwner(ctx, time.Minute, "worker-1")
		return err
	})
	expectRepoErr(t, "MarkDone", func() error {
		_, err := q.MarkDone(ctx, 1, "worker-1")
		return err
	})
	expectRepoErr(t, "MarkFailed", func() error {
		_, err := q.MarkFailed(ctx, 1, "worker-1", errors.New("failed"), 3)
		return err
	})
	expectRepoErr(t, "ResetStaleClaims", func() error {
		_, err := q.ResetStaleClaims(ctx, time.Minute)
		return err
	})
	expectRepoErr(t, "RefreshClaim", func() error {
		_, err := q.RefreshClaim(ctx, 1, "worker-1")
		return err
	})
	expectRepoErr(t, "QueueDepthByTenant", func() error {
		_, err := q.QueueDepthByTenant(ctx)
		return err
	})
	expectRepoErr(t, "QueueDepth", func() error {
		_, err := q.QueueDepth(ctx, "tenant-1")
		return err
	})
}

func newUnreachableQueue(t *testing.T) *Queue {
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
	return New(pool, "embedding_task", "enable_llm")
}

func expectRepoErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
