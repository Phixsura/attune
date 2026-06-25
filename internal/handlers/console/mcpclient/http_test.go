// SPDX-License-Identifier: Apache-2.0

package mcpclient_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/handlers/console/mcpclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type fakeAuditRecorder struct {
	events []auditlogsvc.Event
}

func (f *fakeAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	f.events = append(f.events, event)
	return nil
}

func TestHandlerServeListReturnsJSON(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	handler := mcpclient.NewHandler(ptrext.Of(fakeClientStore{client: mcprepo.Client{
		ID:             clientID,
		TenantID:       "tenant-1",
		Name:           "agent",
		RedirectURIs:   []string{"https://example.com/callback"},
		Scopes:         []string{domain.MCPScopeRead},
		ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
		CreatedAt:      time.Now(),
		CreatedBy:      "admin",
	}}), nil, nil, nil)

	req := authRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeList(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp mcpclient.ListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Clients, 1)
	require.Equal(t, clientID.String(), resp.Clients[0].ID)
}

func TestHandlerServeCreateRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	handler := mcpclient.NewHandler(nil, nil, nil, nil)
	req := authRequest(http.MethodPost, "/", bytes.NewBufferString("{"))
	rec := httptest.NewRecorder()

	handler.ServeCreate(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "validation_error", resp["code"])
	require.Contains(t, resp["message"], "body")
}

func TestHandlerServeRevokeCascadesClientRevocation(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	clients := ptrext.Of(fakeClientStore{client: mcprepo.Client{
		ID:             clientID,
		TenantID:       "tenant-1",
		Name:           "agent",
		RedirectURIs:   []string{"https://example.com/callback"},
		Scopes:         []string{domain.MCPScopeRead},
		ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
		CreatedAt:      time.Now(),
		CreatedBy:      "admin",
	}})
	tokens := ptrext.Of(fakeTokenStore{})
	sessions := ptrext.Of(fakeSessionStore{})
	audit := ptrext.Of(fakeAuditRecorder{})
	handler := mcpclient.NewHandler(clients, tokens, sessions, nil)
	handler.SetAuditLogger(audit)

	req := routeRequest(
		authRequest(http.MethodDelete, "/", nil),
		map[string]string{"id": clientID.String()},
	)
	rec := httptest.NewRecorder()

	handler.ServeRevoke(rec, req)

	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, clientID, clients.revokedID)
	require.Equal(t, clientID, tokens.revokedClient)
	require.Equal(t, clientID, sessions.closedClient)
	require.Len(t, audit.events, 1)
	require.Equal(t, domain.AuditActionMCPClientRevoke, audit.events[0].Action)
}

func TestHandlerServeGovernanceEndpoints(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	sessionID := uuid.New()
	grantID := uuid.New()
	clients := ptrext.Of(fakeClientStore{client: mcprepo.Client{
		ID:             clientID,
		TenantID:       "tenant-1",
		Name:           "agent",
		RedirectURIs:   []string{"https://example.com/callback"},
		Scopes:         []string{domain.MCPScopeRead, domain.MCPScopeWrite},
		ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
		CreatedAt:      time.Now(),
		CreatedBy:      "admin",
	}})
	tokens := ptrext.Of(fakeTokenStore{grants: []mcprepo.RefreshToken{{
		ID:        grantID,
		ClientID:  clientID,
		SessionID: sessionID,
		Scopes:    []string{domain.MCPScopeRead},
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}}})
	sessions := ptrext.Of(fakeSessionStore{sessions: []mcprepo.Session{{
		ID:        sessionID,
		ClientID:  clientID,
		TenantID:  "tenant-1",
		Scopes:    []string{domain.MCPScopeRead},
		CreatedAt: time.Now(),
	}}})
	policies := ptrext.Of(fakeToolPolicyStore{})
	handler := mcpclient.NewHandler(clients, tokens, sessions, policies)

	t.Run("get", func(t *testing.T) {
		req := routeRequest(
			authRequest(http.MethodGet, "/", nil),
			map[string]string{"id": clientID.String()},
		)
		rec := httptest.NewRecorder()

		handler.ServeGet(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("update", func(t *testing.T) {
		req := routeRequest(
			authRequest(http.MethodPatch, "/", bytes.NewBufferString(`{"tool_policy_mode":"allow_list","rate_limit_rpm":30,"rate_limit_burst":5}`)),
			map[string]string{"id": clientID.String()},
		)
		rec := httptest.NewRecorder()

		handler.ServeUpdate(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, domain.MCPToolPolicyModeAllowList, clients.client.ToolPolicyMode)
		require.Equal(t, 30, ptrext.IndirectOr(clients.client.RateLimitRPM, 0))
		require.Equal(t, 5, ptrext.IndirectOr(clients.client.RateLimitBurst, 0))
	})

	t.Run("replace tool policies", func(t *testing.T) {
		req := routeRequest(
			authRequest(http.MethodPut, "/", bytes.NewBufferString(`{"policies":[{"tool_name":"list_feedback","effect":"allow"}]}`)),
			map[string]string{"id": clientID.String()},
		)
		rec := httptest.NewRecorder()

		handler.ServeReplaceToolPolicies(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Len(t, policies.replacements, 1)
		require.Equal(t, "list_feedback", policies.replacements[0].ToolName)
	})

	t.Run("revoke session", func(t *testing.T) {
		req := routeRequest(
			authRequest(http.MethodDelete, "/", nil),
			map[string]string{"id": clientID.String(), "session_id": sessionID.String()},
		)
		rec := httptest.NewRecorder()

		handler.ServeRevokeSession(rec, req)

		require.Equal(t, http.StatusNoContent, rec.Code)
		require.Equal(t, sessionID, tokens.revokedSession)
		require.Equal(t, sessionID, sessions.closedID)
	})

	t.Run("revoke refresh grant", func(t *testing.T) {
		req := routeRequest(
			authRequest(http.MethodDelete, "/", nil),
			map[string]string{"id": clientID.String(), "grant_id": grantID.String()},
		)
		rec := httptest.NewRecorder()

		handler.ServeRevokeRefreshGrant(rec, req)

		require.Equal(t, http.StatusNoContent, rec.Code)
		require.Equal(t, grantID, tokens.revokedGrant)
	})
}

func authRequest(method, target string, body *bytes.Buffer) *http.Request {
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, body)
	}
	return req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserID:   "admin-user",
		UserType: "admin",
	})))
}

func routeRequest(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for key, value := range params {
		rctx.URLParams.Add(key, value)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}
