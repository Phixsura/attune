//go:build integration

package feedbacktag_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func setup(t *testing.T) (context.Context, *feedbacktag.Repo, string) {
	t.Helper()
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "tag-io", "Tag IO")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return ctx, feedbacktag.New(pool), tenantID
}

func TestPG_TagCRUD(t *testing.T) {
	ctx, repo, tenantID := setup(t)

	created, err := repo.Create(ctx, feedbacktag.Tag{
		TenantID:  tenantID,
		Name:      "bug",
		Color:     "#ef4444",
		CreatedBy: "user-1",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "bug" || created.Color != "#ef4444" || created.TenantID != tenantID {
		t.Fatalf("unexpected tag: %+v", created)
	}
	if created.UsageCount != 0 {
		t.Fatalf("new tag usage_count = %d want 0", created.UsageCount)
	}

	t.Run("GetByID", func(t *testing.T) {
		got, err := repo.GetByID(ctx, tenantID, created.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.Name != "bug" {
			t.Fatalf("GetByID name = %q want %q", got.Name, "bug")
		}
	})

	t.Run("GetByName", func(t *testing.T) {
		gotName, err := repo.GetByName(ctx, tenantID, "bug")
		if err != nil {
			t.Fatalf("GetByName: %v", err)
		}
		if gotName.ID != created.ID {
			t.Fatalf("GetByName id mismatch: got %s want %s", gotName.ID, created.ID)
		}
	})

	t.Run("Update", func(t *testing.T) {
		updated, err := repo.Update(ctx, feedbacktag.Tag{
			ID:          created.ID,
			TenantID:    tenantID,
			Name:        "bug-report",
			Color:       "#dc2626",
			Description: "confirmed bugs",
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if updated.Name != "bug-report" || updated.Description != "confirmed bugs" {
			t.Fatalf("unexpected update result: %+v", updated)
		}
	})

	t.Run("Archive_and_List", func(t *testing.T) {
		if err := repo.Archive(ctx, tenantID, created.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}
		archived, err := repo.GetByID(ctx, tenantID, created.ID)
		if err != nil {
			t.Fatalf("GetByID after archive: %v", err)
		}
		if archived.ArchivedAt == nil {
			t.Fatal("expected archived_at to be set")
		}
		active, err := repo.List(ctx, tenantID, false)
		if err != nil {
			t.Fatalf("List active: %v", err)
		}
		if len(active) != 0 {
			t.Fatalf("List(includeArchived=false) = %d want 0", len(active))
		}
		all, err := repo.List(ctx, tenantID, true)
		if err != nil {
			t.Fatalf("List all: %v", err)
		}
		if len(all) != 1 {
			t.Fatalf("List(includeArchived=true) = %d want 1", len(all))
		}
	})
}

func TestPG_TagNameConflict(t *testing.T) {
	ctx, repo, tenantID := setup(t)

	if _, err := repo.Create(ctx, feedbacktag.Tag{
		TenantID: tenantID, Name: "dup", Color: "#000000",
	}); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err := repo.Create(ctx, feedbacktag.Tag{
		TenantID: tenantID, Name: "dup", Color: "#111111",
	})
	if !errors.Is(err, feedbacktag.ErrNameConflict) {
		t.Fatalf("duplicate name: got %v want %v", err, feedbacktag.ErrNameConflict)
	}
}

func TestPG_TagExclusiveScope(t *testing.T) {
	ctx, repo, tenantID := setup(t)

	tag, err := repo.Create(ctx, feedbacktag.Tag{
		TenantID:       tenantID,
		Name:           "priority-high",
		Color:          "#f59e0b",
		ExclusiveScope: ptrext.Of("priority"),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tag.ExclusiveScope == nil || *tag.ExclusiveScope != "priority" {
		t.Fatalf("scope = %v want 'priority'", tag.ExclusiveScope)
	}
}

func TestPG_TagUsageCount(t *testing.T) {
	ctx, repo, tenantID := setup(t)

	tag, err := repo.Create(ctx, feedbacktag.Tag{
		TenantID: tenantID, Name: "counted", Color: "#000000",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.IncrementUsage(ctx, tenantID, tag.ID); err != nil {
		t.Fatalf("IncrementUsage: %v", err)
	}
	if err := repo.IncrementUsage(ctx, tenantID, tag.ID); err != nil {
		t.Fatalf("IncrementUsage 2: %v", err)
	}
	got, err := repo.GetByID(ctx, tenantID, tag.ID)
	if err != nil {
		t.Fatalf("GetByID after increment: %v", err)
	}
	if got.UsageCount != 2 {
		t.Fatalf("usage_count = %d want 2", got.UsageCount)
	}

	if err := repo.DecrementUsage(ctx, tenantID, tag.ID); err != nil {
		t.Fatalf("DecrementUsage: %v", err)
	}
	got, err = repo.GetByID(ctx, tenantID, tag.ID)
	if err != nil {
		t.Fatalf("GetByID after decrement: %v", err)
	}
	if got.UsageCount != 1 {
		t.Fatalf("usage_count = %d want 1", got.UsageCount)
	}
}

func TestPG_TagNotFound(t *testing.T) {
	ctx, repo, tenantID := setup(t)

	_, err := repo.GetByName(ctx, tenantID, "nonexistent")
	if !errors.Is(err, feedbacktag.ErrNotFound) {
		t.Fatalf("GetByName missing: got %v want %v", err, feedbacktag.ErrNotFound)
	}

	if err := repo.Archive(ctx, tenantID, uuid.Nil); !errors.Is(err, feedbacktag.ErrNotFound) {
		t.Fatalf("Archive missing: got %v want %v", err, feedbacktag.ErrNotFound)
	}
}
