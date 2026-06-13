//go:build integration

// Package testdb provides the real-Postgres harness for integration tests.
package testdb

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Phixsura/attune/internal/infra/database"
)

const serviceContainerDSNEnv = "ATTUNE_TEST_DATABASE_URL"

// NewPool returns a migrated, isolated PostgreSQL pool for one integration
// test. In CI, ATTUNE_TEST_DATABASE_URL points at the Postgres service
// container and NewPool creates a temporary database per test. Locally, when
// that env var is unset, it starts a fresh testcontainers Postgres.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if dsn := os.Getenv(serviceContainerDSNEnv); dsn != "" {
		return newServicePool(t, dsn)
	}
	return newContainerPool(t)
}

func newContainerPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	pg, err := tcpg.Run(
		ctx,
		"pgvector/pgvector:pg17",
		tcpg.WithDatabase("attune"),
		tcpg.WithUsername("attune"),
		tcpg.WithPassword("attune"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		_ = pg.Terminate(context.Background())
	})

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres dsn: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect postgres pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := database.RunMigrations(context.Background(), pool); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return pool
}

func newServicePool(t *testing.T, adminDSN string) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connect service postgres: %v", err)
	}
	t.Cleanup(admin.Close)

	dbName := "attune_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{dbName}.Sanitize()); err != nil {
		t.Fatalf("create test database %s: %v", dbName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = admin.Exec(dropCtx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()`, dbName)
		_, _ = admin.Exec(dropCtx, `DROP DATABASE IF EXISTS `+pgx.Identifier{dbName}.Sanitize())
	})

	cfg, err := pgxpool.ParseConfig(adminDSN)
	if err != nil {
		t.Fatalf("parse service postgres dsn: %v", err)
	}
	cfg.ConnConfig.Config.Database = dbName
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect test database %s: %v", dbName, err)
	}
	t.Cleanup(pool.Close)
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatalf("run migrations in %s: %v", dbName, err)
	}
	return pool
}
