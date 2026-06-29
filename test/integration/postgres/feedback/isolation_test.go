//go:build integration

package feedback_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func createIsoTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	id, err := tenant.NewTenant(pool).Create(ctx, slug, slug)
	if err != nil {
		t.Fatalf("create tenant %s: %v", slug, err)
	}
	return id
}

func seedRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, content string) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u1', 'api', $2)
		RETURNING id`, tenantID, content).Scan(&id)
	if err != nil {
		t.Fatalf("seed feedback: %v", err)
	}
	return id
}

func TestPG_GetForConsole_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tidA := createIsoTenant(t, ctx, pool, "fb-iso-a")
	tidB := createIsoTenant(t, ctx, pool, "fb-iso-b")

	idB := seedRow(t, ctx, pool, tidB, "B's private feedback")

	repo := feedback.NewFeedback(pool)

	// Tenant B should find its own row.
	got, err := repo.GetForConsole(ctx, tidB, idB)
	if err != nil {
		t.Fatalf("GetForConsole(tenantB, idB): %v", err)
	}
	if got.Content != "B's private feedback" {
		t.Fatalf("unexpected content: %q", got.Content)
	}

	// Tenant A must not see tenant B's row.
	_, err = repo.GetForConsole(ctx, tidA, idB)
	if err == nil {
		t.Error("ISOLATION BREACH: GetForConsole(tenantA, idB) succeeded")
	}
}

func TestPG_ListForConsole_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tidA := createIsoTenant(t, ctx, pool, "fb-list-a")
	tidB := createIsoTenant(t, ctx, pool, "fb-list-b")

	seedRow(t, ctx, pool, tidA, "A's feedback 1")
	seedRow(t, ctx, pool, tidA, "A's feedback 2")
	seedRow(t, ctx, pool, tidB, "B's feedback")

	repo := feedback.NewFeedback(pool)

	rowsA, err := repo.ListForConsole(ctx, tidA, feedback.ConsoleListOpts{Limit: 100})
	if err != nil {
		t.Fatalf("ListForConsole(tenantA): %v", err)
	}
	if len(rowsA) != 2 {
		t.Fatalf("expected 2 rows for tenant A, got %d", len(rowsA))
	}

	rowsB, err := repo.ListForConsole(ctx, tidB, feedback.ConsoleListOpts{Limit: 100})
	if err != nil {
		t.Fatalf("ListForConsole(tenantB): %v", err)
	}
	if len(rowsB) != 1 {
		t.Fatalf("expected 1 row for tenant B, got %d", len(rowsB))
	}
	if rowsB[0].Content != "B's feedback" {
		t.Errorf("ISOLATION BREACH: tenant B got unexpected content %q", rowsB[0].Content)
	}
}
