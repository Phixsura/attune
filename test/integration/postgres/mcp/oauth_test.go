//go:build integration

package mcp_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestPG_MCPClientsCreateListRevoke(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "mcp-test-io", "MCP Test IO")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	clients := mcprepo.NewClients(pool)

	client, err := clients.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tenantID,
		Name:         "test-agent",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"mcp:read", "mcp:write"},
		CreatedBy:    "test-user",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if client.ID == uuid.Nil {
		t.Fatal("expected non-nil client ID")
	}
	if client.Name != "test-agent" {
		t.Fatalf("unexpected name: %s", client.Name)
	}

	list, err := clients.ListByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListByTenant: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 client, got %d", len(list))
	}
	if list[0].ID != client.ID {
		t.Fatalf("client ID mismatch")
	}

	got, err := clients.GetByID(ctx, client.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "test-agent" {
		t.Fatalf("GetByID name mismatch: %s", got.Name)
	}

	if err := clients.Revoke(ctx, client.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	revoked, err := clients.GetByID(ctx, client.ID)
	if err != nil {
		t.Fatalf("GetByID after revoke: %v", err)
	}
	if revoked.RevokedAt == nil {
		t.Fatal("expected revoked_at to be set")
	}

	_, err = clients.GetActiveByID(ctx, client.ID)
	if !errors.Is(err, mcprepo.ErrClientNotFound) {
		t.Fatalf("GetActiveByID after revoke: got %v, want %v", err, mcprepo.ErrClientNotFound)
	}
}

func TestPG_MCPCodesCreateConsume(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "mcp-codes-io", "MCP Codes IO")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	clients := mcprepo.NewClients(pool)
	client, err := clients.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tenantID,
		Name:         "code-test-agent",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "test-user",
	})
	if err != nil {
		t.Fatalf("Create client: %v", err)
	}

	codes := mcprepo.NewCodes(pool)
	code, err := codes.Create(ctx, mcprepo.CreateCodeParams{
		ClientID:      client.ID,
		RedirectURI:   "http://localhost:8080/callback",
		Scopes:        []string{"mcp:read"},
		CodeChallenge: "challenge123",
		UserID:        tenantID,
		ExpiresAt:     time.Now().Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Create code: %v", err)
	}
	if len(code.Code) == 0 {
		t.Fatal("expected non-empty code")
	}

	consumed, err := codes.Consume(ctx, code.Code)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if consumed.ClientID != client.ID {
		t.Fatalf("consumed client ID mismatch")
	}

	_, err = codes.Consume(ctx, code.Code)
	if !errors.Is(err, mcprepo.ErrCodeNotFound) {
		t.Fatalf("second Consume: got %v, want %v", err, mcprepo.ErrCodeNotFound)
	}
}

func TestPG_MCPRefreshTokensCreateRevokeByClient(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "mcp-tokens-io", "MCP Tokens IO")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	clients := mcprepo.NewClients(pool)
	client, err := clients.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tenantID,
		Name:         "token-test-agent",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "test-user",
	})
	if err != nil {
		t.Fatalf("Create client: %v", err)
	}

	tokens := mcprepo.NewTokens(pool)
	result, err := tokens.Create(ctx, mcprepo.CreateRefreshTokenParams{
		ClientID:  client.ID,
		Scopes:    []string{"mcp:read"},
		UserID:    tenantID,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Create token: %v", err)
	}
	if len(result.RawToken) == 0 {
		t.Fatal("expected non-empty raw token")
	}

	found, err := tokens.GetByToken(ctx, result.RawToken)
	if err != nil {
		t.Fatalf("GetByToken: %v", err)
	}
	if found.ID != result.Token.ID {
		t.Fatalf("token ID mismatch")
	}

	count, err := tokens.RevokeByClient(ctx, client.ID)
	if err != nil {
		t.Fatalf("RevokeByClient: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 token revoked, got %d", count)
	}

	_, err = tokens.GetByToken(ctx, result.RawToken)
	if !errors.Is(err, mcprepo.ErrRefreshTokenNotFound) {
		t.Fatalf("GetByToken after revoke: got %v, want %v", err, mcprepo.ErrRefreshTokenNotFound)
	}
}

func TestPG_MCPSessionsCreateTouch(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "mcp-sessions-io", "MCP Sessions IO")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	clients := mcprepo.NewClients(pool)
	client, err := clients.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tenantID,
		Name:         "session-test-agent",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "test-user",
	})
	if err != nil {
		t.Fatalf("Create client: %v", err)
	}

	sessions := mcprepo.NewSessions(pool)
	session, err := sessions.Create(ctx, mcprepo.CreateSessionParams{
		ClientID: client.ID,
		TenantID: tenantID,
		Scopes:   []string{"mcp:read"},
	})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	if session.ID == uuid.Nil {
		t.Fatal("expected non-nil session ID")
	}

	got, err := sessions.GetByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.TenantID != tenantID {
		t.Fatalf("tenant ID mismatch")
	}

	if err := sessions.Touch(ctx, session.ID); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	if err := sessions.Close(ctx, session.ID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err = sessions.GetByID(ctx, session.ID)
	if !errors.Is(err, mcprepo.ErrSessionNotFound) {
		t.Fatalf("GetByID after close: got %v, want %v", err, mcprepo.ErrSessionNotFound)
	}
}
