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

	deps := &tools.Deps{
		Feedback:       f.Feedback,
		FeedbackWriter: feedback.NewFeedback(f.Pool),
		WorkflowState:  f.Workflow,
		Tag:            f.Tags,
		TagAssign:      feedbacktagassignment.New(f.Pool),
	}

	d := jsonrpc.NewDispatcher()
	tools.RegisterReadTools(d, deps)
	tools.RegisterWriteTools(d, deps)

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
		Scopes:    []string{"mcp:read", "mcp:write"},
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
			if status == http.StatusOK && containsAny(body, forbiddenUUIDs...) {
				t.Errorf("MCP %s leaked tenant B data in response:\n%s", method, string(body))
			}
		})
	}
}

func TestMCP_BearerJWT_CrossTenantGetDenied(t *testing.T) {
	env := newMCPEnv(t)

	_, body := doJSONRPC(t, env, env.TokenA, "get_feedback", map[string]any{
		"id": env.Fixture.TenantB.FeedbackID,
	})

	var resp struct {
		Error *struct{ Message string } `json:"error"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	if resp.Error == nil {
		if containsAny(body, env.Fixture.TenantB.TenantID, fmt.Sprintf("%d", env.Fixture.TenantB.FeedbackID)) {
			t.Error("ISOLATION BREACH: get_feedback returned tenant B data via tenant A JWT")
		}
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
