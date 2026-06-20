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

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/handlers/mcp"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/oauth"
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

type mockClientStore struct{}

func (m *mockClientStore) GetByID(_ context.Context, _ uuid.UUID) (*oauth.Client, error) {
	return nil, oauth.ErrInvalidClient
}

func (m *mockClientStore) ValidateRedirectURI(_ context.Context, _ uuid.UUID, _ string) (bool, error) {
	return false, nil
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

func newTestHandler() *mcp.Handler {
	cfg := mcp.Config{
		BaseURL:   "https://attune.example.com",
		JWTSecret: []byte("test-secret-key-for-jwt-signing-32b"),
		JWTIssuer: "https://attune.example.com/mcp/oauth",
	}
	stores := mcp.Stores{
		Clients:          ptrext.Of(mockClientStore{}),
		Codes:            ptrext.Of(mockCodeStore{}),
		Tokens:           ptrext.Of(mockTokenStore{}),
		Sessions:         ptrext.Of(mockSessionStore{}),
		SessionValidator: ptrext.Of(mockSessionValidator{}),
	}
	deps := ptrext.Of(tools.Deps{
		Feedback: ptrext.Of(mockFeedbackReader{}),
	})
	return mcp.NewHandler(cfg, stores, deps)
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

func TestHandler_Unauthorized(t *testing.T) {
	h := newTestHandler()
	router := h.Routes()

	body := `{"jsonrpc":"2.0","method":"list_feedback","id":"1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_AuthenticatedRequest(t *testing.T) {
	h := newTestHandler()
	router := h.Routes()

	claims := oauth.AccessTokenClaims{
		TenantID:  "tenant-123",
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    []string{"mcp:read"},
	}
	token, err := h.Signer().Sign(claims, time.Hour)
	require.NoError(t, err)

	body := `{"jsonrpc":"2.0","method":"list_feedback","id":"1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp jsonrpc.Response
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)
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

func TestHandler_RateLimitHeaders(t *testing.T) {
	h := newTestHandler()
	router := h.Routes()

	claims := oauth.AccessTokenClaims{
		TenantID:  "tenant-123",
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    []string{"mcp:read"},
	}
	token, err := h.Signer().Sign(claims, time.Hour)
	require.NoError(t, err)

	body := `{"jsonrpc":"2.0","method":"list_feedback","id":"1"}`
	req := httptest.NewRequest(http.MethodPost, "/v1", bytes.NewBufferString(body))
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
