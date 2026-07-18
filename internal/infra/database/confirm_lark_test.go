// SPDX-License-Identifier: Apache-2.0

package database_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/database"
)

// Unit smoke — verifies the config-bypass branch without needing a DB. The
// guard MUST return nil immediately when the operator has opted in, regardless
// of database state (it never even runs the count query).
func TestConfirmLarkDelete_ConfigOptInBypassesDB(t *testing.T) {
	if err := database.ConfirmLarkDelete(context.Background(), nil, true); err != nil {
		t.Errorf("config opt-in path returned %v; want nil", err)
	}
}

func TestConfirmLarkDelete_TableCheckError(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := database.ConfirmLarkDelete(ctx, newUnreachableConfirmPool(t), false)
	if err == nil {
		t.Fatal("ConfirmLarkDelete() error = nil, want table check error")
	}
	if !strings.Contains(err.Error(), "ConfirmLarkDelete check table") {
		t.Fatalf("ConfirmLarkDelete() error = %v, want table check wrapper", err)
	}
	if errors.Is(err, database.ErrDestructiveMigrationGuard) {
		t.Fatalf("ConfirmLarkDelete() error = %v, should not report destructive guard for query failures", err)
	}
}

// Sentinel error text guard.
func TestConfirmLarkDelete_ErrSentinelExported(t *testing.T) {
	if !errors.Is(database.ErrDestructiveMigrationGuard, database.ErrDestructiveMigrationGuard) {
		t.Error("ErrDestructiveMigrationGuard not identity-equal to itself")
	}
	if database.ErrDestructiveMigrationGuard.Error() == "" {
		t.Error("ErrDestructiveMigrationGuard error text is empty")
	}
}

func newUnreachableConfirmPool(t *testing.T) *pgxpool.Pool {
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
