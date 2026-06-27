// SPDX-License-Identifier: Apache-2.0

package oauth_test

import (
	"context"
	"encoding/json"
	"errors"
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

// errSentinel is a reusable error for mock store failures.
var errSentinel = errors.New("mock store error")

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

func (m *mockTokenStore) get(hash string) (*oauth.RefreshToken, error) {
	if t, ok := m.tokens[hash]; ok {
		return t, nil
	}
	return nil, oauth.ErrInvalidRefreshToken
}

func (m *mockTokenStore) Create(_ context.Context, token *oauth.RefreshToken) error {
	m.tokens[token.TokenHash] = token
	return nil
}

func (m *mockTokenStore) GetByHash(_ context.Context, hash string) (*oauth.RefreshToken, error) {
	return m.get(hash)
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
	t, err := m.get(hash)
	if err != nil {
		return nil, err
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

// testServer bundles common test fixtures for OAuth tests.
type testServer struct {
	server       *oauth.AuthServer
	clientID     uuid.UUID
	client       *oauth.Client
	codeStore    *mockCodeStore
	tokenStore   *mockTokenStore
	sessionStore *mockSessionStore
}

// newTestServer creates a test server with standard fixtures.
func newTestServer(t *testing.T, clientScopes []string) *testServer {
	t.Helper()
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		Name:         "Test Client",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       clientScopes,
	})
	codeStore := newMockCodeStore()
	tokenStore := newMockTokenStore()
	sessionStore := newMockSessionStore()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		codeStore, tokenStore, sessionStore, signer,
		oauth.AuthServerConfig{BaseURL: "https://attune.example.com"},
	)
	return ptrext.Of(testServer{server: server, clientID: clientID, client: client, codeStore: codeStore, tokenStore: tokenStore, sessionStore: sessionStore})
}

// authorize performs an authorization request with standard PKCE.
func (ts *testServer) authorize(t *testing.T, scope string, verifier string) *oauth.AuthorizeResponse {
	t.Helper()
	challenge := oauth.GenerateCodeChallenge(verifier)
	resp, err := ts.server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            ts.clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		Scope:               scope,
		State:               "test-state",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)
	return resp
}

// token exchanges an auth code for tokens.
func (ts *testServer) token(t *testing.T, code, verifier string) *oauth.TokenResponse {
	t.Helper()
	resp, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     ts.clientID.String(),
		CodeVerifier: verifier,
	})
	require.NoError(t, err)
	return resp
}

const testCodeVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

func TestAuthServer_Authorize(t *testing.T) {
	ts := newTestServer(t, []string{"mcp:read", "mcp:write"})
	resp := ts.authorize(t, "mcp:read", testCodeVerifier)
	assert.NotEmpty(t, resp.Code)
	assert.Equal(t, "test-issuer", resp.Issuer)
	assert.Equal(t, "https://example.com/callback", resp.RedirectURI)
}

func TestServeAuthorize_RedirectIncludesIssuer(t *testing.T) {
	ts := newTestServer(t, []string{"mcp:read"})
	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)
	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {ts.clientID.String()},
		"redirect_uri":          {"https://example.com/callback"},
		"response_type":         {"code"},
		"scope":                 {"mcp:read"},
		"state":                 {"test-state"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode(), nil)
	rec := httptest.NewRecorder()

	ts.server.ServeAuthorize(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	location, err := rec.Result().Location()
	require.NoError(t, err)
	assert.Equal(t, "test-issuer", location.Query().Get("iss"))
	assert.Equal(t, "test-state", location.Query().Get("state"))
	assert.NotEmpty(t, location.Query().Get("code"))
}

func TestAuthServer_Authorize_PKCERequired(t *testing.T) {
	ts := newTestServer(t, []string{"mcp:read"})
	_, err := ts.server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:     ts.clientID.String(),
		RedirectURI:  "https://example.com/callback",
		ResponseType: "code",
	})
	assert.ErrorIs(t, err, oauth.ErrPKCERequired)
}

func TestAuthServer_Authorize_InvalidTarget(t *testing.T) {
	ts := newTestServer(t, []string{"mcp:read"})
	_, err := ts.server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            ts.clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		Scope:               "mcp:read",
		Resource:            "https://evil.example/mcp",
		CodeChallenge:       oauth.GenerateCodeChallenge(testCodeVerifier),
		CodeChallengeMethod: "S256",
	})
	assert.ErrorIs(t, err, oauth.ErrInvalidTarget)
}

