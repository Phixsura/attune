// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestClientsRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pool := newUnreachableMCPPool(t)
	r := NewClients(pool)
	clientID := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")

	expectMCPRepoErr(t, "Clients.Create", func() error {
		_, err := r.Create(ctx, CreateClientParams{
			TenantID:     "tenant-1",
			Name:         "Agent",
			RedirectURIs: []string{"https://example.test/callback"},
			Scopes:       []string{"mcp:tools"},
			CreatedBy:    "admin-1",
		})
		return err
	})
	expectMCPRepoErr(t, "Clients.GetByID", func() error {
		_, err := r.GetByID(ctx, clientID)
		return err
	})
	expectMCPRepoErr(t, "Clients.GetActiveByID", func() error {
		_, err := r.GetActiveByID(ctx, clientID)
		return err
	})
	expectMCPRepoErr(t, "Clients.ListByTenant", func() error {
		_, err := r.ListByTenant(ctx, "tenant-1")
		return err
	})
	expectMCPRepoErr(t, "Clients.Revoke", func() error {
		return r.Revoke(ctx, clientID)
	})
	expectMCPRepoErr(t, "Clients.UpdateGovernance", func() error {
		_, err := r.UpdateGovernance(ctx, UpdateClientGovernanceParams{
			ID:             clientID,
			ToolPolicyMode: "manual",
		})
		return err
	})
	expectMCPRepoErr(t, "Clients.IsRevoked", func() error {
		_, err := r.IsRevoked(ctx, clientID)
		return err
	})
}

func TestCodesRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := NewCodes(newUnreachableMCPPool(t))
	clientID := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")

	expectMCPRepoErr(t, "Codes.Create", func() error {
		_, err := r.Create(ctx, CreateCodeParams{
			Code:          "code-1",
			ClientID:      clientID,
			RedirectURI:   "https://example.test/callback",
			Scopes:        []string{"mcp:tools"},
			CodeChallenge: "challenge",
			UserID:        "user-1",
			ExpiresAt:     time.Now().UTC().Add(time.Minute),
		})
		return err
	})
	expectMCPRepoErr(t, "Codes.Consume", func() error {
		_, err := r.Consume(ctx, "code-1")
		return err
	})
	expectMCPRepoErr(t, "Codes.Cleanup", func() error {
		_, err := r.Cleanup(ctx)
		return err
	})
}

func TestSessionsRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := NewSessions(newUnreachableMCPPool(t))
	sessionID := uuid.MustParse("bbbbbbbb-1111-2222-3333-cccccccccccc")
	clientID := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")

	expectMCPRepoErr(t, "Sessions.Create", func() error {
		_, err := r.Create(ctx, CreateSessionParams{
			ClientID: clientID,
			TenantID: "tenant-1",
			Scopes:   []string{"mcp:tools"},
		})
		return err
	})
	expectMCPRepoErr(t, "Sessions.GetByID", func() error {
		_, err := r.GetByID(ctx, sessionID)
		return err
	})
	expectMCPRepoErr(t, "Sessions.ListByClient", func() error {
		_, err := r.ListByClient(ctx, clientID)
		return err
	})
	expectMCPRepoErr(t, "Sessions.Touch", func() error {
		return r.Touch(ctx, sessionID)
	})
	expectMCPRepoErr(t, "Sessions.RecordActivity", func() error {
		return r.RecordActivity(ctx, sessionID, "tool.echo", "allow", "127.0.0.1", "agent")
	})
	expectMCPRepoErr(t, "Sessions.Close", func() error {
		return r.Close(ctx, sessionID)
	})
	expectMCPRepoErr(t, "Sessions.CloseWithReason", func() error {
		return r.CloseWithReason(ctx, sessionID, "operator", "admin-1")
	})
	expectMCPRepoErr(t, "Sessions.CloseByClient", func() error {
		_, err := r.CloseByClient(ctx, clientID)
		return err
	})
	expectMCPRepoErr(t, "Sessions.CleanupIdle", func() error {
		_, err := r.CleanupIdle(ctx, time.Minute)
		return err
	})
	expectMCPRepoErr(t, "Sessions.IsActive", func() error {
		_, err := r.IsActive(ctx, sessionID)
		return err
	})
}

func TestTokensRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := NewTokens(newUnreachableMCPPool(t))
	tokenID := uuid.MustParse("dddddddd-1111-2222-3333-eeeeeeeeeeee")
	sessionID := uuid.MustParse("bbbbbbbb-1111-2222-3333-cccccccccccc")
	clientID := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")
	expiresAt := time.Now().UTC().Add(time.Hour)

	expectMCPRepoErr(t, "Tokens.Create", func() error {
		_, err := r.Create(ctx, CreateRefreshTokenParams{
			ClientID:  clientID,
			Scopes:    []string{"mcp:tools"},
			UserID:    "user-1",
			ExpiresAt: expiresAt,
		})
		return err
	})
	expectMCPRepoErr(t, "Tokens.CreateWithHash", func() error {
		_, err := r.CreateWithHash(ctx, CreateWithHashParams{
			TokenHash: "hash-1",
			ClientID:  clientID,
			SessionID: sessionID,
			Scopes:    []string{"mcp:tools"},
			UserID:    "user-1",
			ExpiresAt: expiresAt,
		})
		return err
	})
	expectMCPRepoErr(t, "Tokens.GetByToken", func() error {
		_, err := r.GetByToken(ctx, "raw-token")
		return err
	})
	expectMCPRepoErr(t, "Tokens.GetByHash", func() error {
		_, err := r.GetByHash(ctx, "hash-1")
		return err
	})
	expectMCPRepoErr(t, "Tokens.Revoke", func() error {
		return r.Revoke(ctx, tokenID)
	})
	expectMCPRepoErr(t, "Tokens.Consume", func() error {
		_, err := r.Consume(ctx, "hash-1")
		return err
	})
	expectMCPRepoErr(t, "Tokens.RotateToken", func() error {
		_, _, err := r.RotateToken(ctx, RotateTokenParams{
			OldTokenHash: "old-hash",
			NewTokenHash: "new-hash",
			NewExpiresAt: expiresAt,
		})
		return err
	})
	expectMCPRepoErr(t, "Tokens.RevokeByClient", func() error {
		_, err := r.RevokeByClient(ctx, clientID)
		return err
	})
	expectMCPRepoErr(t, "Tokens.RevokeBySession", func() error {
		_, err := r.RevokeBySession(ctx, sessionID)
		return err
	})
	expectMCPRepoErr(t, "Tokens.ListByClient", func() error {
		_, err := r.ListByClient(ctx, clientID)
		return err
	})
	expectMCPRepoErr(t, "Tokens.Cleanup", func() error {
		_, err := r.Cleanup(ctx)
		return err
	})
}

func TestToolPoliciesRepoMethodsReturnPoolErrors(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := NewToolPolicies(newUnreachableMCPPool(t))
	clientID := uuid.MustParse("aaaaaaaa-1111-2222-3333-bbbbbbbbbbbb")
	policy := UpsertToolPolicyParams{
		ClientID: clientID,
		ToolName: "tool.echo",
		Effect:   "allow",
	}

	expectMCPRepoErr(t, "ToolPolicies.GetByClientAndTool", func() error {
		_, err := r.GetByClientAndTool(ctx, clientID, "tool.echo")
		return err
	})
	expectMCPRepoErr(t, "ToolPolicies.ListByClient", func() error {
		_, err := r.ListByClient(ctx, clientID)
		return err
	})
	expectMCPRepoErr(t, "ToolPolicies.Upsert", func() error {
		_, err := r.Upsert(ctx, policy)
		return err
	})
	expectMCPRepoErr(t, "ToolPolicies.ReplaceByClient", func() error {
		return r.ReplaceByClient(ctx, clientID, []UpsertToolPolicyParams{policy})
	})
}

func newUnreachableMCPPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func expectMCPRepoErr(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
