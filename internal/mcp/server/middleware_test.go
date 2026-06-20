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

func TestAuthMiddleware_ValidToken(t *testing.T) {
	secret := []byte("test-secret-key-for-jwt-signing-32b")
	issuer := "https://attune.example.com/mcp/oauth"

	signer := oauth.NewJWTSigner(secret, issuer)
	middleware := server.NewAuthMiddleware(signer, nil, nil)

	claims := oauth.AccessTokenClaims{
		TenantID:  "tenant-123",
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    []string{"mcp:read"},
	}

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

func TestAuthMiddleware_MissingHeader(t *testing.T) {
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "issuer")
	middleware := server.NewAuthMiddleware(signer, nil, nil)

	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "Bearer", rec.Header().Get("WWW-Authenticate"))
}

func TestAuthMiddleware_InvalidHeader(t *testing.T) {
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "issuer")
	middleware := server.NewAuthMiddleware(signer, nil, nil)

	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "issuer")
	middleware := server.NewAuthMiddleware(signer, nil, nil)

	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Header().Get("WWW-Authenticate"), "invalid_token")
}

func TestRequireScope_HasScope(t *testing.T) {
	claims := ptrext.Of(oauth.AccessTokenClaims{Scopes: []string{"mcp:read"}})
	ctx := server.WithClaims(httptest.NewRequest(http.MethodGet, "/", nil).Context(), claims)

	handler := server.RequireScope("mcp:read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireScope_MissingScope(t *testing.T) {
	claims := ptrext.Of(oauth.AccessTokenClaims{Scopes: []string{"mcp:read"}})
	ctx := server.WithClaims(httptest.NewRequest(http.MethodGet, "/", nil).Context(), claims)

	handler := server.RequireScope("mcp:write")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
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

func TestAuthMiddleware_RevokedClient(t *testing.T) {
	secret := []byte("test-secret-key-for-jwt-signing-32b")
	issuer := "https://attune.example.com/mcp/oauth"
	signer := oauth.NewJWTSigner(secret, issuer)
	middleware := server.NewAuthMiddleware(signer, ptrext.Of(mockClientValidator{revoked: true}), nil)

	token, err := signer.Sign(oauth.AccessTokenClaims{
		TenantID:  "tenant-123",
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    []string{"mcp:read"},
	}, time.Hour)
	assert.NoError(t, err)

	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid or expired token")
}

func TestAuthMiddleware_ClosedSession(t *testing.T) {
	secret := []byte("test-secret-key-for-jwt-signing-32b")
	issuer := "https://attune.example.com/mcp/oauth"
	signer := oauth.NewJWTSigner(secret, issuer)
	middleware := server.NewAuthMiddleware(signer, nil, ptrext.Of(mockSessionValidator{active: false}))

	token, err := signer.Sign(oauth.AccessTokenClaims{
		TenantID:  "tenant-123",
		ClientID:  uuid.New(),
		SessionID: uuid.New(),
		Scopes:    []string{"mcp:read"},
	}, time.Hour)
	assert.NoError(t, err)

	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid or expired token")
}