func TestAuthServer_Token_AuthorizationCode(t *testing.T) {
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)
	tokenResp := ts.token(t, authResp.Code, testCodeVerifier)
	assert.NotEmpty(t, tokenResp.AccessToken)
	assert.Equal(t, "Bearer", tokenResp.TokenType)
	assert.NotEmpty(t, tokenResp.RefreshToken)
}

func TestAuthServer_Token_RefreshToken(t *testing.T) {
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)
	tokenResp := ts.token(t, authResp.Code, testCodeVerifier)

	refreshResp, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType: "refresh_token", RefreshToken: tokenResp.RefreshToken, ClientID: ts.clientID.String(),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, refreshResp.AccessToken)
	assert.NotEmpty(t, refreshResp.RefreshToken, "rotated refresh token must be returned")
	assert.NotEqual(t, tokenResp.RefreshToken, refreshResp.RefreshToken, "refresh token must be rotated")

	_, err = ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType: "refresh_token", RefreshToken: tokenResp.RefreshToken, ClientID: ts.clientID.String(),
	})
	assert.ErrorIs(t, err, oauth.ErrInvalidRefreshToken)
}

func TestAuthServer_Token_InvalidTarget(t *testing.T) {
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)

	_, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         authResp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     ts.clientID.String(),
		CodeVerifier: testCodeVerifier,
		Resource:     "https://evil.example/mcp",
	})
	assert.ErrorIs(t, err, oauth.ErrInvalidTarget)
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
	ts := newTestServer(t, []string{"mcp:read"})
	challenge := oauth.GenerateCodeChallenge("test-verifier")
	_, err := ts.server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID: ts.clientID.String(), RedirectURI: "https://example.com/callback", ResponseType: "code",
		Scope: "mcp:read mcp:write mcp:ingest", CodeChallenge: challenge, CodeChallengeMethod: "S256",
	})
	assert.ErrorIs(t, err, oauth.ErrInvalidScope)
}

func TestAuthServer_Authorize_ScopeSubsetAllowed(t *testing.T) {
	ts := newTestServer(t, []string{"mcp:read", "mcp:write", "mcp:ingest"})
	resp := ts.authorize(t, "mcp:read", "test-verifier")
	assert.NotEmpty(t, resp.Code)
}

func TestAuthServer_Token_ClientIDMismatch(t *testing.T) {
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)
	wrongClientID := uuid.New()
	_, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType: "authorization_code", Code: authResp.Code, RedirectURI: "https://example.com/callback",
		ClientID: wrongClientID.String(), CodeVerifier: testCodeVerifier,
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

func TestAuthServer_Token_RefreshClientIDValidation(t *testing.T) {
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)
	tokenResp := ts.token(t, authResp.Code, testCodeVerifier)
	for _, clientID := range []string{"", uuid.New().String()} {
		_, err := ts.server.Token(context.Background(), oauth.TokenRequest{
			GrantType: "refresh_token", RefreshToken: tokenResp.RefreshToken, ClientID: clientID,
		})
		assert.ErrorIs(t, err, oauth.ErrInvalidClient)
	}
}

func TestServeToken_CacheControlNoStore(t *testing.T) {
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)

	tests := []struct {
		name, code, verifier string
		expect               int
	}{
		{"success", authResp.Code, testCodeVerifier, http.StatusOK},
		{"error", "invalid-code", "test-verifier", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := url.Values{
				"grant_type": {"authorization_code"}, "code": {tt.code},
				"redirect_uri": {"https://example.com/callback"}, "client_id": {ts.clientID.String()}, "code_verifier": {tt.verifier},
			}
			req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			ts.server.ServeToken(rec, req)
			assert.Equal(t, tt.expect, rec.Code)
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		})
	}
}

