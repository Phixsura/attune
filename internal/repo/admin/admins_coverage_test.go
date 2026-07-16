// SPDX-License-Identifier: Apache-2.0

package admin

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
	r := newUnreachableAdminRepo(t)
	newAdmin := NewAdmin{
		Email:        "admin@example.test",
		PasswordHash: "hash",
		DisplayName:  "Admin",
		Role:         "admin",
	}

	expectAdminErr(t, "Create", func() error {
		_, err := r.Create(ctx, newAdmin)
		return err
	})
	expectAdminErr(t, "GetByEmail", func() error {
		_, err := r.GetByEmail(ctx, "admin@example.test")
		return err
	})
	expectAdminErr(t, "GetByID", func() error {
		_, err := r.GetByID(ctx, "admin-1")
		return err
	})
	expectAdminErr(t, "IncrementFailedAttempts", func() error {
		return r.IncrementFailedAttempts(ctx, "admin-1")
	})
	expectAdminErr(t, "UpdatePasswordHash", func() error {
		return r.UpdatePasswordHash(ctx, "admin-1", "new-hash")
	})
	expectAdminErr(t, "ResetFailedAttempts", func() error {
		return r.ResetFailedAttempts(ctx, "admin-1")
	})
	expectAdminErr(t, "Count", func() error {
		_, err := r.Count(ctx)
		return err
	})
	expectAdminErr(t, "Bootstrap", func() error {
		return r.Bootstrap(ctx, newAdmin)
	})
}

func newUnreachableAdminRepo(t *testing.T) *Repo {
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

func expectAdminErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
