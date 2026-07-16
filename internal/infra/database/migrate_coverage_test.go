// SPDX-License-Identifier: Apache-2.0

package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunMigrationsReturnsAcquireError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := RunMigrations(ctx, newUnreachableMigrationPool(t))
	if err == nil {
		t.Fatal("RunMigrations() error = nil, want acquire error")
	}
	if !strings.Contains(err.Error(), "failed to connect") && !strings.Contains(err.Error(), "connect") {
		t.Fatalf("RunMigrations() error = %v, want connection failure", err)
	}
}

func TestDetectDirtyMigrationsReturnsAcquireError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := DetectDirtyMigrations(ctx, newUnreachableMigrationPool(t))
	if err == nil {
		t.Fatal("DetectDirtyMigrations() error = nil, want acquire error")
	}
	if !strings.Contains(err.Error(), "acquire connection") {
		t.Fatalf("DetectDirtyMigrations() error = %v, want acquire connection wrapper", err)
	}
}

func newUnreachableMigrationPool(t *testing.T) *pgxpool.Pool {
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