func TestAuthServer_Authorize_InputLengthValidation(t *testing.T) {
	ts := newTestServer(t, []string{"mcp:read"})
	clientID := ts.clientID.String()

	tests := []struct {
		name  string
		req   oauth.AuthorizeRequest
		errIs error
	}{
		{
			name: "state too long",
			req: oauth.AuthorizeRequest{
				ClientID: clientID, RedirectURI: "https://example.com/callback",
				State: strings.Repeat("a", 257), CodeChallenge: "valid", CodeChallengeMethod: "S256",
			},
			errIs: oauth.ErrInvalidRequest,
		},
		{
			name: "scope too long",
			req: oauth.AuthorizeRequest{
				ClientID: clientID, RedirectURI: "https://example.com/callback",
				Scope: strings.Repeat("a", 1025), CodeChallenge: "valid", CodeChallengeMethod: "S256",
			},
			errIs: oauth.ErrInvalidRequest,
		},
		{
			name: "code_challenge too long",
			req: oauth.AuthorizeRequest{
				ClientID: clientID, RedirectURI: "https://example.com/callback",
				CodeChallenge: strings.Repeat("a", 129), CodeChallengeMethod: "S256",
			},
			errIs: oauth.ErrInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ts.server.Authorize(context.Background(), tt.req)
			assert.ErrorIs(t, err, tt.errIs)
		})
	}
}

// --- Error-returning mock stores for coverage of error paths ---

// failCodeStore returns an error from Create.
type failCodeStore struct {
	mockCodeStore
	createErr error
}

func (m *failCodeStore) Create(_ context.Context, _ *oauth.AuthCode) error {
	return m.createErr
}

// failSessionStore returns errors from Create or IsActive, and can mark sessions inactive.
type failSessionStore struct {
	sessions    map[uuid.UUID]*oauth.Session
	createErr   error
	touchErr    error
	isActiveErr error
	allInactive bool
}

func newFailSessionStore() *failSessionStore {
	return ptrext.Of(failSessionStore{sessions: make(map[uuid.UUID]*oauth.Session)})
}

func (m *failSessionStore) Create(_ context.Context, session *oauth.Session) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.sessions[session.ID] = session
	return nil
}

func (m *failSessionStore) Touch(_ context.Context, id uuid.UUID) error {
	if m.touchErr != nil {
		return m.touchErr
	}
	if s, ok := m.sessions[id]; ok {
		s.LastUsed = time.Now()
	}
	return nil
}

func (m *failSessionStore) IsActive(_ context.Context, _ uuid.UUID) (bool, error) {
	if m.isActiveErr != nil {
		return false, m.isActiveErr
	}
	if m.allInactive {
		return false, nil
	}
	return true, nil
}

// failTokenStore returns errors from Create or RotateToken.
type failTokenStore struct {
	tokens    map[string]*oauth.RefreshToken
	createErr error
	rotateErr error
}

func newFailTokenStore() *failTokenStore {
	return ptrext.Of(failTokenStore{tokens: make(map[string]*oauth.RefreshToken)})
}

func (m *failTokenStore) Create(_ context.Context, token *oauth.RefreshToken) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.tokens[token.TokenHash] = token
	return nil
}

func (m *failTokenStore) GetByHash(_ context.Context, hash string) (*oauth.RefreshToken, error) {
	if t, ok := m.tokens[hash]; ok {
		return t, nil
	}
	return nil, oauth.ErrInvalidRefreshToken
}

func (m *failTokenStore) Revoke(_ context.Context, id uuid.UUID) error {
	for hash, token := range m.tokens {
		if token.ID == id {
			delete(m.tokens, hash)
			return nil
		}
	}
	return nil
}

func (m *failTokenStore) Consume(_ context.Context, hash string) (*oauth.RefreshToken, error) {
	t, ok := m.tokens[hash]
	if !ok {
		return nil, oauth.ErrInvalidRefreshToken
	}
	delete(m.tokens, hash)
	return t, nil
}

func (m *failTokenStore) RotateToken(_ context.Context, oldHash, newHash string, newExpiresAt time.Time) (*oauth.RefreshToken, *oauth.RefreshToken, error) {
	if m.rotateErr != nil {
		return nil, nil, m.rotateErr
	}
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

// --- Test: validateAuthRequest branches ---

func TestValidateAuthRequest_UnsupportedResponseType(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)

	_, err := ts.server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            ts.clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "token",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported response_type")
}

func TestValidateAuthRequest_WrongChallengeMethod(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})

	_, err := ts.server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            ts.clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		CodeChallenge:       "some-challenge",
		CodeChallengeMethod: "plain",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "code_challenge_method must be S256")
}

func TestValidateAuthRequest_FieldLengthLimits(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})

	tests := []struct {
		name string
		req  oauth.AuthorizeRequest
	}{
		{
			name: "redirect_uri too long",
			req: oauth.AuthorizeRequest{
				ClientID:    ts.clientID.String(),
				RedirectURI: strings.Repeat("x", 2049),
			},
		},
		{
			name: "resource too long",
			req: oauth.AuthorizeRequest{
				ClientID:    ts.clientID.String(),
				RedirectURI: "https://example.com/callback",
				Resource:    strings.Repeat("r", 2049),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No t.Parallel() here: subtests share the test server fixture.
			_, err := ts.server.Authorize(context.Background(), tt.req)
			require.ErrorIs(t, err, oauth.ErrInvalidRequest)
		})
	}
}

