//go:build integration

package workflowstate_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_ListStates_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "wf-iso-a", "WF Iso A")
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "wf-iso-b", "WF Iso B")
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	repo := workflowstate.New(pool)

	_, err = repo.Create(ctx, workflowstate.WorkflowState{
		TenantID:    tidA,
		Name:        "open",
		DisplayName: domain.I18nString{"default": "Open"},
		Color:       "#22c55e",
		Category:    "open",
		Position:    0,
		IsDefault:   true,
	})
	if err != nil {
		t.Fatalf("create state A: %v", err)
	}

	_, err = repo.Create(ctx, workflowstate.WorkflowState{
		TenantID:    tidB,
		Name:        "closed",
		DisplayName: domain.I18nString{"default": "Closed"},
		Color:       "#ef4444",
		Category:    "closed",
		Position:    0,
		IsDefault:   false,
	})
	if err != nil {
		t.Fatalf("create state B: %v", err)
	}

	states, err := repo.List(ctx, tidA, false)
	if err != nil {
		t.Fatalf("List tenant A: %v", err)
	}
	for _, s := range states {
		if s.TenantID != tidA {
			t.Errorf("ISOLATION BREACH: List(tenantA) returned state %q belonging to tenant %q", s.Name, s.TenantID)
		}
	}

	statesB, err := repo.List(ctx, tidB, false)
	if err != nil {
		t.Fatalf("List tenant B: %v", err)
	}
	for _, s := range statesB {
		if s.TenantID != tidB {
			t.Errorf("ISOLATION BREACH: List(tenantB) returned state %q belonging to tenant %q", s.Name, s.TenantID)
		}
	}

	if len(states) != 1 || states[0].Name != "open" {
		t.Errorf("expected exactly tenant A's state 'open', got %d states", len(states))
	}
	if len(statesB) != 1 || statesB[0].Name != "closed" {
		t.Errorf("expected exactly tenant B's state 'closed', got %d states", len(statesB))
	}
}

func TestPG_ArchiveState_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "wf-arch-a", "WF Archive A")
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "wf-arch-b", "WF Archive B")
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	repo := workflowstate.New(pool)

	created, err := repo.Create(ctx, workflowstate.WorkflowState{
		TenantID:    tidB,
		Name:        "in-progress",
		DisplayName: domain.I18nString{"default": "In Progress"},
		Color:       "#f59e0b",
		Category:    "open",
		Position:    0,
		IsDefault:   false,
	})
	if err != nil {
		t.Fatalf("create state for B: %v", err)
	}

	// Attempt to archive B's state using A's tenant ID.
	err = repo.Archive(ctx, tidA, created.ID)
	if err == nil {
		t.Error("ISOLATION BREACH: Archive succeeded with wrong tenant ID")
	} else if !errors.Is(err, workflowstate.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}

	// Verify the state is still active for B.
	got, err := repo.GetByTenantAndID(ctx, tidB, created.ID)
	if err != nil {
		t.Fatalf("GetByTenantAndID after cross-tenant archive attempt: %v", err)
	}
	if got.ArchivedAt != nil {
		t.Error("ISOLATION BREACH: state was archived despite wrong tenant ID")
	}
}
