// SPDX-License-Identifier: Apache-2.0

package mcp_test

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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/mcp"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/oauth"
	mcppolicy "github.com/Phixsura/attune/internal/mcp/policy"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

type mockFeedbackReader struct{}

func (m *mockFeedbackReader) ListForConsole(_ context.Context, _ string, _ feedback.ConsoleListOpts) ([]feedback.ConsoleListRow, error) {
	return []feedback.ConsoleListRow{{ID: 1, Content: "test"}}, nil
}

func (m *mockFeedbackReader) GetForConsole(_ context.Context, _ string, _ int64) (*feedback.ConsoleDetailRow, error) {
	return nil, feedback.ErrFeedbackNotFound
}

type mockClientStore struct {
	toolPolicyMode string
	rateLimitRPM   *int
	rateLimitBurst *int
}

func (m *mockClientStore) GetByID(_ context.Context, id uuid.UUID) (*oauth.Client, error) {
	mode := m.toolPolicyMode
	if mode == "" {
		mode = domain.MCPToolPolicyModeLegacyAllowAll
	}
	return ptrext.Of(oauth.Client{
		ID:             id,
		TenantID:       "tenant-123",
		Name:           "test-client",
		RedirectURIs:   []string{"https://attune.example.com/callback"},
		Scopes:         []string{domain.MCPScopeRead, domain.MCPScopeWrite, domain.MCPScopeIngest},
		ToolPolicyMode: mode,
		RateLimitRPM:   m.rateLimitRPM,
		RateLimitBurst: m.rateLimitBurst,
		CreatedAt:      time.Now(),
	}), nil
}

func (m *mockClientStore) ValidateRedirectURI(_ context.Context, _ uuid.UUID, _ string) (bool, error) {
	return true, nil
}

type mockCodeStore struct{}

func (m *mockCodeStore) Create(_ context.Context, _ *oauth.AuthCode) error { return nil }
func (m *mockCodeStore) Consume(_ context.Context, _ string) (*oauth.AuthCode, error) {
	return nil, oauth.ErrInvalidCode
}

type mockTokenStore struct{}

func (m *mockTokenStore) Create(_ context.Context, _ *oauth.RefreshToken) error { return nil }
func (m *mockTokenStore) GetByHash(_ context.Context, _ string) (*oauth.RefreshToken, error) {
	return nil, oauth.ErrInvalidRefreshToken
}
func (m *mockTokenStore) Revoke(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockTokenStore) Consume(_ context.Context, _ string) (*oauth.RefreshToken, error) {
	return nil, oauth.ErrInvalidRefreshToken
}

func (m *mockTokenStore) RotateToken(_ context.Context, _, _ string, _ time.Time) (*oauth.RefreshToken, *oauth.RefreshToken, error) {
	return nil, nil, oauth.ErrInvalidRefreshToken
}

type mockSessionStore struct{}

func (m *mockSessionStore) Create(_ context.Context, _ *oauth.Session) error { return nil }
func (m *mockSessionStore) Touch(_ context.Context, _ uuid.UUID) error       { return nil }
func (m *mockSessionStore) IsActive(_ context.Context, _ uuid.UUID) (bool, error) {
	return true, nil
}

type mockSessionValidator struct{}

func (m *mockSessionValidator) IsActive(_ context.Context, _ uuid.UUID) (bool, error) {
	return true, nil
}

type mockToolPolicyStore struct {
	policies map[string]*mcppolicy.ToolPolicy
}

func (m *mockToolPolicyStore) GetByClientAndTool(_ context.Context, _ uuid.UUID, toolName string) (*mcppolicy.ToolPolicy, error) {
	if m == nil || m.policies == nil {
		return nil, nil
	}
	return m.policies[toolName], nil
}

type mockSessionActivityTracker struct{}

func (m *mockSessionActivityTracker) RecordActivity(context.Context, uuid.UUID, string, string, string, string) error {
	return nil
}

type testHandlerOptions struct {
	toolPolicyMode string
	toolPolicies   map[string]*mcppolicy.ToolPolicy
	rateLimitRPM   *int
	rateLimitBurst *int
}

func newTestHandler(opts ...testHandlerOptions) *mcp.Handler {
	var cfgOpts testHandlerOptions
	if len(opts) > 0 {
		cfgOpts = opts[0]
	}

	cfg := mcp.Config{
		PublicBaseURL: "https://attune.example.com",
		JWTSecret:     []byte("test-secret-key-for-jwt-signing-32b"),
		JWTIssuer:     "https://attune.example.com/mcp/oauth",
	}
	stores := mcp.Stores{
		Clients: ptrext.Of(mockClientStore{
			toolPolicyMode: cfgOpts.toolPolicyMode,
			rateLimitRPM:   cfgOpts.rateLimitRPM,
			rateLimitBurst: cfgOpts.rateLimitBurst,
		}),
		Codes:            ptrext.Of(mockCodeStore{}),
		Tokens:           ptrext.Of(mockTokenStore{}),
		Sessions:         ptrext.Of(mockSessionStore{}),
		ToolPolicies:     ptrext.Of(mockToolPolicyStore{policies: cfgOpts.toolPolicies}),
		SessionValidator: ptrext.Of(mockSessionValidator{}),
		SessionActivity:  ptrext.Of(mockSessionActivityTracker{}),
	}
	deps := ptrext.Of(tools.Deps{
		Feedback: ptrext.Of(mockFeedbackReader{}),
	})
	return mcp.NewHandler(cfg, stores, deps)
}