// --- Test: Authorize error paths ---

func TestAuthorize_InvalidClientIDFormat(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})

	_, err := ts.server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            "not-a-uuid",
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		CodeChallenge:       oauth.GenerateCodeChallenge(testCodeVerifier),
		CodeChallengeMethod: "S256",
	})
	require.ErrorIs(t, err, oauth.ErrInvalidClient)
}

func TestAuthorize_ClientNotFound(t *testing.T) {
	t.Parallel()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: nil, valid: false}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	_, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            uuid.New().String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		CodeChallenge:       oauth.GenerateCodeChallenge(testCodeVerifier),
		CodeChallengeMethod: "S256",
	})
	require.ErrorIs(t, err, oauth.ErrInvalidClient)
}

func TestAuthorize_EmptyScopeUsesClientDefaults(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read", "mcp:write"})
	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)

	resp, err := ts.server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            ts.clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		Scope:               "", // empty scope
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Code)

	// Exchange the code to verify client scopes were used
	tokenResp := ts.token(t, resp.Code, testCodeVerifier)
	require.Equal(t, "mcp:read mcp:write", tokenResp.Scope)
}

func TestAuthorize_CodeCreateFails(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		ptrext.Of(failCodeStore{createErr: errSentinel}),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	_, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		Scope:               "mcp:read",
		CodeChallenge:       oauth.GenerateCodeChallenge(testCodeVerifier),
		CodeChallengeMethod: "S256",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "authorization failed")
}

func TestAuthorize_ValidResource(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)

	resp, err := ts.server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            ts.clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		Scope:               "mcp:read",
		Resource:            "https://attune.example.com/mcp/v1",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Code)
}

// --- Test: tokenFromCode error paths ---

func TestTokenFromCode_InvalidCodeNotFound(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})

	_, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         "nonexistent-code",
		RedirectURI:  "https://example.com/callback",
		ClientID:     ts.clientID.String(),
		CodeVerifier: testCodeVerifier,
	})
	require.ErrorIs(t, err, oauth.ErrInvalidCode)
}

func TestTokenFromCode_InvalidClientIDFormat(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)

	_, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         authResp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     "not-a-uuid",
		CodeVerifier: testCodeVerifier,
	})
	require.ErrorIs(t, err, oauth.ErrInvalidClient)
}

func TestTokenFromCode_RedirectURIMismatch(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)

	_, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         authResp.Code,
		RedirectURI:  "https://evil.com/callback",
		ClientID:     ts.clientID.String(),
		CodeVerifier: testCodeVerifier,
	})
	require.ErrorIs(t, err, oauth.ErrInvalidRedirectURI)
}

func TestTokenFromCode_PKCEVerificationFails(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)

	_, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         authResp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     ts.clientID.String(),
		CodeVerifier: "wrong-verifier-that-is-at-least-43-characters-long-for-pkce",
	})
	require.ErrorIs(t, err, oauth.ErrPKCEFailed)
}

func TestTokenFromCode_InvalidResource(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)

	_, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         authResp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     ts.clientID.String(),
		CodeVerifier: testCodeVerifier,
		Resource:     "https://evil.example/mcp",
	})
	require.ErrorIs(t, err, oauth.ErrInvalidTarget)
}

func TestTokenFromCode_SessionCreateFails(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})
	codeStore := newMockCodeStore()
	sessionStore := newFailSessionStore()
	sessionStore.createErr = errSentinel
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		codeStore,
		newMockTokenStore(),
		sessionStore,
		signer,
		oauth.AuthServerConfig{},
	)

	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)
	resp, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		Scope:               "mcp:read",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)

	_, err = server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         resp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     clientID.String(),
		CodeVerifier: testCodeVerifier,
	})
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)
}

func TestTokenFromCode_RefreshTokenCreateFails(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})
	codeStore := newMockCodeStore()
	tokenStore := newFailTokenStore()
	tokenStore.createErr = errSentinel
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		codeStore,
		tokenStore,
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)
	resp, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		Scope:               "mcp:read",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)

	_, err = server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         resp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     clientID.String(),
		CodeVerifier: testCodeVerifier,
	})
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)
}

