// SPDX-License-Identifier: Apache-2.0

package oauth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type mockClientStore struct {
	client *oauth.Client
	valid  bool
}

func (m *mockClientStore) GetByID(_ context.Context, _ uuid.UUID) (*oauth.Client, error) {
	if m.client == nil {
		return nil, oauth.ErrInvalidClient
	}
	return m.client, nil
}

func (m *mockClientStore) ValidateRedirectURI(_ context.Context, _ uuid.UUID, _ string) (bool, error) {
	return m.valid, nil
}

type mockCodeStore struct {
	codes map[string]*oauth.AuthCode
}

func newMockCodeStore() *mockCodeStore {
	return ptrext.Of(mockCodeStore{codes: make(map[string]*oauth.AuthCode)})
}

func (m *mockCodeStore) Create(_ context.Context, code *oauth.AuthCode) error {
	m.codes[code.Code] = code
	return nil
}

func (m *mockCodeStore) Consume(_ context.Context, code string) (*oauth.AuthCode, error) {
	c, ok := m.codes[code]
	if !ok {
		return nil, oauth.ErrInvalidCode
	}
	delete(m.codes, code)
	return c, nil
}

type mockTokenStore struct {
	tokens map[string]*oauth.RefreshToken
}

func newMockTokenStore() *mockTokenStore {
	return ptrext.Of(mockTokenStore{tokens: make(map[string]*oauth.RefreshToken)})
}

func (m *mockTokenStore) Create(_ context.Context, token *oauth.RefreshToken) error {
	m.tokens[token.TokenHash] = token
	return nil
}

func (m *mockTokenStore) GetByHash(_ context.Context, hash string) (*oauth.RefreshToken, error) {
	t, ok := m.tokens[hash]
	if !ok {
		return nil, oauth.ErrInvalidRefreshToken
	}
	return t, nil
}

func (m *mockTokenStore) Revoke(_ context.Context, id uuid.UUID) error {
	for hash, token := range m.tokens {
		if token.ID == id {
			delete(m.tokens, hash)
			return nil
		}
	}
	return nil
}

func (m *mockTokenStore) Consume(_ context.Context, hash string) (*oauth.RefreshToken, error) {
	t, ok := m.tokens[hash]
	if !ok {
		return nil, oauth.ErrInvalidRefreshToken
	}
	delete(m.tokens, hash)
	return t, nil
}

func (m *mockTokenStore) RotateToken(_ context.Context, oldHash, newHash string, newExpiresAt time.Time) (*oauth.RefreshToken, *oauth.RefreshToken, error) {
	old, ok := m.tokens[oldHash]
	if !ok {
		return nil, nil, oauth.ErrInvalidRefreshToken
	}
	delete(m.tokens, oldHash)
	newToken := ptrext.Of(oauth.RefreshToken{
		ID:        uuid.New(),
		TokenHash: newHash,
		ClientID:  old.ClientID,
		TenantID:  old.TenantID,
		SessionID: old.SessionID,
		Scopes:    old.Scopes,
		ExpiresAt: newExpiresAt,
		CreatedAt: time.Now(),
	})
	m.tokens[newHash] = newToken
	return old, newToken, nil
}

type mockSessionStore struct {
	sessions map[uuid.UUID]*oauth.Session
}

func newMockSessionStore() *mockSessionStore {
	return ptrext.Of(mockSessionStore{sessions: make(map[uuid.UUID]*oauth.Session)})
}

func (m *mockSessionStore) Create(_ context.Context, session *oauth.Session) error {
	m.sessions[session.ID] = session
	return nil
}

func (m *mockSessionStore) Touch(_ context.Context, id uuid.UUID) error {
	if s, ok := m.sessions[id]; ok {
		s.LastUsed = time.Now()
	}
	return nil
}

func (m *mockSessionStore) IsActive(_ context.Context, id uuid.UUID) (bool, error) {
	_, ok := m.sessions[id]
	return ok, nil
}

func TestAuthServer_Authorize(t *testing.T) {
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		Name:         "Test Client",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read", "mcp:write"},
	})

	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{BaseURL: "https://attune.example.com"},
	)

	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	resp, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		Scope:               "mcp:read",
		State:               "xyz",
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
	})

	require.NoError(t, err)
	assert.NotEmpty(t, resp.Code)
	assert.Equal(t, "xyz", resp.State)
	assert.Equal(t, "https://example.com/callback", resp.RedirectURI)
}

func TestAuthServer_Authorize_PKCERequired(t *testing.T) {
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:       clientID,
		TenantID: "tenant-123",
	})

	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	_, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:     clientID.String(),
		RedirectURI:  "https://example.com/callback",
		ResponseType: "code",
	})

	assert.ErrorIs(t, err, oauth.ErrPKCERequired)
}

