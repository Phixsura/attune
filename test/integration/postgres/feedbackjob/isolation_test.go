//go:build integration

package feedbackjob_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/repo/feedbackjob"
	"github.com/Phixsura/attune/internal/testdb"
)

func isoTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO tenants (slug, name) VALUES ($1, $1)
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`, slug).Scan(&id) // ptrext:allow scan-target
	if err != nil {
		t.Fatalf("insert tenant %s: %v", slug, err)
	}
	return id
}

func TestPG_JobGet_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	jobs := feedbackjob.New(pool)

	tidA := isoTenant(t, ctx, pool, "job-iso-a")
	tidB := isoTenant(t, ctx, pool, "job-iso-b")

	jobB, err := jobs.Create(ctx, tidB, "user-b", []byte(`{"op":"tag"}`), 10)
	if err != nil {
		t.Fatalf("Create job B: %v", err)
	}

	// Tenant B retrieves its own job.
	got, err := jobs.Get(ctx, tidB, jobB.ID)
	if err != nil {
		t.Fatalf("Get(tenantB, jobB): %v", err)
	}
	if got.TenantID != tidB {
		t.Fatalf("unexpected tenant: got %q, want %q", got.TenantID, tidB)
	}

	// Tenant A must not see tenant B's job.
	_, err = jobs.Get(ctx, tidA, jobB.ID)
	if err == nil {
		t.Error("ISOLATION BREACH: Get(tenantA, jobB) succeeded")
	} else if !errors.Is(err, feedbackjob.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPG_JobList_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	jobs := feedbackjob.New(pool)

	tidA := isoTenant(t, ctx, pool, "job-list-a")
	tidB := isoTenant(t, ctx, pool, "job-list-b")

	_, err := jobs.Create(ctx, tidA, "user-a", []byte(`{"op":"tag"}`), 5)
	if err != nil {
		t.Fatalf("Create job A: %v", err)
	}
	_, err = jobs.Create(ctx, tidA, "user-a", []byte(`{"op":"delete"}`), 3)
	if err != nil {
		t.Fatalf("Create job A2: %v", err)
	}
	_, err = jobs.Create(ctx, tidB, "user-b", []byte(`{"op":"tag"}`), 7)
	if err != nil {
		t.Fatalf("Create job B: %v", err)
	}

	listA, _, err := jobs.List(ctx, tidA, nil, 100, "")
	if err != nil {
		t.Fatalf("List(tenantA): %v", err)
	}
	if len(listA) != 2 {
		t.Fatalf("expected 2 jobs for tenant A, got %d", len(listA))
	}
	for _, j := range listA {
		if j.TenantID != tidA {
			t.Errorf("ISOLATION BREACH: List(tenantA) returned job %s belonging to tenant %q", j.ID, j.TenantID)
		}
	}

	listB, _, err := jobs.List(ctx, tidB, nil, 100, "")
	if err != nil {
		t.Fatalf("List(tenantB): %v", err)
	}
	if len(listB) != 1 {
		t.Fatalf("expected 1 job for tenant B, got %d", len(listB))
	}
	if listB[0].TenantID != tidB {
		t.Errorf("ISOLATION BREACH: List(tenantB) returned job belonging to tenant %q", listB[0].TenantID)
	}
}

func TestPG_JobCancel_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	jobs := feedbackjob.New(pool)

	tidA := isoTenant(t, ctx, pool, "job-cancel-a")
	tidB := isoTenant(t, ctx, pool, "job-cancel-b")

	jobB, err := jobs.Create(ctx, tidB, "user-b", []byte(`{"op":"tag"}`), 10)
	if err != nil {
		t.Fatalf("Create job B: %v", err)
	}

	// Tenant A must not be able to cancel tenant B's job.
	err = jobs.Cancel(ctx, tidA, jobB.ID)
	if err == nil {
		t.Error("ISOLATION BREACH: Cancel(tenantA, jobB) succeeded")
	}

	// Job must still be queued for tenant B.
	got, err := jobs.Get(ctx, tidB, jobB.ID)
	if err != nil {
		t.Fatalf("Get(tenantB) after cross-tenant cancel: %v", err)
	}
	if got.Status != feedbackjob.StatusQueued {
		t.Errorf("ISOLATION BREACH: job status changed to %q after cross-tenant cancel", got.Status)
	}
}
