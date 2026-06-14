//go:build integration

package workflowstate_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	"github.com/Phixsura/attune/internal/testdb"
)

func setup(t *testing.T) (context.Context, *workflowstate.Repo, string) {
	t.Helper()
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "wf-io", "Workflow IO")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return ctx, workflowstate.New(pool), tenantID
}

func TestPG_StateCRUD(t *testing.T) {
	ctx, repo, tenantID := setup(t)

	created, err := repo.Create(ctx, workflowstate.WorkflowState{
		TenantID:  tenantID,
		Name:      "New",
		Color:     "#3b82f6",
		Category:  "open",
		Position:  0,
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "New" || created.Category != "open" || !created.IsDefault {
		t.Fatalf("unexpected state: %+v", created)
	}
	if created.ArchivedAt != nil {
		t.Fatal("new state should not be archived")
	}

	t.Run("GetByTenantAndID", func(t *testing.T) {
		got, err := repo.GetByTenantAndID(ctx, tenantID, created.ID)
		if err != nil {
			t.Fatalf("GetByTenantAndID: %v", err)
		}
		if got.Name != "New" {
			t.Fatalf("name = %q want %q", got.Name, "New")
		}
	})

	t.Run("Update", func(t *testing.T) {
		updated, err := repo.Update(ctx, workflowstate.WorkflowState{
			ID:    created.ID,
			Name:  "Open",
			Color: "#22c55e",
		})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if updated.Name != "Open" || updated.Color != "#22c55e" {
			t.Fatalf("unexpected update: %+v", updated)
		}
	})

	t.Run("List", func(t *testing.T) {
		states, err := repo.List(ctx, tenantID, false)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(states) != 1 {
			t.Fatalf("List len = %d want 1", len(states))
		}
	})

	t.Run("NameConflict", func(t *testing.T) {
		_, err := repo.Create(ctx, workflowstate.WorkflowState{
			TenantID: tenantID,
			Name:     "Open",
			Color:    "#000",
			Category: "open",
		})
		if !errors.Is(err, workflowstate.ErrNameConflict) {
			t.Fatalf("expected ErrNameConflict, got %v", err)
		}
	})

	t.Run("Archive_and_ListIncluded", func(t *testing.T) {
		if err := repo.Archive(ctx, tenantID, created.ID); err != nil {
			t.Fatalf("Archive: %v", err)
		}
		active, err := repo.List(ctx, tenantID, false)
		if err != nil {
			t.Fatalf("List active: %v", err)
		}
		if len(active) != 0 {
			t.Fatalf("active count = %d want 0", len(active))
		}
		all, err := repo.List(ctx, tenantID, true)
		if err != nil {
			t.Fatalf("List all: %v", err)
		}
		if len(all) != 1 {
			t.Fatalf("all count = %d want 1", len(all))
		}
		if all[0].ArchivedAt == nil {
			t.Fatal("archived state should have archived_at")
		}
	})
}

func TestPG_Transitions(t *testing.T) {
	ctx, repo, tenantID := setup(t)

	s1, err := repo.Create(ctx, workflowstate.WorkflowState{
		TenantID: tenantID, Name: "New", Color: "#3b82f6", Category: "open", Position: 0, IsDefault: true,
	})
	if err != nil {
		t.Fatalf("Create s1: %v", err)
	}
	s2, err := repo.Create(ctx, workflowstate.WorkflowState{
		TenantID: tenantID, Name: "InProgress", Color: "#f59e0b", Category: "active", Position: 1,
	})
	if err != nil {
		t.Fatalf("Create s2: %v", err)
	}
	s3, err := repo.Create(ctx, workflowstate.WorkflowState{
		TenantID: tenantID, Name: "Done", Color: "#22c55e", Category: "closed", Position: 2,
	})
	if err != nil {
		t.Fatalf("Create s3: %v", err)
	}

	edges := []workflowstate.TransitionEdge{
		{FromStateID: s1.ID, ToStateID: s2.ID},
		{FromStateID: s2.ID, ToStateID: s3.ID},
		{FromStateID: s2.ID, ToStateID: s1.ID},
	}
	transitions, err := repo.ReplaceTransitions(ctx, tenantID, edges)
	if err != nil {
		t.Fatalf("ReplaceTransitions: %v", err)
	}
	if len(transitions) != 3 {
		t.Fatalf("transition count = %d want 3", len(transitions))
	}

	t.Run("ListTransitions", func(t *testing.T) {
		ts, err := repo.ListTransitions(ctx, tenantID)
		if err != nil {
			t.Fatalf("ListTransitions: %v", err)
		}
		if len(ts) != 3 {
			t.Fatalf("ListTransitions len = %d want 3", len(ts))
		}
	})

	t.Run("CheckTransition_valid", func(t *testing.T) {
		ok, err := repo.CheckTransition(ctx, tenantID, s1.ID, s2.ID)
		if err != nil {
			t.Fatalf("CheckTransition: %v", err)
		}
		if !ok {
			t.Fatal("expected s1→s2 to be valid")
		}
	})

	t.Run("CheckTransition_invalid", func(t *testing.T) {
		ok, err := repo.CheckTransition(ctx, tenantID, s1.ID, s3.ID)
		if err != nil {
			t.Fatalf("CheckTransition: %v", err)
		}
		if ok {
			t.Fatal("expected s1→s3 to be invalid")
		}
	})

	t.Run("AllowedNext", func(t *testing.T) {
		next, err := repo.AllowedNext(ctx, tenantID, s2.ID)
		if err != nil {
			t.Fatalf("AllowedNext: %v", err)
		}
		if len(next) != 2 {
			t.Fatalf("AllowedNext len = %d want 2", len(next))
		}
		names := map[string]bool{}
		for _, n := range next {
			names[n.Name] = true
		}
		if !names["Done"] || !names["New"] {
			t.Fatalf("unexpected allowed next: %v", names)
		}
	})

	t.Run("ReplaceTransitions_overwrites", func(t *testing.T) {
		newEdges := []workflowstate.TransitionEdge{
			{FromStateID: s1.ID, ToStateID: s3.ID},
		}
		ts, err := repo.ReplaceTransitions(ctx, tenantID, newEdges)
		if err != nil {
			t.Fatalf("ReplaceTransitions: %v", err)
		}
		if len(ts) != 1 {
			t.Fatalf("after replace len = %d want 1", len(ts))
		}
		all, err := repo.ListTransitions(ctx, tenantID)
		if err != nil {
			t.Fatalf("ListTransitions: %v", err)
		}
		if len(all) != 1 {
			t.Fatalf("total after replace = %d want 1", len(all))
		}
	})
}
