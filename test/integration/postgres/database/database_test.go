//go:build integration

package database_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/infra/database"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_RunMigrations_Idempotent(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	before := migrationCount(t, ctx, pool)
	if err := database.RunMigrations(ctx, pool); err != nil {
		t.Fatalf("second RunMigrations: %v", err)
	}
	after := migrationCount(t, ctx, pool)
	if after != before {
		t.Fatalf("migration tracker count changed: before=%d after=%d", before, after)
	}
}

func TestPG_RunMigrations_Concurrent(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	before := migrationCount(t, ctx, pool)

	var wg sync.WaitGroup
	errCh := make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- database.RunMigrations(ctx, pool)
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("RunMigrations concurrent error: %v", err)
		}
	}
	after := migrationCount(t, ctx, pool)
	if after != before {
		t.Fatalf("migration tracker count changed: before=%d after=%d", before, after)
	}
}

func TestPG_ConfirmLarkDelete_GuardsLegacyRows(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID := insertTenant(t, ctx, pool)
	insertFeedback(t, ctx, pool, tenantID, "lark-webhook")

	err := database.ConfirmLarkDelete(ctx, pool, false)
	if !errors.Is(err, database.ErrDestructiveMigrationGuard) {
		t.Fatalf("ConfirmLarkDelete error: got %v want %v", err, database.ErrDestructiveMigrationGuard)
	}

	if err := database.ConfirmLarkDelete(ctx, pool, true); err != nil {
		t.Fatalf("ConfirmLarkDelete with opt-in: %v", err)
	}
}

func migrationCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations_feedback`).Scan(&n); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	return n
}

func insertTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var tenantID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name)
		VALUES ('database-io', 'Database IO')
		RETURNING id`,
	).Scan(&tenantID); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	return tenantID
}

func insertFeedback(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, source string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u1', $2, 'legacy lark row')`,
		tenantID, source,
	); err != nil {
		t.Fatalf("insert feedback: %v", err)
	}
}
