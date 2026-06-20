//go:build integration

package feedback_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/testdb"
)

// seedTenant inserts a tenant by slug (idempotent) and returns its id.
func seedTenant(t *testing.T, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO tenants (slug, name) VALUES ($1, $1)
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`, slug).Scan(&id)
	if err != nil {
		t.Fatalf("insert tenant %s: %v", slug, err)
	}
	return id
}

// seedEnrichedRow inserts a row for tenantID and drives it to
// enrichment_status='done' (which stamps enriched_at=NOW()).
func seedEnrichedRow(t *testing.T, pool *pgxpool.Pool, repo *feedback.FeedbackRepo, tenantID, content string) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	err := pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u1', 'api', $2)
		RETURNING id`, tenantID, content).Scan(&id)
	if err != nil {
		t.Fatalf("insert row: %v", err)
	}
	if _, err := repo.TryClaim(ctx, id); err != nil {
		t.Fatalf("TryClaim: %v", err)
	}
	if err := repo.MarkDone(ctx, id, domain.Enriched{
		Title: "t",
		Attrs: map[string]any{"labels": []string{"payment"}},
	}); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}
	return id
}

// TestPG_SampleEnrichedByTenant_Isolation is the security-critical test for
// the #83 Console eval-suggestions endpoint: the tenant-scoped sampler must
// NEVER return another tenant's rows.
func TestPG_SampleEnrichedByTenant_Isolation(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	tenantA := seedTenant(t, pool, "iso-tenant-a")
	tenantB := seedTenant(t, pool, "iso-tenant-b")

	seedEnrichedRow(t, pool, repo, tenantA, "a-row-1")
	seedEnrichedRow(t, pool, repo, tenantA, "a-row-2")
	seedEnrichedRow(t, pool, repo, tenantB, "b-row-1")

	since := time.Now().Add(-time.Hour)

	rowsA, err := repo.SampleEnrichedByTenant(ctx, tenantA, since, 100)
	if err != nil {
		t.Fatalf("SampleEnrichedByTenant(A): %v", err)
	}
	if len(rowsA) != 2 {
		t.Fatalf("tenant A: expected 2 enriched rows, got %d", len(rowsA))
	}
	for _, r := range rowsA {
		if r.TenantID != tenantA {
			t.Errorf("ISOLATION BREACH: tenant A sample returned row from tenant %s", r.TenantID)
		}
	}

	rowsB, err := repo.SampleEnrichedByTenant(ctx, tenantB, since, 100)
	if err != nil {
		t.Fatalf("SampleEnrichedByTenant(B): %v", err)
	}
	if len(rowsB) != 1 {
		t.Fatalf("tenant B: expected 1 enriched row, got %d", len(rowsB))
	}
	if rowsB[0].TenantID != tenantB {
		t.Errorf("ISOLATION BREACH: tenant B sample returned row from tenant %s", rowsB[0].TenantID)
	}
}

// TestPG_SampleEnrichedByTenant_FiltersUnenriched verifies the sampler only
// returns rows whose enrichment completed (status='done').
func TestPG_SampleEnrichedByTenant_FiltersUnenriched(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "filter-tenant")

	// One enriched row.
	seedEnrichedRow(t, pool, repo, tenantID, "enriched")
	// One pending (never claimed/marked-done) row.
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_feedback (tenant_id, user_id, source, content)
		VALUES ($1, 'u1', 'api', 'pending')`, tenantID); err != nil {
		t.Fatalf("insert pending: %v", err)
	}

	rows, err := repo.SampleEnrichedByTenant(ctx, tenantID, time.Now().Add(-time.Hour), 100)
	if err != nil {
		t.Fatalf("SampleEnrichedByTenant: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected only the enriched row, got %d", len(rows))
	}
	if rows[0].Content != "enriched" {
		t.Errorf("expected enriched row, got %q", rows[0].Content)
	}
}

// TestPG_SampleEnrichedByTenant_SinceFilter verifies the `since` lower bound
// excludes rows enriched before the cutoff.
func TestPG_SampleEnrichedByTenant_SinceFilter(t *testing.T) {
	pool := testdb.NewPool(t)
	repo := feedback.NewFeedback(pool)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "since-tenant")
	id := seedEnrichedRow(t, pool, repo, tenantID, "old-row")

	// Backdate enriched_at well into the past.
	if _, err := pool.Exec(ctx,
		`UPDATE user_feedback SET enriched_at = NOW() - INTERVAL '10 days' WHERE id = $1`, id); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	// since = 1 day ago → the 10-day-old row must be excluded.
	rows, err := repo.SampleEnrichedByTenant(ctx, tenantID, time.Now().Add(-24*time.Hour), 100)
	if err != nil {
		t.Fatalf("SampleEnrichedByTenant: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows after since cutoff, got %d", len(rows))
	}
}