// --- Test: tokenFromRefresh error paths ---

func TestTokenFromRefresh_MissingClientID(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)
	tokenResp := ts.token(t, authResp.Code, testCodeVerifier)

	_, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     "",
	})
	require.ErrorIs(t, err, oauth.ErrInvalidClient)
}

func TestTokenFromRefresh_InvalidClientIDFormat(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)
	tokenResp := ts.token(t, authResp.Code, testCodeVerifier)

	_, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     "not-a-uuid",
	})
	require.ErrorIs(t, err, oauth.ErrInvalidClient)
}

func TestTokenFromRefresh_TokenNotFound(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})

	_, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: "nonexistent-refresh-token",
		ClientID:     ts.clientID.String(),
	})
	require.ErrorIs(t, err, oauth.ErrInvalidRefreshToken)
}

func TestTokenFromRefresh_ClientIDMismatch(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)
	tokenResp := ts.token(t, authResp.Code, testCodeVerifier)

	otherID := uuid.New()
	_, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     otherID.String(),
	})
	require.ErrorIs(t, err, oauth.ErrInvalidClient)
}

func TestTokenFromRefresh_InvalidResource(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)
	tokenResp := ts.token(t, authResp.Code, testCodeVerifier)

	_, err := ts.server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     ts.clientID.String(),
		Resource:     "https://evil.example/mcp",
	})
	require.ErrorIs(t, err, oauth.ErrInvalidTarget)
}

func TestTokenFromRefresh_SessionNotActive(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})
	codeStore := newMockCodeStore()
	tokenStore := newMockTokenStore()
	sessionStore := newFailSessionStore()
	sessionStore.allInactive = true
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		codeStore, tokenStore, sessionStore, signer,
		oauth.AuthServerConfig{},
	)

	// Authorize and get tokens (session create still works since createErr is nil)
	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)
	authResp, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		Scope:               "mcp:read",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)

	tokenResp, err := server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         authResp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     clientID.String(),
		CodeVerifier: testCodeVerifier,
	})
	require.NoError(t, err)

	// Now try refresh - session is marked as inactive
	_, err = server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     clientID.String(),
	})
	require.ErrorIs(t, err, oauth.ErrInvalidRefreshToken)
}

func TestTokenFromRefresh_RotateTokenFails(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})
	codeStore := newMockCodeStore()
	tokenStore := newFailTokenStore()
	tokenStore.rotateErr = errSentinel
	sessionStore := newMockSessionStore()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		codeStore, tokenStore, sessionStore, signer,
		oauth.AuthServerConfig{},
	)

	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)
	authResp, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		Scope:               "mcp:read",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)

	tokenResp, err := server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         authResp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     clientID.String(),
		CodeVerifier: testCodeVerifier,
	})
	require.NoError(t, err)

	// Refresh should fail because RotateToken returns an error
	_, err = server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     clientID.String(),
	})
	require.ErrorIs(t, err, oauth.ErrInvalidRefreshToken)
}

func TestTokenFromRefresh_SessionTouchFailsNonFatal(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})
	codeStore := newMockCodeStore()
	tokenStore := newMockTokenStore()
	sessionStore := newFailSessionStore()
	sessionStore.touchErr = errSentinel
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		codeStore, tokenStore, sessionStore, signer,
		oauth.AuthServerConfig{},
	)

	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)
	authResp, err := server.Authorize(context.Background(), oauth.AuthorizeRequest{
		ClientID:            clientID.String(),
		RedirectURI:         "https://example.com/callback",
		ResponseType:        "code",
		Scope:               "mcp:read",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	require.NoError(t, err)

	tokenResp, err := server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "authorization_code",
		Code:         authResp.Code,
		RedirectURI:  "https://example.com/callback",
		ClientID:     clientID.String(),
		CodeVerifier: testCodeVerifier,
	})
	require.NoError(t, err)

	// Refresh should succeed even though Touch fails (non-fatal)
	refreshResp, err := server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: tokenResp.RefreshToken,
		ClientID:     clientID.String(),
	})
	require.NoError(t, err)
	require.NotEmpty(t, refreshResp.AccessToken)
	require.NotEmpty(t, refreshResp.RefreshToken)
}

// --- Test: ServeAuthorize HTTP handler ---

