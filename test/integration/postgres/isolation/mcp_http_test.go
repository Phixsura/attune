//go:build integration

package isolation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/server"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbacktagassignment"
)

type mcpEnv struct {
	Server  *httptest.Server
	TokenA  string
	TokenB  string
	Fixture *Fixture
}

type mockClientValidator struct{ revoked bool }

func (m *mockClientValidator) IsRevoked(_ context.Context, _ uuid.UUID) (bool, error) {
	return m.revoked, nil
}

type mockSessionValidator struct{ active bool }

func (m *mockSessionValidator) IsActive(_ context.Context, _ uuid.UUID) (bool, error) {
	return m.active, nil
}

type mockWorkflowTransitioner struct {
	lastTenantID string
}

func (m *mockWorkflowTransitioner) Transition(_ context.Context, tenantID string, _ int64, _, _, _ string) error {
	m.lastTenantID = tenantID
	return fmt.Errorf("feedback not found")
}

type mockIngestor struct {
	lastTenantID string
}

func (m *mockIngestor) Ingest(_ context.Context, tenantID, _ string, _ domain.IngestInput) (int64, error) {
	m.lastTenantID = tenantID
	return 0, nil
}

func newMCPEnv(t *testing.T) *mcpEnv {
	t.Helper()

	f := NewFixture(t)

	jwtSecret := []byte("test-isolation-secret-must-be-at-least-32-bytes")
	signer := oauth.NewJWTSigner(jwtSecret, "test-issuer")
	auth := server.NewAuthMiddleware(
		signer,
		&mockClientValidator{revoked: false},
		&mockSessionValidator{active: true},
		"",
	)

	wfTransit := &mockWorkflowTransitioner{}
	ingestor := &mockIngestor{}
	deps := &tools.Deps{
		Feedback:        f.Feedback,
		FeedbackWriter:  feedback.NewFeedback(f.Pool),
		WorkflowState:   f.Workflow,
		WorkflowTransit: wfTransit,
		Tag:             f.Tags,
		TagAssign:       feedbacktagassignment.New(f.Pool),
		Ingestor:        ingestor,
	}

	d := jsonrpc.NewDispatcher()
	tools.RegisterReadTools(d, deps)
	tools.RegisterWriteTools(d, deps)
	tools.RegisterIngestTools(d, deps)

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(auth.Wrap)
		r.Post("/mcp/v1", jsonrpc.NewHandler(d).ServeHTTP)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	tokenA := signJWT(t, signer, f.TenantA.TenantID, f.TenantA.MCPClientID)
	tokenB := signJWT(t, signer, f.TenantB.TenantID, f.TenantB.MCPClientID)

	return &mcpEnv{Server: srv, TokenA: tokenA, TokenB: tokenB, Fixture: f}
}

func signJWT(t *testing.T, signer *oauth.JWTSigner, tenantID string, clientID uuid.UUID) string {
	t.Helper()
	token, err := signer.Sign(oauth.AccessTokenClaims{
		TenantID:  tenantID,
		ClientID:  clientID,
		SessionID: uuid.New(),
		Scopes:    []string{"mcp:read", "mcp:write", "mcp:ingest"},
	}, time.Hour)
	require.NoError(t, err)
	return token
}

func doJSONRPC(t *testing.T, env *mcpEnv, bearerToken, method string, params any) (int, json.RawMessage) {
	t.Helper()
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      1,
	}
	if params != nil {
		reqBody["params"] = params
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, env.Server.URL+"/mcp/v1", strings.NewReader(string(body)))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+bearerToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, respBody
}

type jsonrpcResult struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func assertNoLeakedIDs(t *testing.T, label string, raw json.RawMessage, forbidden []string) {
	t.Helper()
	s := string(raw)
	for _, id := range forbidden {
		if id != "" && strings.Contains(s, id) {
			t.Errorf("ISOLATION BREACH: %s result contains forbidden ID %s", label, id)
		}
	}
}

func TestMCP_BearerJWT_CrossTenantListsDenied(t *testing.T) {
	env := newMCPEnv(t)

	forbiddenUUIDs := []string{
		env.Fixture.TenantB.TenantID,
		env.Fixture.TenantB.TagID.String(),
		env.Fixture.TenantB.WorkflowID,
	}

	listMethods := []string{"list_feedback", "list_tags", "list_workflow_states"}

	for _, method := range listMethods {
		t.Run(method, func(t *testing.T) {
			status, body := doJSONRPC(t, env, env.TokenA, method, nil)
			if status != http.StatusOK {
				return
			}
			var resp jsonrpcResult
			if json.Unmarshal(body, &resp) != nil || resp.Error != nil {
				return
			}
			assertNoLeakedIDs(t, method, resp.Result, forbiddenUUIDs)
		})
	}
}