func newTestToken(t *testing.T, h *mcp.Handler) string {
	t.Helper()
	return newTestTokenWithScopes(t, h, []string{"mcp:read"})
}

func newTestTokenWithScopes(t *testing.T, h *mcp.Handler, scopes []string) string {
	t.Helper()
	claims := oauth.AccessTokenClaims{TenantID: "tenant-123", ClientID: uuid.New(), SessionID: uuid.New(), Scopes: scopes}
	token, err := h.Signer().Sign(claims, time.Hour)
	require.NoError(t, err)
	return token
}

func TestHandler_Discovery(t *testing.T) {
	h := newTestHandler()
	router := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp oauth.DiscoveryResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "https://attune.example.com/mcp/v1", resp.Resource)
}

func TestHandler_MountWellKnownRoutes(t *testing.T) {
	h := newTestHandler()
	router := chi.NewRouter()
	require.NoError(t, h.MountWellKnownRoutes(router))
	router.Mount("/mcp", h.Routes())

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource/mcp/v1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var prm oauth.DiscoveryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &prm))
	assert.Equal(t, "https://attune.example.com/mcp/v1", prm.Resource)

	req = httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server/mcp/oauth", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var metadata oauth.AuthorizationServerMetadataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &metadata))
	assert.Equal(t, "https://attune.example.com/mcp/oauth", metadata.Issuer)
	assert.Equal(t, "https://attune.example.com/mcp/oauth/authorize", metadata.AuthorizationEndpoint)
	assert.Equal(t, "https://attune.example.com/mcp/oauth/token", metadata.TokenEndpoint)

	req = httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration/mcp/oauth", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestHandler_Unauthorized(t *testing.T) {
	h := newTestHandler()
	router := h.Routes()

	body := `{"jsonrpc":"2.0","method":"list_feedback","id":"1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "resource_metadata")
}

func TestHandler_AuthenticatedRequest(t *testing.T) {
	h := newTestHandler()
	router := h.Routes()
	token := newTestToken(t, h)

	body := `{"jsonrpc":"2.0","method":"list_feedback","id":"1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp jsonrpc.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Nil(t, resp.Error)
}

func TestHandler_AuthenticatedRequestDeniedByAllowList(t *testing.T) {
	h := newTestHandler(testHandlerOptions{
		toolPolicyMode: domain.MCPToolPolicyModeAllowList,
	})
	router := h.Routes()
	token := newTestToken(t, h)

	body := `{"jsonrpc":"2.0","method":"list_feedback","id":"1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	var resp jsonrpc.Response
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, "tool not allowed for client", resp.Error.Message)
}

func TestHandler_OAuthEndpoints(t *testing.T) {
	h := newTestHandler()
	router := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=invalid&response_type=code", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_SecurityHeaders(t *testing.T) {
	h := newTestHandler()
	router := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"))
}

func TestHandler_InsufficientScopeChallenge(t *testing.T) {
	h := newTestHandler()
	router := h.Routes()
	token := newTestTokenWithScopes(t, h, []string{"mcp:read"})

	req := httptest.NewRequest(http.MethodPost, "/v1", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"submit_feedback","id":"1","params":{"content":"hi"}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `error="insufficient_scope"`)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), `scope="mcp:ingest"`)
}

func TestHandler_RateLimitHeaders(t *testing.T) {
	h := newTestHandler()
	router := h.Routes()
	token := newTestToken(t, h)

	req := httptest.NewRequest(http.MethodPost, "/v1", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"list_feedback","id":"1"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Limit"))
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Reset"))
}

func TestHandler_OAuthRateLimitHeaders(t *testing.T) {
	h := newTestHandler()
	router := h.Routes()

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?client_id=invalid&response_type=code", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	// Even on error, rate limit headers should be present
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Limit"))
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, rec.Header().Get("X-RateLimit-Reset"))
}

func TestHandler_ClientBurstLimitAllowsConfiguredBurst(t *testing.T) {
	t.Parallel()

	rpm := 1
	burst := 2
	h := newTestHandler(testHandlerOptions{
		rateLimitRPM:   ptrext.Of(rpm),
		rateLimitBurst: ptrext.Of(burst),
	})
	router := h.Routes()
	token := newTestToken(t, h)

	makeRequest := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"list_feedback","id":"1"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	assert.Equal(t, http.StatusOK, makeRequest().Code)
	assert.Equal(t, http.StatusOK, makeRequest().Code)

	rec := makeRequest()
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
}

func TestHandler_ToolBurstLimitUsesToolOverride(t *testing.T) {
	t.Parallel()

	rpm := 1
	burst := 2
	h := newTestHandler(testHandlerOptions{
		toolPolicies: map[string]*mcppolicy.ToolPolicy{
			"list_feedback": {
				Effect:         domain.MCPToolPolicyEffectAllow,
				RateLimitRPM:   ptrext.Of(rpm),
				RateLimitBurst: ptrext.Of(burst),
			},
		},
	})
	router := h.Routes()
	token := newTestToken(t, h)

	makeRequest := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"list_feedback","id":"1"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	assert.Equal(t, http.StatusOK, makeRequest().Code)
	assert.Equal(t, http.StatusOK, makeRequest().Code)
	assert.Equal(t, http.StatusTooManyRequests, makeRequest().Code)
}
