//go:build integration

package mcp_test

import (
	"context"
	"testing"

	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_MCPClients_ListByTenant_Isolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "mcp-iso-a", "MCP Iso A")
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "mcp-iso-b", "MCP Iso B")
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	clients := mcprepo.NewClients(pool)

	_, err = clients.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tidA,
		Name:         "agent-a",
		RedirectURIs: []string{"http://localhost/cb"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "admin-a",
	})
	if err != nil {
		t.Fatalf("create client A: %v", err)
	}

	_, err = clients.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tidB,
		Name:         "agent-b",
		RedirectURIs: []string{"http://localhost/cb"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "admin-b",
	})
	if err != nil {
		t.Fatalf("create client B: %v", err)
	}

	listA, err := clients.ListByTenant(ctx, tidA)
	if err != nil {
		t.Fatalf("ListByTenant(A): %v", err)
	}
	if len(listA) != 1 {
		t.Fatalf("expected 1 client for A, got %d", len(listA))
	}
	if listA[0].TenantID != tidA {
		t.Errorf("ISOLATION BREACH: ListByTenant(A) returned client belonging to tenant %q", listA[0].TenantID)
	}
	if listA[0].Name != "agent-a" {
		t.Errorf("expected 'agent-a', got %q", listA[0].Name)
	}

	listB, err := clients.ListByTenant(ctx, tidB)
	if err != nil {
		t.Fatalf("ListByTenant(B): %v", err)
	}
	if len(listB) != 1 {
		t.Fatalf("expected 1 client for B, got %d", len(listB))
	}
	if listB[0].TenantID != tidB {
		t.Errorf("ISOLATION BREACH: ListByTenant(B) returned client belonging to tenant %q", listB[0].TenantID)
	}
	if listB[0].Name != "agent-b" {
		t.Errorf("expected 'agent-b', got %q", listB[0].Name)
	}
}

func TestPG_MCPSessions_ListByClient_TenantIsolation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()
	tenants := tenant.NewTenant(pool)

	tidA, err := tenants.Create(ctx, "mcp-sess-a", "MCP Sess A")
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tidB, err := tenants.Create(ctx, "mcp-sess-b", "MCP Sess B")
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	clients := mcprepo.NewClients(pool)
	sessions := mcprepo.NewSessions(pool)

	clientA, err := clients.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tidA,
		Name:         "sess-agent-a",
		RedirectURIs: []string{"http://localhost/cb"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "admin-a",
	})
	if err != nil {
		t.Fatalf("create client A: %v", err)
	}
	clientB, err := clients.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tidB,
		Name:         "sess-agent-b",
		RedirectURIs: []string{"http://localhost/cb"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "admin-b",
	})
	if err != nil {
		t.Fatalf("create client B: %v", err)
	}

	_, err = sessions.Create(ctx, mcprepo.CreateSessionParams{
		ClientID: clientA.ID,
		TenantID: tidA,
		Scopes:   []string{"mcp:read"},
	})
	if err != nil {
		t.Fatalf("create session A: %v", err)
	}
	_, err = sessions.Create(ctx, mcprepo.CreateSessionParams{
		ClientID: clientB.ID,
		TenantID: tidB,
		Scopes:   []string{"mcp:read"},
	})
	if err != nil {
		t.Fatalf("create session B: %v", err)
	}

	// ListByClient returns sessions for one client; since clients are
	// tenant-scoped, this inherits tenant isolation.
	sessA, err := sessions.ListByClient(ctx, clientA.ID)
	if err != nil {
		t.Fatalf("ListByClient(A): %v", err)
	}
	if len(sessA) != 1 {
		t.Fatalf("expected 1 session for client A, got %d", len(sessA))
	}
	if sessA[0].TenantID != tidA {
		t.Errorf("ISOLATION BREACH: session for client A belongs to tenant %q", sessA[0].TenantID)
	}

	sessB, err := sessions.ListByClient(ctx, clientB.ID)
	if err != nil {
		t.Fatalf("ListByClient(B): %v", err)
	}
	if len(sessB) != 1 {
		t.Fatalf("expected 1 session for client B, got %d", len(sessB))
	}
	if sessB[0].TenantID != tidB {
		t.Errorf("ISOLATION BREACH: session for client B belongs to tenant %q", sessB[0].TenantID)
	}
}
