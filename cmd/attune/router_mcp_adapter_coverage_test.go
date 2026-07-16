// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	mcpoauth "github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
)

func TestMCPClientStoreAdapterMapsRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := mcprepo.NewClients(newUnreachableMCPAdapterPool(t))
	adapter := newMCPClientStore(repo)
	clientID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000001")

	if _, err := adapter.GetByID(ctx, clientID); !errors.Is(err, mcpoauth.ErrInvalidClient) {
		t.Fatalf("GetByID error = %v, want ErrInvalidClient", err)
	}
	ok, err := adapter.ValidateRedirectURI(ctx, clientID, "https://example.test/callback")
	if err != nil || ok {
		t.Fatalf("ValidateRedirectURI = %t, %v; want false, nil", ok, err)
	}
}

func TestMCPCodeStoreAdapterMapsRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	adapter := newMCPCodeStore(mcprepo.NewCodes(newUnreachableMCPAdapterPool(t)))
	clientID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000002")

	if err := adapter.Create(ctx, ptrext.Of(mcpoauth.AuthCode{
		Code: "code-1", ClientID: clientID, TenantID: "tenant-1",
		RedirectURI: "https://example.test/callback", Scopes: []string{"feedback:read"},
		ExpiresAt: time.Now().Add(time.Minute),
	})); err == nil {
		t.Fatal("Create error = nil, want repo error")
	}
	if _, err := adapter.Consume(ctx, "missing-code"); !errors.Is(err, mcpoauth.ErrInvalidCode) {
		t.Fatalf("Consume error = %v, want ErrInvalidCode", err)
	}
}

func TestMCPTokenStoreAdapterMapsRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	adapter := newMCPTokenStore(mcprepo.NewTokens(newUnreachableMCPAdapterPool(t)))
	clientID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000003")
	sessionID := uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000004")

	if err := adapter.Create(ctx, ptrext.Of(mcpoauth.RefreshToken{
		TokenHash: "hash-1", ClientID: clientID, SessionID: sessionID,
		TenantID: "tenant-1", Scopes: []string{"feedback:read"}, ExpiresAt: time.Now().Add(time.Hour),
	})); err == nil {
		t.Fatal("Create error = nil, want repo error")
	}
	if _, err := adapter.GetByHash(ctx, "missing-hash"); !errors.Is(err, mcpoauth.ErrInvalidRefreshToken) {
		t.Fatalf("GetByHash error = %v, want ErrInvalidRefreshToken", err)
	}
	if err := adapter.Revoke(ctx, uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000005")); err == nil {
		t.Fatal("Revoke error = nil, want repo error")
	}
	if _, err := adapter.Consume(ctx, "missing-hash"); !errors.Is(err, mcpoauth.ErrInvalidRefreshToken) {
		t.Fatalf("Consume error = %v, want ErrInvalidRefreshToken", err)
	}
	if _, _, err := adapter.RotateToken(ctx, "old-hash", "new-hash", time.Now().Add(time.Hour)); !errors.Is(err, mcpoauth.ErrInvalidRefreshToken) {
		t.Fatalf("RotateToken error = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestMCPSessionAndPolicyAdaptersReturnRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := newUnreachableMCPAdapterPool(t)
	sessionAdapter := newMCPSessionStore(mcprepo.NewSessions(pool))
	session := ptrext.Of(mcpoauth.Session{
		ClientID: uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000006"),
		TenantID: "tenant-1", Scopes: []string{"feedback:read"},
	})

	if err := sessionAdapter.Create(ctx, session); err == nil {
		t.Fatal("Create session error = nil, want repo error")
	}
	if err := sessionAdapter.Touch(ctx, uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000007")); err == nil {
		t.Fatal("Touch error = nil, want repo error")
	}
	if _, err := sessionAdapter.IsActive(ctx, uuid.MustParse("aaaaaaaa-1000-4000-8000-000000000008")); err == nil {
		t.Fatal("IsActive error = nil, want repo error")
	}

	policyAdapter := newMCPToolPolicyStore(mcprepo.NewToolPolicies(pool))
	if _, err := policyAdapter.GetByClientAndTool(ctx, session.ClientID, "feedback.search"); err == nil {
		t.Fatal("GetByClientAndTool error = nil, want repo error")
	}
}

func newUnreachableMCPAdapterPool(t *testing.T) *pgxpool.Pool {
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
