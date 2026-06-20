// SPDX-License-Identifier: Apache-2.0

package oauth_test

import (
	"context"
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

func (m *mockTokenStore) Revoke(_ context.Context, _ uuid.UUID) error {
	return nil
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

	signer := oauth.NewJWTSigner([]byte("test-secret"), "test-issuer")
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
		CodeVerifier: codeVerifier,
	})
	require.NoError(t, err)

	refreshResp, err := server.Token(context.Background(), oauth.TokenRequest{
		GrantType:    "refresh_token",
		RefreshToken: tokenResp.RefreshToken,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, refreshResp.AccessToken)
	assert.Equal(t, "Bearer", refreshResp.TokenType)
}

func TestAuthServer_Token_InvalidGrant(t *testing.T) {
	signer := oauth.NewJWTSigner([]byte("test-secret"), "test-issuer")
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
