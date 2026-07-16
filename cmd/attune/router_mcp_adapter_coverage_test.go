// ptrext:file-allow router adapter tests construct service pointers through public constructors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	mcpoauth "github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	"github.com/Phixsura/attune/internal/service/ingest"
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

func TestMCPIngestorAdapterForwardsToIngestor(t *testing.T) {
	t.Parallel()

	repo := &routerAdapterFeedbackRepo{insertID: 42}
	adapter := newMCPIngestorAdapter(ingest.NewIngestor(repo, nil, nil))

	id, err := adapter.Ingest(context.Background(), "tenant-1", "user-ignored", domain.IngestInput{
		Content: "checkout is broken",
		Source:  "api",
	})

	require.NoError(t, err)
	require.Equal(t, int64(42), id)
	require.Equal(t, "tenant-1", repo.tenantID)
	require.Equal(t, "api", repo.input.Source)
}

func TestMCPAuditAdapterRecordsMCPActor(t *testing.T) {
	t.Parallel()

	repo := &routerAdapterAuditRepo{}
	adapter := newMCPAuditAdapter(auditlogsvc.New(repo))

	err := adapter.Record(context.Background(), tools.AuditEvent{
		TenantID:   "tenant-1",
		Actor:      "client-1",
		ActorIP:    "203.0.113.10",
		UserAgent:  "mcp-test/1.0",
		Action:     "mcp.submit_feedback",
		TargetType: "feedback",
		TargetID:   "42",
		Summary:    "Submitted feedback",
		Before:     map[string]any{"state": "before"},
		After:      map[string]any{"state": "after"},
	})

	require.NoError(t, err)
	require.Len(t, repo.entries, 1)
	entry := repo.entries[0]
	require.Equal(t, "tenant-1", entry.TenantID)
	require.Equal(t, "mcp", entry.ActorType)
	require.Equal(t, "client-1", entry.ActorID)
	require.Equal(t, "203.0.113.10", entry.ActorIP)
	require.Equal(t, "mcp.submit_feedback", entry.Action)
	require.Equal(t, "feedback", entry.TargetType)
	require.Equal(t, "42", entry.TargetID)
}

func TestMCPAdapterConstructorsAllowNilDependencies(t *testing.T) {
	t.Parallel()

	require.NotNil(t, newMCPWorkflowAdapter(nil))
	require.NotNil(t, newMCPIngestorAdapter(nil))
	require.NotNil(t, newMCPAuditAdapter(nil))
	require.NotNil(t, newMCPToolPolicyStore(nil))
	require.NotNil(t, newMCPSessionStore(nil))
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

type routerAdapterFeedbackRepo struct {
	insertID int64
	tenantID string
	input    domain.IngestInput
}

func (r *routerAdapterFeedbackRepo) Insert(
	_ context.Context,
	tenantID string,
	_, _, _, _ string,
	in domain.IngestInput,
) (int64, error) {
	r.tenantID = tenantID
	r.input = in
	return r.insertID, nil
}

func (r *routerAdapterFeedbackRepo) InsertIdempotent(
	ctx context.Context,
	tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
	in domain.IngestInput,
	_ []byte,
) (int64, bool, error) {
	id, err := r.Insert(ctx, tenantID, userID, subjectKey, subjectDisplay, subjectHash, in)
	return id, false, err
}

type routerAdapterAuditRepo struct {
	entries     []auditlogrepo.Entry
	pruneRows   int64
	pruneErr    error
	pruneCutoff time.Time
}

func (r *routerAdapterAuditRepo) Insert(_ context.Context, entry auditlogrepo.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

func (r *routerAdapterAuditRepo) InsertTx(_ context.Context, _ pgx.Tx, entry auditlogrepo.Entry) error {
	r.entries = append(r.entries, entry)
	return nil
}

func (r *routerAdapterAuditRepo) List(context.Context, auditlogrepo.ListFilter) (auditlogrepo.ListResult, error) {
	return auditlogrepo.ListResult{}, nil
}

func (r *routerAdapterAuditRepo) PruneBefore(_ context.Context, cutoff time.Time) (int64, error) {
	r.pruneCutoff = cutoff
	return r.pruneRows, r.pruneErr
}
