// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/server"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

const (
	testJWTSecret = "test-secret-key-for-jwt-signing-32b"
	testIssuer    = "https://attune.example.com/mcp/oauth"
)

func newTestSigner() *oauth.JWTSigner { return oauth.NewJWTSigner([]byte(testJWTSecret), testIssuer) }

func newTestMiddleware() *server.AuthMiddleware {
	return server.NewAuthMiddleware(newTestSigner(), nil, nil)
}

func testFailingHandler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not reach handler") })
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	signer := newTestSigner()
	middleware := server.NewAuthMiddleware(signer, nil, nil)
	claims := oauth.AccessTokenClaims{TenantID: "tenant-123", ClientID: uuid.New(), SessionID: uuid.New(), Scopes: []string{"mcp:read"}}
	token, err := signer.Sign(claims, time.Hour)
	assert.NoError(t, err)

	var capturedClaims *oauth.AccessTokenClaims
	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedClaims = server.ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotNil(t, capturedClaims)
	assert.Equal(t, "tenant-123", capturedClaims.TenantID)
}

func TestAuthMiddleware_Unauthorized(t *testing.T) {
	handler := newTestMiddleware().Wrap(testFailingHandler(t))
	tests := []struct {
		name, auth, wwwAuth string
	}{
		{"missing_header", "", "Bearer"},
		{"invalid_header", "Basic dXNlcjpwYXNz", ""},
		{"invalid_token", "Bearer invalid-token", "invalid_token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			if tt.wwwAuth != "" {
				assert.Contains(t, rec.Header().Get("WWW-Authenticate"), tt.wwwAuth)
			}
		})
	}
}

func TestRequireScope(t *testing.T) {
	claims := ptrext.Of(oauth.AccessTokenClaims{Scopes: []string{"mcp:read"}})
	ctx := server.WithClaims(httptest.NewRequest(http.MethodGet, "/", nil).Context(), claims)

	tests := []struct {
		name   string
		scope  string
		expect int
	}{
		{"has_scope", "mcp:read", http.StatusOK},
		{"missing_scope", "mcp:write", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := server.RequireScope(tt.scope)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assert.Equal(t, tt.expect, rec.Code)
		})
	}
}

type mockClientValidator struct {
	revoked bool
}

func (m *mockClientValidator) IsRevoked(_ context.Context, _ uuid.UUID) (bool, error) {
	return m.revoked, nil
}

type mockSessionValidator struct {
	active bool
}

func (m *mockSessionValidator) IsActive(_ context.Context, _ uuid.UUID) (bool, error) {
	return m.active, nil
}

func testValidToken(t *testing.T) string {
	signer := newTestSigner()
	token, err := signer.Sign(oauth.AccessTokenClaims{
		TenantID: "tenant-123", ClientID: uuid.New(), SessionID: uuid.New(), Scopes: []string{"mcp:read"},
	}, time.Hour)
	assert.NoError(t, err)
	return token
}

func TestAuthMiddleware_RevokedOrClosed(t *testing.T) {
	signer := newTestSigner()
	tests := []struct {
		name       string
		middleware *server.AuthMiddleware
	}{
		{"revoked_client", server.NewAuthMiddleware(signer, ptrext.Of(mockClientValidator{revoked: true}), nil)},
		{"closed_session", server.NewAuthMiddleware(signer, nil, ptrext.Of(mockSessionValidator{active: false}))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+testValidToken(t))
			rec := httptest.NewRecorder()
			tt.middleware.Wrap(testFailingHandler(t)).ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Contains(t, rec.Body.String(), "invalid or expired token")
		})
	}
}
