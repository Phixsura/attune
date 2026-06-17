//go:build integration

// Package testdb provides the real-Postgres harness for integration tests.
package testdb

import (
	"context"
	"fmt"
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
	pool, cleanup, err := OpenPool()
	if err != nil {
		t.Fatalf("open postgres pool: %v", err)
	}
	t.Cleanup(cleanup)
	return pool
}

// OpenPool returns a migrated PostgreSQL pool plus a cleanup callback. It is
// intended for package-level integration fixtures that want to reuse one
// database across multiple tests.
func OpenPool() (*pgxpool.Pool, func(), error) {
	if dsn := os.Getenv(serviceContainerDSNEnv); dsn != "" {
		return openServicePool(dsn)
	}
	return openContainerPool()
}

func openContainerPool() (*pgxpool.Pool, func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	var pool *pgxpool.Pool

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
		return nil, nil, fmt.Errorf("start postgres container: %w", err)
	}
	cleanup := func() {
		if pool != nil {
			pool.Close()
		}
		_ = pg.Terminate(context.Background())
	}

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("postgres dsn: %w", err)
	}
	pool, err = pgxpool.New(context.Background(), dsn)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("connect postgres pool: %w", err)
	}

	if err := database.RunMigrations(context.Background(), pool); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("run migrations: %w", err)
	}
	return pool, cleanup, nil
}

func openServicePool(adminDSN string) (*pgxpool.Pool, func(), error) {
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, adminDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("connect service postgres: %w", err)
	}

	dbName := "attune_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{dbName}.Sanitize()); err != nil {
		admin.Close()
		return nil, nil, fmt.Errorf("create test database %s: %w", dbName, err)
	}
	dropDB := func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = admin.Exec(dropCtx, `
			SELECT pg_terminate_backend(pid)
			FROM pg_stat_activity
			WHERE datname = $1 AND pid <> pg_backend_pid()`, dbName)
		_, _ = admin.Exec(dropCtx, `DROP DATABASE IF EXISTS `+pgx.Identifier{dbName}.Sanitize())
		admin.Close()
	}

	cfg, err := pgxpool.ParseConfig(adminDSN)
	if err != nil {
		dropDB()
		return nil, nil, fmt.Errorf("parse service postgres dsn: %w", err)
	}
	cfg.ConnConfig.Config.Database = dbName
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		dropDB()
		return nil, nil, fmt.Errorf("connect test database %s: %w", dbName, err)
	}
	if err := database.RunMigrations(ctx, pool); err != nil {
		pool.Close()
		dropDB()
		return nil, nil, fmt.Errorf("run migrations in %s: %w", dbName, err)
	}
	return pool, func() {
		pool.Close()
		dropDB()
	}, nil
}