func TestServeAuthorize_InvalidClient(t *testing.T) {
	t.Parallel()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: nil, valid: false}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {"not-a-uuid"},
		"redirect_uri":          {"https://example.com/callback"},
		"response_type":         {"code"},
		"code_challenge":        {"some-challenge"},
		"code_challenge_method": {"S256"},
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	server.ServeAuthorize(rec, req)

	// Invalid client must NOT redirect (open redirect prevention)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServeAuthorize_InvalidRedirectURI(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: false}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {clientID.String()},
		"redirect_uri":          {"https://evil.com/steal"},
		"response_type":         {"code"},
		"code_challenge":        {"some-challenge"},
		"code_challenge_method": {"S256"},
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	server.ServeAuthorize(rec, req)

	// Invalid redirect_uri must NOT redirect
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServeAuthorize_ErrorRedirectsAfterValidation(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})

	// PKCE required error - client and redirect_uri are valid, so redirect with error
	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":     {ts.clientID.String()},
		"redirect_uri":  {"https://example.com/callback"},
		"response_type": {"code"},
		// Missing code_challenge triggers ErrPKCERequired
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	ts.server.ServeAuthorize(rec, req)

	// After validation passes, PKCE error should redirect
	require.Equal(t, http.StatusFound, rec.Code)
	location, err := rec.Result().Location()
	require.NoError(t, err)
	require.Equal(t, "invalid_request", location.Query().Get("error"))
	require.Contains(t, location.Query().Get("error_description"), "PKCE")
}

func TestServeAuthorize_ScopeEscalationRedirects(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)

	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {ts.clientID.String()},
		"redirect_uri":          {"https://example.com/callback"},
		"response_type":         {"code"},
		"scope":                 {"mcp:read mcp:admin"},
		"state":                 {"my-state"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	ts.server.ServeAuthorize(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	location, err := rec.Result().Location()
	require.NoError(t, err)
	require.Equal(t, "invalid_scope", location.Query().Get("error"))
	require.Equal(t, "my-state", location.Query().Get("state"))
}

func TestServeAuthorize_InvalidTargetRedirects(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)

	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {ts.clientID.String()},
		"redirect_uri":          {"https://example.com/callback"},
		"response_type":         {"code"},
		"scope":                 {"mcp:read"},
		"resource":              {"https://evil.example/mcp"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	ts.server.ServeAuthorize(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	location, err := rec.Result().Location()
	require.NoError(t, err)
	require.Equal(t, "invalid_target", location.Query().Get("error"))
}

// --- Test: ServeToken HTTP handler error branches ---

func TestServeToken_ErrorBranches(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)
	tokenResp := ts.token(t, authResp.Code, testCodeVerifier)

	tests := []struct {
		name     string
		form     url.Values
		wantCode string
	}{
		{
			name: "pkce_failed",
			form: url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {"some-code"},
				"redirect_uri":  {"https://example.com/callback"},
				"client_id":     {ts.clientID.String()},
				"code_verifier": {"wrong-verifier-that-is-at-least-43-characters-long-value"},
			},
			wantCode: "invalid_grant",
		},
		{
			name: "invalid_client",
			form: url.Values{
				"grant_type":    {"refresh_token"},
				"refresh_token": {tokenResp.RefreshToken},
				"client_id":     {"not-a-uuid"},
			},
			wantCode: "invalid_client",
		},
		{
			name: "invalid_refresh_token",
			form: url.Values{
				"grant_type":    {"refresh_token"},
				"refresh_token": {"nonexistent"},
				"client_id":     {ts.clientID.String()},
			},
			wantCode: "invalid_grant",
		},
		{
			name: "invalid_scope",
			form: url.Values{
				"grant_type":    {"authorization_code"},
				"code":          {"dummy"},
				"redirect_uri":  {"https://example.com/callback"},
				"client_id":     {ts.clientID.String()},
				"code_verifier": {testCodeVerifier},
			},
			wantCode: "invalid_grant",
		},
		{
			name: "invalid_target",
			form: url.Values{
				"grant_type":    {"refresh_token"},
				"refresh_token": {"nonexistent"},
				"client_id":     {ts.clientID.String()},
				"resource":      {"https://evil.example/mcp"},
			},
			wantCode: "invalid_grant",
		},
		{
			name: "unsupported_grant_type",
			form: url.Values{
				"grant_type": {"password"},
			},
			wantCode: "invalid_grant",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No t.Parallel() here: subtests share the mutable token/code store maps.
			req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(tt.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			ts.server.ServeToken(rec, req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

			var body map[string]string
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
			require.Equal(t, tt.wantCode, body["error"])
		})
	}
}

func TestServeToken_InvalidScopeError(t *testing.T) {
	t.Parallel()
	// Build a server where authorize will fail with ErrInvalidScope,
	// then test that ServeToken maps it correctly.
	// We need an actual code exchange that triggers ErrInvalidScope,
	// but that's only on refresh. Instead, test the mapping via Token directly.
	ts := newTestServer(t, []string{"mcp:read"})

	// Create a custom auth code with scope that won't match
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)
	tokenResp := ts.token(t, authResp.Code, testCodeVerifier)

	// Use the token but with invalid resource to trigger ErrInvalidTarget
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tokenResp.RefreshToken},
		"client_id":     {ts.clientID.String()},
		"resource":      {"https://evil.example/mcp"},
	}
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ts.server.ServeToken(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "invalid_target", body["error"])
	require.Contains(t, body["error_description"], "not served by this authorization server")
}

