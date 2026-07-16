// SPDX-License-Identifier: Apache-2.0

package oidcuser

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
	r := newUnreachableOIDCUserRepo(t)

	expectOIDCUserErr(t, "GetByExternalID", func() error {
		_, err := r.GetByExternalID(ctx, "google", "external-1")
		return err
	})
	expectOIDCUserErr(t, "GetByID", func() error {
		_, err := r.GetByID(ctx, "user-1")
		return err
	})
	expectOIDCUserErr(t, "Upsert", func() error {
		_, err := r.Upsert(ctx, domain.OIDCUser{
			Provider:    "google",
			ExternalID:  "external-1",
			Email:       "user@example.test",
			DisplayName: "User",
			Role:        "member",
			Groups:      []string{"support"},
		})
		return err
	})
}

func newUnreachableOIDCUserRepo(t *testing.T) *Repo {
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

func expectOIDCUserErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
