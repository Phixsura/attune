// SPDX-License-Identifier: Apache-2.0

package digestrun

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)

	expectRepoErr(t, "TryClaim", func() error {
		_, err := r.TryClaim(ctx, time.Minute)
		return err
	})
	expectRepoErr(t, "TryClaimWithOwner", func() error {
		_, err := r.TryClaimWithOwner(ctx, time.Minute, "worker-1")
		return err
	})
	expectRepoErr(t, "RefreshClaim", func() error {
		_, err := r.RefreshClaim(ctx, 1, "worker-1")
		return err
	})
	expectRepoErr(t, "MarkSent", func() error {
		_, err := r.MarkSent(ctx, 1, "worker-1", 3, 2)
		return err
	})
	expectRepoErr(t, "MarkSkippedEmpty", func() error {
		_, err := r.MarkSkippedEmpty(ctx, 1, "worker-1", 0)
		return err
	})
	expectRepoErr(t, "MarkFailed", func() error {
		_, err := r.MarkFailed(ctx, 1, "worker-1", errors.New("failed"), 3)
		return err
	})
	expectRepoErr(t, "ResetStaleClaims", func() error {
		_, err := r.ResetStaleClaims(ctx, time.Minute)
		return err
	})
}

func newUnreachableRepo(t *testing.T) *Repo {
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

func expectRepoErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