func TestServeToken_SuccessResponse(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authResp.Code},
		"redirect_uri":  {"https://example.com/callback"},
		"client_id":     {ts.clientID.String()},
		"code_verifier": {testCodeVerifier},
	}
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ts.server.ServeToken(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var body oauth.TokenResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.NotEmpty(t, body.AccessToken)
	require.Equal(t, "Bearer", body.TokenType)
	require.NotEmpty(t, body.RefreshToken)
	require.Equal(t, "mcp:read", body.Scope)
	require.Greater(t, body.ExpiresIn, 0)
}

// --- Test: validateResource ---

func TestValidateResource(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)

	tests := []struct {
		name     string
		resource string
		wantErr  error
	}{
		{"empty resource is valid", "", nil},
		{"whitespace-only resource is valid", "   ", nil},
		{"exact match", "https://attune.example.com/mcp/v1", nil},
		{"wrong resource", "https://other.example.com/mcp/v1", oauth.ErrInvalidTarget},
		{"partial path", "https://attune.example.com/mcp", oauth.ErrInvalidTarget},
		{"extra path", "https://attune.example.com/mcp/v1/extra", oauth.ErrInvalidTarget},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No t.Parallel() here: subtests share the mutable codeStore map.
			_, err := ts.server.Authorize(context.Background(), oauth.AuthorizeRequest{
				ClientID:            ts.clientID.String(),
				RedirectURI:         "https://example.com/callback",
				ResponseType:        "code",
				Scope:               "mcp:read",
				Resource:            tt.resource,
				CodeChallenge:       challenge,
				CodeChallengeMethod: "S256",
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// --- Test: NewAuthServer defaults ---

func TestNewAuthServer_Defaults(t *testing.T) {
	t.Parallel()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")

	// Create with zero config to verify defaults are applied
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)
	require.NotNil(t, server)
}

func TestNewAuthServer_CustomTTLs(t *testing.T) {
	t.Parallel()
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")

	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{
			CodeLifetime: 5 * time.Minute,
			AccessTTL:    30 * time.Minute,
			RefreshTTL:   24 * time.Hour,
		},
	)
	require.NotNil(t, server)
}

// --- Test: redirectWithCode covers issuer and state absence ---

func TestServeAuthorize_NoState(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)

	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {ts.clientID.String()},
		"redirect_uri":          {"https://example.com/callback"},
		"response_type":         {"code"},
		"scope":                 {"mcp:read"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		// No state parameter
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	ts.server.ServeAuthorize(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	location, err := rec.Result().Location()
	require.NoError(t, err)
	require.NotEmpty(t, location.Query().Get("code"))
	require.Empty(t, location.Query().Get("state"), "state should be absent when not provided")
}

// --- Test: ServeToken with refresh_token grant via HTTP ---

func TestServeAuthorize_CodeCreateFailRedirects(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: true}),
		ptrext.Of(failCodeStore{createErr: errSentinel}),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)
	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)

	// The "authorization failed" error from code create should hit the default
	// case in mapAuthError ("access_denied") and redirect with it.
	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {clientID.String()},
		"redirect_uri":          {"https://example.com/callback"},
		"response_type":         {"code"},
		"scope":                 {"mcp:read"},
		"state":                 {"keep-me"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	server.ServeAuthorize(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	location, err := rec.Result().Location()
	require.NoError(t, err)
	require.Equal(t, "access_denied", location.Query().Get("error"))
	require.Equal(t, "keep-me", location.Query().Get("state"))
}

func TestServeAuthorize_UnsupportedResponseTypeRedirects(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	challenge := oauth.GenerateCodeChallenge(testCodeVerifier)

	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {ts.clientID.String()},
		"redirect_uri":          {"https://example.com/callback"},
		"response_type":         {"token"},
		"scope":                 {"mcp:read"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	ts.server.ServeAuthorize(rec, req)

	// unsupported response_type is not one of the non-redirect errors,
	// so it redirects with the error
	require.Equal(t, http.StatusFound, rec.Code)
	location, err := rec.Result().Location()
	require.NoError(t, err)
	require.NotEmpty(t, location.Query().Get("error"))
}

func TestServeAuthorize_NoStateInErrorRedirect(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})

	// Trigger PKCE error without state parameter
	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":     {ts.clientID.String()},
		"redirect_uri":  {"https://example.com/callback"},
		"response_type": {"code"},
		// No state, no code_challenge -> ErrPKCERequired
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	ts.server.ServeAuthorize(rec, req)

	require.Equal(t, http.StatusFound, rec.Code)
	location, err := rec.Result().Location()
	require.NoError(t, err)
	require.Empty(t, location.Query().Get("state"), "state should not appear in redirect if not provided")
	require.NotEmpty(t, location.Query().Get("error"))
}

