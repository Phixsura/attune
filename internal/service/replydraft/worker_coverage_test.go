// SPDX-License-Identifier: Apache-2.0

package replydraft

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
)

func TestDraftWorkerProcessOnceHandlesClaimErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	w := newUnreachableDraftWorker(t)

	w.ProcessOnce(ctx)
}

func TestDraftWorkerRunStopsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := newUnreachableDraftWorker(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		w.Run(ctx)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

func TestDraftWorkerHeartbeatReturnsOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := newUnreachableDraftWorker(t)
	cancelled := false

	w.heartbeat(ctx, 42, func() { cancelled = true })
	if cancelled {
		t.Fatal("heartbeat called cancelTask after parent context cancellation")
	}
}

func newUnreachableDraftWorker(t *testing.T) *DraftWorker {
	t.Helper()
	return NewWorker(replydraftrepo.NewDraftTaskRepo(newUnreachableDraftWorkerPool(t)), nil)
}

func newUnreachableDraftWorkerPool(t *testing.T) *pgxpool.Pool {
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