func TestAuthServer_Token_AuthorizationCode(t *testing.T) {
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})

	codeStore := newMockCodeStore()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		codeStore,
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	authResp, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)

	tokenResp, err := server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         authResp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     clientID.String(),
		CodeVerifier: codeVerifier,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, tokenResp.AccessToken)
	assert.Equal(t, "Bearer", tokenResp.TokenType)
	assert.NotEmpty(t, tokenResp.RefreshToken)
}

func TestAuthServer_Token_RefreshToken(t *testing.T) {
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})

	codeStore := newMockCodeStore()
	tokenStore := newMockTokenStore()
	sessionStore := newMockSessionStore()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		codeStore,
		tokenStore,
		sessionStore,
		signer,
		oauth.AuthServerConfig{},
	)

	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	authResp, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)

	tokenResp, err := server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         authResp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     clientID.String(),
		CodeVerifier: codeVerifier,
	})
	require.NoError(t, err)

	refreshResp, err := server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     clientID.String(),
	})

	require.NoError(t, err)
	assert.NotEmpty(t, refreshResp.AccessToken)
	assert.Equal(t, "Bearer", refreshResp.TokenType)
	assert.NotEmpty(t, refreshResp.RefreshToken, "rotated refresh token must be returned")
	assert.NotEqual(t, tokenResp.RefreshToken, refreshResp.RefreshToken, "refresh token must be rotated")

	// Old refresh token should no longer work
	_, err = server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     clientID.String(),
	})
	assert.ErrorIs(t, err, oauth.ErrInvalidRefreshToken)
}

func TestAuthServer_Token_InvalidGrant(t *testing.T) {
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	_, err := server.Token(context.Background(), oauth.TokenRequest{
		GrantType: "password",
	})

	assert.ErrorIs(t, err, oauth.ErrInvalidGrant)
}

func TestAuthServer_Token_ExpiredCode(t *testing.T) {
	clientID := uuid.New()
	codeStore := newMockCodeStore()

	codeStore.codes["expired-code"] = ptrext.Of(oauth.AuthCode{
		Code:                "expired-code",
		ClientID:            clientID,
		TenantID:            "tenant-123",
		RedirectURI:         "https://example.com/callback",
		Scopes:              []string{"mcp:read"},
		CodeChallenge:       oauth.GenerateCodeChallenge("test-verifier"),
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(-1 * time.Hour),
		CreatedAt:           time.Now().Add(-2 * time.Hour),
	})

	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{
			client: ptrext.Of(oauth.Client{
				ID:       clientID,
				TenantID: "tenant-123",
				Scopes:   []string{"mcp:read"},
			}),
			valid: true,
		}),
		codeStore,
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	_, err := server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         "expired-code",
		RedirectURI:  "https://example.com/callback",
		ClientID:     clientID.String(),
		CodeVerifier: "test-verifier",
	})

	assert.ErrorIs(t, err, oauth.ErrInvalidCode)
}

func TestAuthServer_Authorize_ScopeEscalationPrevention(t *testing.T) {
	clientID := uuid.New()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{
			client: ptrext.Of(oauth.Client{
				ID:           clientID,
				TenantID:     "tenant-123",
				RedirectURIs: []string{"https://example.com/callback"},
				Scopes:       []string{"mcp:read"},
			}),
			valid: true,
		}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	codeChallenge := oauth.GenerateCodeChallenge("test-verifier")

	_, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		Scope:               "mcp:read mcp:write mcp:ingest",
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
	})

	assert.ErrorIs(t, err, oauth.ErrInvalidScope)
}

func TestAuthServer_Authorize_ScopeSubsetAllowed(t *testing.T) {
	clientID := uuid.New()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{
			client: ptrext.Of(oauth.Client{
				ID:           clientID,
				TenantID:     "tenant-123",
				RedirectURIs: []string{"https://example.com/callback"},
				Scopes:       []string{"mcp:read", "mcp:write", "mcp:ingest"},
			}),
			valid: true,
		}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	codeChallenge := oauth.GenerateCodeChallenge("test-verifier")

	resp, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		Scope:               "mcp:read",
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
	})

	require.NoError(t, err)
	assert.NotEmpty(t, resp.Code)
}

func TestAuthServer_Token_ClientIDMismatch(t *testing.T) {
	clientID := uuid.New()
	wrongClientID := uuid.New()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	codeStore := newMockCodeStore()
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{
			client: ptrext.Of(oauth.Client{
				ID:           clientID,
				TenantID:     "tenant-123",
				RedirectURIs: []string{"https://example.com/callback"},
				Scopes:       []string{"mcp:read"},
			}),
			valid: true,
		}),
		codeStore,
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	authResp, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)

	_, err = server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         authResp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     wrongClientID.String(),
		CodeVerifier: codeVerifier,
	})

	assert.ErrorIs(t, err, oauth.ErrInvalidClient)
}