func TestServeToken_PKCEFailedError(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authResp.Code},
		"redirect_uri":  {"https://example.com/callback"},
		"client_id":     {ts.clientID.String()},
		"code_verifier": {"wrong-verifier-that-is-at-least-43-characters-long-value"},
	}
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ts.server.ServeToken(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "invalid_grant", body["error"])
	require.Equal(t, "PKCE verification failed", body["error_description"])
}

func TestServeToken_InvalidCodeError(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"nonexistent-code"},
		"redirect_uri":  {"https://example.com/callback"},
		"client_id":     {ts.clientID.String()},
		"code_verifier": {testCodeVerifier},
	}
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ts.server.ServeToken(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.Equal(t, "invalid_grant", body["error"])
	require.Equal(t, "invalid or expired code", body["error_description"])
}

func TestServeAuthorize_ValidClientInvalidRedirectURI(t *testing.T) {
	t.Parallel()
	// Tests validateClientAndRedirect where client exists but redirect_uri is invalid.
	// GetByID succeeds but ValidateRedirectURI returns false.
	clientID := uuid.New()
	client := ptrext.Of(oauth.Client{
		ID:           clientID,
		TenantID:     "tenant-123",
		RedirectURIs: []string{"https://example.com/callback"},
		Scopes:       []string{"mcp:read"},
	})
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: client, valid: false}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {clientID.String()},
		"redirect_uri":          {"https://evil.com/callback"},
		"response_type":         {"code"},
		"code_challenge":        {"test"},
		"code_challenge_method": {"S256"},
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	server.ServeAuthorize(rec, req)

	// Must return 400, NOT redirect (prevent open redirect)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid redirect_uri")
}

func TestServeAuthorize_ClientNotFoundHTTP(t *testing.T) {
	t.Parallel()
	// Tests validateClientAndRedirect where UUID is valid but client doesn't exist.
	signer := oauth.NewJWTSigner([]byte("test-secret-key-for-jwt-signing-32b"), "test-issuer")
	server := oauth.NewAuthServer(
		ptrext.Of(mockClientStore{client: nil, valid: false}),
		newMockCodeStore(),
		newMockTokenStore(),
		newMockSessionStore(),
		signer,
		oauth.AuthServerConfig{},
	)

	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {uuid.New().String()},
		"redirect_uri":          {"https://example.com/callback"},
		"response_type":         {"code"},
		"code_challenge":        {"test"},
		"code_challenge_method": {"S256"},
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	server.ServeAuthorize(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid client")
}

func TestServeToken_RefreshTokenSuccess(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t, []string{"mcp:read"})
	authResp := ts.authorize(t, "mcp:read", testCodeVerifier)
	tokenResp := ts.token(t, authResp.Code, testCodeVerifier)

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tokenResp.RefreshToken},
		"client_id":     {ts.clientID.String()},
	}
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ts.server.ServeToken(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body oauth.TokenResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	require.NotEmpty(t, body.AccessToken)
	require.NotEmpty(t, body.RefreshToken)
	require.NotEqual(t, tokenResp.RefreshToken, body.RefreshToken)
}
