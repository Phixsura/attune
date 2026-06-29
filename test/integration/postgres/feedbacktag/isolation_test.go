//go:build integration

package feedbacktag_test

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_ListTags_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "tag-iso-a", "Tag Iso A")
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "tag-iso-b", "Tag Iso B")
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	repo := feedbacktag.New(pool)

	_, err = repo.Create(ctx, feedbacktag.Tag{
		TenantID:  tidA,
		Name:      "urgent",
		Color:     "#ef4444",
		CreatedBy: "admin-a",
	})
	if err != nil {
		t.Fatalf("create tag for A: %v", err)
	}

	_, err = repo.Create(ctx, feedbacktag.Tag{
		TenantID:  tidB,
		Name:      "feature",
		Color:     "#3b82f6",
		CreatedBy: "admin-b",
	})
	if err != nil {
		t.Fatalf("create tag for B: %v", err)
	}

	tagsA, err := repo.List(ctx, tidA, false)
	if err != nil {
		t.Fatalf("List(tenantA): %v", err)
	}
	for _, tag := range tagsA {
		if tag.TenantID != tidA {
			t.Errorf("ISOLATION BREACH: List(tenantA) returned tag %q belonging to tenant %q", tag.Name, tag.TenantID)
		}
	}
	if len(tagsA) != 1 || tagsA[0].Name != "urgent" {
		t.Errorf("expected exactly tenant A's tag 'urgent', got %d tags", len(tagsA))
	}

	tagsB, err := repo.List(ctx, tidB, false)
	if err != nil {
		t.Fatalf("List(tenantB): %v", err)
	}
	for _, tag := range tagsB {
		if tag.TenantID != tidB {
			t.Errorf("ISOLATION BREACH: List(tenantB) returned tag %q belonging to tenant %q", tag.Name, tag.TenantID)
		}
	}
	if len(tagsB) != 1 || tagsB[0].Name != "feature" {
		t.Errorf("expected exactly tenant B's tag 'feature', got %d tags", len(tagsB))
	}
}