func TestAuthServer_Authorize_InvalidRedirectURINoOpenRedirect(t *testing.T) {
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})

	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: false}), // valid=false makes ValidateRedirectURI return false
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	codeChallenge := oauth.GenerateCodeChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")

	// Try with an attacker-controlled redirect URI
	_, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://evil.com/steal-token",
		ResponseType:        "code",
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
	})

	// Should return error, NOT redirect
	assert.ErrorIs(t, err, oauth.ErrInvalidRedirectURI)
}

func TestAuthServer_Token_RefreshRequiresClientID(t *testing.T) {
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})

	codeStore := newMockCodeStore()
	tokenStore := newMockTokenStore()
	sessionStore := newMockSessionStore()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		codeStore,
		tokenStore,
		sessionStore,
		signer,
		oauth.AuthServerConfig{},
	)

	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	authResp, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)

	tokenResp, err := server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         authResp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     clientID.String(),
		CodeVerifier: codeVerifier,
	})
	require.NoError(t, err)

	// Try to refresh without client_id - should fail
	_, err = server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     "", // Missing client_id
	})

	assert.ErrorIs(t, err, oauth.ErrInvalidClient)
}

func TestAuthServer_Token_RefreshClientIDMismatch(t *testing.T) {
	clientID := uuid.New()
	wrongClientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})

	codeStore := newMockCodeStore()
	tokenStore := newMockTokenStore()
	sessionStore := newMockSessionStore()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		codeStore,
		tokenStore,
		sessionStore,
		signer,
		oauth.AuthServerConfig{},
	)

	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	authResp, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)

	tokenResp, err := server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         authResp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     clientID.String(),
		CodeVerifier: codeVerifier,
	})
	require.NoError(t, err)

	// Try to refresh with wrong client_id - should fail
	_, err = server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     wrongClientID.String(),
	})

	assert.ErrorIs(t, err, oauth.ErrInvalidClient)
}

func TestServeToken_CacheControlNoStore(t *testing.T) {
	clientID := uuid.New()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	codeStore := newMockCodeStore()

	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})

	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		codeStore,
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	codeChallenge := oauth.GenerateCodeChallenge(codeVerifier)

	authResp, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", authResp.Code)
	form.Set("redirect_uri", "https://example.com/callback")
	form.Set("client_id", clientID.String())
	form.Set("code_verifier", codeVerifier)

	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	server.ServeToken(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestServeToken_ErrorResponse_CacheControlNoStore(t *testing.T) {
	clientID := uuid.New()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")

	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: nil, valid: false}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "invalid-code")
	form.Set("redirect_uri", "https://example.com/callback")
	form.Set("client_id", clientID.String())
	form.Set("code_verifier", "test-verifier")

	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	server.ServeToken(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestAuthServer_Authorize_InputLengthValidation(t *testing.T) {
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: nil, valid: false}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	tests := []struct {
		name  string
		req   oauth.AuthorizeRequest
		errIs error
	}{
		{
			name: "state too long",
			req: oauth.AuthorizeRequest{
				ClientID:      "valid-uuid-format-1234567890ab",
				RedirectURI:   "https://example.com/callback",
				State:         strings.Repeat("a", 257),
				CodeChallenge: "valid",
			},
			errIs: oauth.ErrInvalidRequest,
		},
		{
			name: "scope too long",
			req: oauth.AuthorizeRequest{
				ClientID:      "valid-uuid-format-1234567890ab",
				RedirectURI:   "https://example.com/callback",
				Scope:         strings.Repeat("a", 1025),
				CodeChallenge: "valid",
			},
			errIs: oauth.ErrInvalidRequest,
		},
		{
			name: "redirect_uri too long",
			req: oauth.AuthorizeRequest{
				ClientID:      "valid-uuid-format-1234567890ab",
				RedirectURI:   "https://example.com/" + strings.Repeat("a", 2050),
				CodeChallenge: "valid",
			},
			errIs: oauth.ErrInvalidRequest,
		},
		{
			name: "code_challenge too long",
			req: oauth.AuthorizeRequest{
				ClientID:      "valid-uuid-format-1234567890ab",
				RedirectURI:   "https://example.com/callback",
				CodeChallenge: strings.Repeat("a", 129),
			},
			errIs: oauth.ErrInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := server.Authorize(context.Background(), tt.req)
			assert.ErrorIs(t, err, tt.errIs)
		})
	}
}
