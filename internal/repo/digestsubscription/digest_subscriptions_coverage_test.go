// SPDX-License-Identifier: Apache-2.0

package digestsubscription

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)
	nextRunAt := time.Now().Add(time.Hour)
	sub := Subscription{
		TenantID:       "tenant-1",
		Enabled:        true,
		Frequency:      "daily",
		SendHour:       9,
		LLMMinFeedback: 3,
		SendOnEmpty:    false,
		NextRunAt:      ptrext.Of(nextRunAt),
	}

	expectRepoErr(t, "GetByTenant", func() error {
		_, err := r.GetByTenant(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "Upsert", func() error {
		_, err := r.Upsert(ctx, sub)
		return err
	})
	expectRepoErr(t, "DeleteByTenant", func() error {
		return r.DeleteByTenant(ctx, "tenant-1")
	})
	expectRepoErr(t, "FindDue", func() error {
		_, err := r.FindDue(ctx, time.Now())
		return err
	})
	expectRepoErr(t, "GetResolved", func() error {
		_, err := r.GetResolved(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "BeginTx", func() error {
		tx, err := r.BeginTx(ctx)
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
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
