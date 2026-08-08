// SPDX-License-Identifier: Apache-2.0

package idempotency

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
	r := newUnreachableIdempotencyRepo(t)
	hash := []byte("request-hash")

	expectIdempotencyErr(t, "Acquire", func() error {
		_, _, err := r.Acquire(ctx, "tenant-1", "key-1", hash, 0)
		return err
	})
	expectIdempotencyErr(t, "Complete", func() error {
		return r.Complete(ctx, "tenant-1", "key-1", 200, []byte(`{"ok":true}`))
	})
	expectIdempotencyErr(t, "Fail", func() error {
		return r.Fail(ctx, "tenant-1", "key-1")
	})
	expectIdempotencyErr(t, "Get", func() error {
		_, err := r.Get(ctx, "tenant-1", "key-1")
		return err
	})
	expectIdempotencyErr(t, "DeleteExpired", func() error {
		_, err := r.DeleteExpired(ctx, "tenant-1", "key-1")
		return err
	})
	expectIdempotencyErr(t, "CleanupExpired", func() error {
		_, err := r.CleanupExpired(ctx)
		return err
	})
}

func newUnreachableIdempotencyRepo(t *testing.T) *Repo {
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
	return New(pool)
}

func expectIdempotencyErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
