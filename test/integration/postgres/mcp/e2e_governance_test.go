//go:build integration

package mcp_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp/policy"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
)

func TestE2E_SessionRevocationInvalidatesOnlyTargetSession(t *testing.T) {
	env := newTestEnv(t, "e2e-session-revoke-test", "E2E Session Revoke Test")
	ctx := context.Background()

	client := env.createClient(t, "session-revoke-agent", []string{"mcp:read"})
	tokenA := env.completeOAuthFlow(t, client.ID, "mcp:read")
	tokenB := env.completeOAuthFlow(t, client.ID, "mcp:read")

	sessionA := mustSessionIDFromAccessToken(t, tokenA.AccessToken)
	sessionB := mustSessionIDFromAccessToken(t, tokenB.AccessToken)
	if sessionA == sessionB {
		t.Fatal("expected separate OAuth flows to create distinct sessions")
	}

	resp, rpcResp := env.callMCPAPI(t, tokenA.AccessToken, "list_feedback", nil)
	if resp.StatusCode != 200 || rpcResp.Error != nil {
		t.Fatalf("session A should be usable before revoke, status=%d err=%+v", resp.StatusCode, rpcResp.Error)
	}
	resp, rpcResp = env.callMCPAPI(t, tokenB.AccessToken, "list_feedback", nil)
	if resp.StatusCode != 200 || rpcResp.Error != nil {
		t.Fatalf("session B should be usable before revoke, status=%d err=%+v", resp.StatusCode, rpcResp.Error)
	}

	if err := env.sessionsRepo.CloseWithReason(ctx, sessionA, "admin_revoked", "integration-test"); err != nil {
		t.Fatalf("revoke session A: %v", err)
	}

	resp, _ = env.callMCPAPI(t, tokenA.AccessToken, "list_feedback", nil)
	if resp.StatusCode != 401 {
		t.Fatalf("revoked session A status=%d, want 401", resp.StatusCode)
	}

	resp, rpcResp = env.callMCPAPI(t, tokenB.AccessToken, "list_feedback", nil)
	if resp.StatusCode != 200 || rpcResp.Error != nil {
		t.Fatalf("session B should still be usable after sibling revoke, status=%d err=%+v", resp.StatusCode, rpcResp.Error)
	}
}

func TestE2E_AllowListPolicyEnforcesToolAccessAndTracksActivity(t *testing.T) {
	env := newTestEnv(t, "e2e-allow-list-test", "E2E Allow List Test")
	ctx := context.Background()

	client := env.createClient(t, "allow-list-agent", []string{"mcp:read"})
	_, err := env.clientsRepo.UpdateGovernance(ctx, mcprepo.UpdateClientGovernanceParams{
		ID:             client.ID,
		ToolPolicyMode: domain.MCPToolPolicyModeAllowList,
	})
	if err != nil {
		t.Fatalf("update governance: %v", err)
	}
	_, err = env.toolPolicies.Upsert(ctx, mcprepo.UpsertToolPolicyParams{
		ClientID: client.ID,
		ToolName: "list_feedback",
		Effect:   domain.MCPToolPolicyEffectAllow,
	})
	if err != nil {
		t.Fatalf("upsert tool policy: %v", err)
	}

	var feedbackID int64
	err = env.pool.QueryRow(ctx, `
		INSERT INTO user_feedback (tenant_id, content, source, type, user_id, enrichment_status, created_at)
		VALUES ($1, 'allow-list test feedback', 'integration', 'other', 'user-1', 'pending', NOW())
		RETURNING id
	`, env.tenantID).Scan(&feedbackID)
	if err != nil {
		t.Fatalf("seed feedback: %v", err)
	}

	tokenData := env.completeOAuthFlow(t, client.ID, "mcp:read")
	sessionID := mustSessionIDFromAccessToken(t, tokenData.AccessToken)

	resp, rpcResp := env.callMCPAPI(t, tokenData.AccessToken, "list_feedback", nil)
	if resp.StatusCode != 200 || rpcResp.Error != nil {
		t.Fatalf("allowed tool should succeed, status=%d err=%+v", resp.StatusCode, rpcResp.Error)
	}

	resp, rpcResp = env.callMCPAPI(t, tokenData.AccessToken, "get_feedback", []byte(`{"id":`+strconv.FormatInt(feedbackID, 10)+`}`))
	if resp.StatusCode != 403 {
		t.Fatalf("disallowed tool status=%d, want 403", resp.StatusCode)
	}
	if rpcResp.Error == nil {
		t.Fatal("disallowed tool should return JSON-RPC error")
	}
	if rpcResp.Error.Message != "tool not allowed for client" {
		t.Fatalf("disallowed tool message=%q, want %q", rpcResp.Error.Message, "tool not allowed for client")
	}

	var lastTool, lastDecision string
	var lastActiveAt time.Time
	err = env.pool.QueryRow(ctx, `
		SELECT last_tool_name, last_decision, last_active_at
		FROM mcp_sessions
		WHERE id = $1
	`, sessionID).Scan(&lastTool, &lastDecision, &lastActiveAt)
	if err != nil {
		t.Fatalf("query session activity: %v", err)
	}
	if lastTool != "get_feedback" {
		t.Fatalf("last_tool_name=%q, want get_feedback", lastTool)
	}
	if lastDecision != policy.DecisionDeniedPolicy {
		t.Fatalf("last_decision=%q, want %q", lastDecision, policy.DecisionDeniedPolicy)
	}
	if time.Since(lastActiveAt) > time.Minute {
		t.Fatalf("last_active_at too old: %s", lastActiveAt)
	}
}

func mustSessionIDFromAccessToken(t *testing.T, accessToken string) uuid.UUID {
	t.Helper()
	claims := extractJWTClaims(t, accessToken)
	sessionIDStr, _ := claims["session_id"].(string)
	sessionID, err := uuid.Parse(sessionIDStr)
	if err != nil {
		t.Fatalf("parse session_id from access token: %v", err)
	}
	return sessionID
}
