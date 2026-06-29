//go:build integration

package outbox_test

import (
	"errors"
	"testing"

	outboxrepo "github.com/Phixsura/attune/internal/repo/outbox"
)

func TestPG_GetByID_TenantIsolation(t *testing.T) {
	e := setup(t)
	other := e.newTenant(t, "outbox-iso-get")

	// Insert a dead row for the primary tenant.
	id := e.insert(t, e.tenant, seed{status: "dead", attempts: 5, kind: "http_5xx", httpStatus: 503})

	// Primary tenant retrieves its own row.
	row, err := e.repo.GetByID(e.ctx, e.tenant, id)
	if err != nil {
		t.Fatalf("GetByID(own tenant): %v", err)
	}
	if row.ID != id {
		t.Fatalf("unexpected row id: got %d, want %d", row.ID, id)
	}

	// Other tenant must NOT see the row.
	_, err = e.repo.GetByID(e.ctx, other, id)
	if err == nil {
		t.Error("ISOLATION BREACH: GetByID succeeded with wrong tenant ID")
	} else if !errors.Is(err, outboxrepo.ErrOutboxNotFound) {
		t.Errorf("expected ErrOutboxNotFound, got: %v", err)
	}
}

func TestPG_ListByStatus_TenantIsolation(t *testing.T) {
	e := setup(t)
	other := e.newTenant(t, "outbox-iso-list")

	e.insert(t, e.tenant, seed{status: "dead", attempts: 5})
	e.insert(t, e.tenant, seed{status: "failed", attempts: 2})
	e.insert(t, other, seed{status: "dead", attempts: 5})
	e.insert(t, other, seed{status: "failed", attempts: 1})

	rowsA, err := e.repo.ListByStatus(e.ctx, e.tenant, []string{"dead", "failed"}, 50, 0)
	if err != nil {
		t.Fatalf("ListByStatus(primary): %v", err)
	}
	for _, r := range rowsA {
		if r.TenantID != e.tenant {
			t.Errorf("ISOLATION BREACH: ListByStatus(primary) returned row %d belonging to tenant %q", r.ID, r.TenantID)
		}
	}
	if len(rowsA) != 2 {
		t.Errorf("expected 2 rows for primary tenant, got %d", len(rowsA))
	}

	rowsB, err := e.repo.ListByStatus(e.ctx, other, []string{"dead", "failed"}, 50, 0)
	if err != nil {
		t.Fatalf("ListByStatus(other): %v", err)
	}
	for _, r := range rowsB {
		if r.TenantID != other {
			t.Errorf("ISOLATION BREACH: ListByStatus(other) returned row %d belonging to tenant %q", r.ID, r.TenantID)
		}
	}
	if len(rowsB) != 2 {
		t.Errorf("expected 2 rows for other tenant, got %d", len(rowsB))
	}
}