func TestMCP_BearerJWT_CrossTenantGetDenied(t *testing.T) {
	env := newMCPEnv(t)

	getOps := []struct {
		name   string
		method string
		params map[string]any
	}{
		{"get_feedback", "get_feedback", map[string]any{
			"id": env.Fixture.TenantB.FeedbackID,
		}},
		{"get_workflow_state", "get_workflow_state", map[string]any{
			"id": env.Fixture.TenantB.WorkflowID,
		}},
	}

	for _, op := range getOps {
		t.Run(op.name, func(t *testing.T) {
			_, body := doJSONRPC(t, env, env.TokenA, op.method, op.params)

			var resp jsonrpcResult
			require.NoError(t, json.Unmarshal(body, &resp))
			if resp.Error == nil {
				assertNoLeakedIDs(t, op.name, resp.Result, []string{env.Fixture.TenantB.TenantID})
			}
		})
	}
}

func TestMCP_BearerJWT_CrossTenantWriteDenied(t *testing.T) {
	env := newMCPEnv(t)

	writeOps := []struct {
		name   string
		method string
		params map[string]any
	}{
		{"set_urgent", "set_urgent", map[string]any{
			"feedback_id": env.Fixture.TenantB.FeedbackID, "urgent": true,
		}},
		{"add_tag", "add_tag", map[string]any{
			"feedback_id": env.Fixture.TenantB.FeedbackID,
			"tag_id":      env.Fixture.TenantA.TagID.String(),
		}},
		{"remove_tag", "remove_tag", map[string]any{
			"feedback_id": env.Fixture.TenantB.FeedbackID,
			"tag_id":      env.Fixture.TenantB.TagID.String(),
		}},
		{"update_workflow_state", "update_workflow_state", map[string]any{
			"feedback_id": env.Fixture.TenantB.FeedbackID,
			"state_id":    env.Fixture.TenantB.WorkflowID,
		}},
	}

	for _, op := range writeOps {
		t.Run(op.name, func(t *testing.T) {
			_, body := doJSONRPC(t, env, env.TokenA, op.method, op.params)
			if containsAny(body, env.Fixture.TenantB.TenantID) {
				t.Errorf("MCP %s leaked tenant B data:\n%s", op.name, string(body))
			}
		})
	}

	verifyFeedbackBIntact(t, env)
}

func TestMCP_BearerJWT_SubmitFeedbackTenantBinding(t *testing.T) {
	env := newMCPEnv(t)

	_, _ = doJSONRPC(t, env, env.TokenA, "submit_feedback", map[string]any{
		"content": "isolation test feedback",
		"source":  "api",
	})

	_, body := doJSONRPC(t, env, env.TokenB, "list_feedback", nil)
	if containsAny(body, "isolation test feedback") {
		t.Error("ISOLATION BREACH: feedback submitted via tenant A JWT appeared in tenant B list")
	}
}

func TestMCP_BearerJWT_TokenSwapDenied(t *testing.T) {
	env := newMCPEnv(t)

	_, bodyA := doJSONRPC(t, env, env.TokenA, "list_feedback", nil)
	_, bodyB := doJSONRPC(t, env, env.TokenB, "list_feedback", nil)

	if containsAny(bodyA, env.Fixture.TenantB.TenantID) {
		t.Error("tenant A token returned tenant B data")
	}
	if containsAny(bodyB, env.Fixture.TenantA.TenantID) {
		t.Error("tenant B token returned tenant A data")
	}
}

func verifyFeedbackBIntact(t *testing.T, env *mcpEnv) {
	t.Helper()
	_, body := doJSONRPC(t, env, env.TokenB, "get_feedback", map[string]any{
		"id": env.Fixture.TenantB.FeedbackID,
	})
	var resp struct {
		Result *struct{ ID int64 } `json:"result"`
	}
	if json.Unmarshal(body, &resp) == nil && resp.Result != nil {
		if resp.Result.ID != env.Fixture.TenantB.FeedbackID {
			t.Error("tenant B feedback was corrupted by cross-tenant write attempts")
		}
	}
}
