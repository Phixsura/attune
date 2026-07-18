// SPDX-License-Identifier: Apache-2.0

package systemsettings

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
)

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableRepo(t)

	expectRepoErr(t, "Get", func() error {
		_, err := r.Get(ctx, "tenant-1", KeyAuthMode)
		return err
	})
	expectRepoErr(t, "Set", func() error {
		return r.Set(ctx, "tenant-1", KeyAuthMode, string(domain.AuthModeHybrid), "admin-1")
	})
	expectRepoErr(t, "GetAuthMode", func() error {
		_, err := r.GetAuthMode(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "SetAuthMode", func() error {
		return r.SetAuthMode(ctx, "tenant-1", domain.AuthModeSSOnly, "admin-1")
	})
	if err := r.SetAuthMode(ctx, "tenant-1", domain.AuthMode("invalid"), "admin-1"); err == nil {
		t.Fatalf("SetAuthMode(invalid) error = nil")
	}
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
	return NewRepo(pool)
}

func expectRepoErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
