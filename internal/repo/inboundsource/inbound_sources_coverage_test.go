// SPDX-License-Identifier: Apache-2.0

package inboundsource

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/inbound"
)

func TestRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := newUnreachableInboundRepo(t)

	expectInboundErr(t, "List", func() error {
		_, err := r.List(ctx, "email")
		return err
	})
	expectInboundErr(t, "Get", func() error {
		_, err := r.Get(ctx, "source-1")
		return err
	})
	expectInboundErr(t, "GetBySlugs", func() error {
		_, err := r.GetBySlugs(ctx, "acme", "email", "support")
		return err
	})
	expectInboundErr(t, "UpdateState", func() error {
		return r.UpdateState(ctx, "source-1", inbound.SourceState{
			LastUID:   42,
			LastError: "boom",
		})
	})
	expectInboundErr(t, "SetEnabled", func() error {
		return r.SetEnabled(ctx, "source-1", false, "paused")
	})
	expectInboundErr(t, "UpdateConfig", func() error {
		return r.UpdateConfig(ctx, "source-1", []byte(`{"cursor":"1"}`))
	})
}

func newUnreachableInboundRepo(t *testing.T) *Repo {
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

func expectInboundErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
