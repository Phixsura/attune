// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTenantRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableTenantRepo(t)

	expectRepoErr(t, "FirstActiveID", func() error {
		_, err := r.FirstActiveID(ctx)
		return err
	})
	expectRepoErr(t, "ResolveSlug", func() error {
		_, err := r.ResolveSlug(ctx, "acme")
		return err
	})
	expectRepoErr(t, "Create validates slug", func() error {
		_, err := r.Create(ctx, "", "Acme")
		return err
	})
	expectRepoErr(t, "Create", func() error {
		_, err := r.Create(ctx, "acme", "Acme")
		return err
	})
	expectRepoErr(t, "GetByID", func() error {
		_, err := r.GetByID(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "GetClusteringEnabled", func() error {
		_, err := r.GetClusteringEnabled(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "SetClusteringEnabled", func() error {
		return r.SetClusteringEnabled(ctx, "tenant-1", true)
	})
}

func TestTenantRepoEnrichConfigMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableTenantRepo(t)
	cfg := EnrichConfig{}

	expectRepoErr(t, "GetEnrichConfig", func() error {
		_, err := r.GetEnrichConfig(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "GetEnrichConfigWithLocale", func() error {
		_, err := r.GetEnrichConfigWithLocale(ctx, "tenant-1")
		return err
	})
	expectRepoErr(t, "UpdateEnrichConfig", func() error {
		return r.UpdateEnrichConfig(ctx, "tenant-1", cfg)
	})
	expectRepoErr(t, "SaveEnrichConfigVersionAndActivate", func() error {
		_, err := r.SaveEnrichConfigVersionAndActivate(ctx, "tenant-1", cfg, map[string]any{"ok": true}, "v1")
		return err
	})
	expectRepoErr(t, "ListEnrichPromptVersions", func() error {
		_, err := r.ListEnrichPromptVersions(ctx, "tenant-1", 10)
		return err
	})
	expectRepoErr(t, "ListEnrichPromptVersionsPage", func() error {
		_, err := r.ListEnrichPromptVersionsPage(ctx, "tenant-1", ListEnrichPromptVersionsFilter{Limit: 100})
		return err
	})
	expectRepoErr(t, "GetEnrichPromptVersion", func() error {
		_, err := r.GetEnrichPromptVersion(ctx, "tenant-1", "version-1")
		return err
	})
	expectRepoErr(t, "ActivateEnrichPromptVersion", func() error {
		_, err := r.ActivateEnrichPromptVersion(ctx, "tenant-1", "version-1")
		return err
	})
}

func TestTenantUserRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableTenantUserRepo(t)

	expectRepoErr(t, "Upsert", func() error {
		_, err := r.Upsert(ctx, "tenant-1", "open-1", "Jane", "", "")
		return err
	})
	expectRepoErr(t, "GetByID", func() error {
		_, err := r.GetByID(ctx, "user-1")
		return err
	})
	expectRepoErr(t, "TouchLastSeen", func() error {
		return r.TouchLastSeen(ctx, "user-1")
	})
}

func newUnreachableTenantRepo(t *testing.T) *TenantRepo {
	t.Helper()
	return NewTenant(newUnreachablePool(t))
}

func newUnreachableTenantUserRepo(t *testing.T) *TenantUserRepo {
	t.Helper()
	return NewTenantUserRepo(newUnreachablePool(t))
}

func newUnreachablePool(t *testing.T) *pgxpool.Pool {
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

func expectRepoErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}

func TestGetTimezoneErrorPath(t *testing.T) {
	r := newUnreachableTenantRepo(t)
	if _, err := r.GetTimezone(context.Background(), "t1"); err == nil {
		t.Fatal("GetTimezone() error = nil, want pool error")
	}
}
