//go:build integration

package mcp_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/Phixsura/attune/internal/handlers/mcp"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/mcp/tools"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/tenant"
	"github.com/Phixsura/attune/internal/testdb"
)

// testEnv bundles all resources needed for OAuth E2E tests.
type testEnv struct {
	pool         *pgxpool.Pool
	tenantID     string
	server       *httptest.Server
	clientsRepo  *mcprepo.ClientsRepo
	codesRepo    *mcprepo.CodesRepo
	tokensRepo   *mcprepo.TokensRepo
	sessionsRepo *mcprepo.SessionsRepo
	feedbackRepo *feedback.FeedbackRepo
	jwtSecret    []byte
	jwtIssuer    string
}

// newTestEnv creates a test environment with all repos and server configured.
func newTestEnv(t *testing.T, tenantSlug, tenantName string) *testEnv {
	t.Helper()
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, tenantSlug, tenantName)
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	env := &testEnv{
		pool:         pool,
		tenantID:     tenantID,
		clientsRepo:  mcprepo.NewClients(pool),
		codesRepo:    mcprepo.NewCodes(pool),
		tokensRepo:   mcprepo.NewTokens(pool),
		sessionsRepo: mcprepo.NewSessions(pool),
		feedbackRepo: feedback.NewFeedback(pool),
		jwtSecret:    []byte("test-secret-key-for-jwt-signing-32bytes!"),
		jwtIssuer:    "https://test.attune.io/mcp/oauth",
	}

	cfg := mcp.Config{
		BaseURL:            "https://test.attune.io",
		JWTSecret:          env.jwtSecret,
		JWTIssuer:          env.jwtIssuer,
		RateLimitPerMinute: 100,
		AccessTokenTTL:     time.Hour,
		RefreshTokenTTL:    7 * 24 * time.Hour,
	}

	stores := mcp.Stores{
		Clients:          &clientStoreAdapter{repo: env.clientsRepo},
		Codes:            &codeStoreAdapter{repo: env.codesRepo},
		Tokens:           &tokenStoreAdapter{repo: env.tokensRepo},
		Sessions:         &sessionStoreAdapter{repo: env.sessionsRepo},
		ClientValidator:  &clientValidatorAdapter{repo: env.clientsRepo},
		SessionValidator: &sessionValidatorAdapter{repo: env.sessionsRepo},
	}

	deps := ptrext.Of(tools.Deps{
		Feedback: &feedbackReaderAdapter{repo: env.feedbackRepo, tenantID: tenantID},
	})

	handler := mcp.NewHandler(cfg, stores, deps)
	router := handler.Routes()

	testRouter := chi.NewRouter()
	testRouter.Mount("/mcp", router)

	env.server = httptest.NewServer(testRouter)
	t.Cleanup(func() { env.server.Close() })

	return env
}

// createClient creates an OAuth client for testing.
func (e *testEnv) createClient(t *testing.T, name string, scopes []string) *mcprepo.Client {
	t.Helper()
	client, err := e.clientsRepo.Create(context.Background(), mcprepo.CreateClientParams{
		TenantID:     e.tenantID,
		Name:         name,
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       scopes,
		CreatedBy:    "test-admin",
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	return client
}

// pkceChallenge generates a PKCE code challenge from a verifier.
func pkceChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// noRedirectHTTPClient returns an http.Client that doesn't follow redirects.
func noRedirectHTTPClient() *http.Client {
	return &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// authorizeAndGetCode performs OAuth authorize and extracts the code from redirect.
func (e *testEnv) authorizeAndGetCode(t *testing.T, clientID uuid.UUID, scopes, state, codeChallenge string) string {
	t.Helper()
	authURL := e.server.URL + "/mcp/oauth/authorize?" + url.Values{
		"client_id":             {clientID.String()},
		"redirect_uri":          {"http://localhost:8080/callback"},
		"response_type":         {"code"},
		"scope":                 {scopes},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}.Encode()

	resp, err := noRedirectHTTPClient().Get(authURL)
	if err != nil {
		t.Fatalf("authorize request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize: expected 302, got %d: %s", resp.StatusCode, string(body))
	}

	location := resp.Header.Get("Location")
	redirectURL, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect URL: %v", err)
	}

	code := redirectURL.Query().Get("code")
	if code == "" {
		t.Fatalf("authorize: missing code in redirect: %s", location)
	}
	return code
}

// tokenResponse holds the JSON response from /oauth/token.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// exchangeCode exchanges an authorization code for tokens.
func (e *testEnv) exchangeCode(t *testing.T, clientID uuid.UUID, code, verifier string) tokenResponse {
	t.Helper()
	resp, err := http.PostForm(e.server.URL+"/mcp/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID.String()},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"code_verifier": {verifier},
	})
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("token: expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var tokenData tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenData); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	return tokenData
}

// completeOAuthFlow performs the full OAuth flow and returns tokens.
func (e *testEnv) completeOAuthFlow(t *testing.T, clientID uuid.UUID, scopes string) tokenResponse {
	t.Helper()
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := pkceChallenge(verifier)
	code := e.authorizeAndGetCode(t, clientID, scopes, "test-state", challenge)
	return e.exchangeCode(t, clientID, code, verifier)
}

// callMCPAPI calls the MCP JSON-RPC API with the given access token.
func (e *testEnv) callMCPAPI(t *testing.T, accessToken string, method string, params json.RawMessage) (*http.Response, jsonrpc.Response) {
	t.Helper()
	req := jsonrpc.Request{JSONRPC: "2.0", Method: method, Params: params, ID: "1"}
	reqBody, _ := json.Marshal(req)

	httpReq, _ := http.NewRequest(http.MethodPost, e.server.URL+"/mcp/v1", bytes.NewReader(reqBody))
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("API request: %v", err)
	}

	var rpcResp jsonrpc.Response
	json.NewDecoder(resp.Body).Decode(&rpcResp)
	return resp, rpcResp
}

// extractJWTClaims decodes a JWT and extracts its claims without verification.
func extractJWTClaims(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT format: %d parts", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal JWT claims: %v", err)
	}
	return claims
}

// TestE2E_FullOAuthFlow tests the complete OAuth 2.1 flow with PKCE.
func TestE2E_FullOAuthFlow(t *testing.T) {
	env := newTestEnv(t, "e2e-oauth-test", "E2E OAuth Test")
	ctx := context.Background()
	client := env.createClient(t, "e2e-test-agent", []string{"mcp:read", "mcp:write"})

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := pkceChallenge(verifier)
	code := env.authorizeAndGetCode(t, client.ID, "mcp:read mcp:write", "test-state-123", challenge)
	tokenData := env.exchangeCode(t, client.ID, code, verifier)

	if tokenData.AccessToken == "" || tokenData.RefreshToken == "" {
		t.Fatal("token: missing access_token or refresh_token")
	}

	t.Run("api_call", func(t *testing.T) {
		resp, rpcResp := env.callMCPAPI(t, tokenData.AccessToken, "list_feedback", json.RawMessage(`{}`))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("API: expected 200, got %d", resp.StatusCode)
		}
		if rpcResp.Error != nil {
			t.Fatalf("API: unexpected error: %+v", rpcResp.Error)
		}
		if xfo := resp.Header.Get("X-Frame-Options"); xfo != "DENY" {
			t.Errorf("X-Frame-Options = %q, want DENY", xfo)
		}
	})

	t.Run("token_refresh", func(t *testing.T) {
		refreshed := env.refreshToken(t, client.ID, tokenData.RefreshToken)
		if refreshed.AccessToken == "" || refreshed.RefreshToken == "" {
			t.Fatal("refresh: missing tokens")
		}
		if refreshed.RefreshToken == tokenData.RefreshToken {
			t.Error("refresh: token should be rotated")
		}
		tokenData = refreshed
	})

	t.Run("old_refresh_token_rejected", func(t *testing.T) {
		resp, _ := http.PostForm(env.server.URL+"/mcp/oauth/token", url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {verifier}, // Use wrong token
			"client_id":     {client.ID.String()},
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("old refresh: expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("code_replay_rejected", func(t *testing.T) {
		resp, _ := http.PostForm(env.server.URL+"/mcp/oauth/token", url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {code},
			"client_id":     {client.ID.String()},
			"redirect_uri":  {"http://localhost:8080/callback"},
			"code_verifier": {verifier},
		})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("replay: expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("revoked_client_rejected", func(t *testing.T) {
		if err := env.clientsRepo.Revoke(ctx, client.ID); err != nil {
			t.Fatalf("revoke client: %v", err)
		}
		resp, _ := env.callMCPAPI(t, tokenData.AccessToken, "list_feedback", json.RawMessage(`{}`))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("revoked: expected 401, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), "client revoked") {
			t.Error("error message leaks client state")
		}
	})
}

// refreshToken performs a token refresh and returns the new tokens.
func (e *testEnv) refreshToken(t *testing.T, clientID uuid.UUID, refreshToken string) tokenResponse {
	t.Helper()
	resp, err := http.PostForm(e.server.URL+"/mcp/oauth/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID.String()},
	})
	if err != nil {
		t.Fatalf("refresh request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("refresh: expected 200, got %d: %s", resp.StatusCode, string(body))
	}
	var tokenData tokenResponse
	json.NewDecoder(resp.Body).Decode(&tokenData)
	return tokenData
}

// TestE2E_PKCEVerifierRejection verifies that wrong PKCE verifier is rejected.
func TestE2E_PKCEVerifierRejection(t *testing.T) {
	env := newTestEnv(t, "e2e-pkce-test", "E2E PKCE Test")
	client := env.createClient(t, "pkce-test-agent", []string{"mcp:read"})

	correctVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := pkceChallenge(correctVerifier)
	code := env.authorizeAndGetCode(t, client.ID, "mcp:read", "test-state", challenge)

	wrongVerifier := "WRONG-verifier-that-does-not-match-the-challenge"
	resp, err := http.PostForm(env.server.URL+"/mcp/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {client.ID.String()},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"code_verifier": {wrongVerifier},
	})
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("wrong PKCE verifier: expected 400, got %d", resp.StatusCode)
	}

	var errResp struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	json.NewDecoder(resp.Body).Decode(&errResp)

	if errResp.Error != "invalid_grant" {
		t.Errorf("wrong PKCE verifier: error = %q, want invalid_grant", errResp.Error)
	}
	if !strings.Contains(errResp.ErrorDescription, "PKCE") {
		t.Errorf("error_description should mention PKCE, got %q", errResp.ErrorDescription)
	}
}

// TestE2E_JWTClaimsValidation verifies JWT contains correct claims.
func TestE2E_JWTClaimsValidation(t *testing.T) {
	env := newTestEnv(t, "e2e-jwt-test", "E2E JWT Test")
	ctx := context.Background()
	client := env.createClient(t, "jwt-test-agent", []string{"mcp:read", "mcp:write"})
	tokenData := env.completeOAuthFlow(t, client.ID, "mcp:read mcp:write")

	claims := extractJWTClaims(t, tokenData.AccessToken)

	if iss, _ := claims["iss"].(string); iss != env.jwtIssuer {
		t.Errorf("JWT issuer = %q, want %q", iss, env.jwtIssuer)
	}

	if aud, _ := claims["aud"].([]any); len(aud) == 0 || aud[0] != "attune-mcp" {
		t.Errorf("JWT audience = %v, want [attune-mcp]", aud)
	}

	if tid, _ := claims["tenant_id"].(string); tid != env.tenantID {
		t.Errorf("JWT tenant_id = %q, want %q", tid, env.tenantID)
	}

	if cid, _ := claims["client_id"].(string); cid != client.ID.String() {
		t.Errorf("JWT client_id = %q, want %q", cid, client.ID.String())
	}

	sessionID, _ := claims["session_id"].(string)
	if sessionID == "" || sessionID == "00000000-0000-0000-0000-000000000000" {
		t.Errorf("JWT session_id is empty or nil UUID: %q", sessionID)
	}
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		t.Errorf("JWT session_id is not a valid UUID: %q", sessionID)
	}

	if scopes, _ := claims["scopes"].([]any); len(scopes) != 2 {
		t.Errorf("JWT scopes = %v, want [mcp:read mcp:write]", scopes)
	}

	if exp, _ := claims["exp"].(float64); time.Unix(int64(exp), 0).Before(time.Now()) {
		t.Errorf("JWT expired at %v", time.Unix(int64(exp), 0))
	}

	active, err := env.sessionsRepo.IsActive(ctx, sid)
	if err != nil {
		t.Fatalf("check session active: %v", err)
	}
	if !active {
		t.Error("session should be active in database")
	}
}

// TestE2E_SessionPersistence verifies session is correctly persisted in database.
func TestE2E_SessionPersistence(t *testing.T) {
	env := newTestEnv(t, "e2e-session-test", "E2E Session Test")
	ctx := context.Background()
	client := env.createClient(t, "session-test-agent", []string{"mcp:read"})
	tokenData := env.completeOAuthFlow(t, client.ID, "mcp:read")

	claims := extractJWTClaims(t, tokenData.AccessToken)
	sessionIDStr, _ := claims["session_id"].(string)
	sessionID, _ := uuid.Parse(sessionIDStr)

	var dbClientID uuid.UUID
	var dbTenantID string
	var dbClosedAt *time.Time
	err := env.pool.QueryRow(ctx, `
		SELECT client_id, tenant_id, closed_at FROM mcp_sessions WHERE id = $1
	`, sessionID).Scan(&dbClientID, &dbTenantID, &dbClosedAt)
	if err != nil {
		t.Fatalf("query session from database: %v", err)
	}

	if dbClientID != client.ID {
		t.Errorf("session client_id = %s, want %s", dbClientID, client.ID)
	}
	if dbTenantID != env.tenantID {
		t.Errorf("session tenant_id = %s, want %s", dbTenantID, env.tenantID)
	}
	if dbClosedAt != nil {
		t.Error("session should not be closed")
	}

	var tokenCount int
	err = env.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM mcp_oauth_refresh_tokens
		WHERE client_id = $1 AND session_id = $2 AND revoked_at IS NULL
	`, client.ID, sessionID).Scan(&tokenCount)
	if err != nil {
		t.Fatalf("query refresh tokens: %v", err)
	}
	if tokenCount != 1 {
		t.Errorf("expected 1 active refresh token, got %d", tokenCount)
	}
}

// TestE2E_CrossTenantAccessBlocked verifies tokens from one tenant cannot access another.
func TestE2E_CrossTenantAccessBlocked(t *testing.T) {
	envA := newTestEnv(t, "tenant-a-test", "Tenant A")
	envB := newTestEnv(t, "tenant-b-test", "Tenant B")

	clientA := envA.createClient(t, "tenant-a-agent", []string{"mcp:read"})
	clientB := envB.createClient(t, "tenant-b-agent", []string{"mcp:read"})

	tokenDataA := envA.completeOAuthFlow(t, clientA.ID, "mcp:read")
	tokenDataB := envB.completeOAuthFlow(t, clientB.ID, "mcp:read")

	claimsA := extractJWTClaims(t, tokenDataA.AccessToken)
	claimsB := extractJWTClaims(t, tokenDataB.AccessToken)

	tenantInTokenA, _ := claimsA["tenant_id"].(string)
	tenantInTokenB, _ := claimsB["tenant_id"].(string)

	if tenantInTokenA != envA.tenantID {
		t.Errorf("token A tenant_id = %s, want %s", tenantInTokenA, envA.tenantID)
	}
	if tenantInTokenB != envB.tenantID {
		t.Errorf("token B tenant_id = %s, want %s", tenantInTokenB, envB.tenantID)
	}
	if tenantInTokenA == tenantInTokenB {
		t.Error("tenant A and B tokens should have different tenant_ids")
	}
}

// Adapter implementations to bridge repo types to oauth interface types

type clientStoreAdapter struct {
	repo *mcprepo.ClientsRepo
}

func (a *clientStoreAdapter) GetByID(ctx context.Context, id uuid.UUID) (*oauth.Client, error) {
	c, err := a.repo.GetActiveByID(ctx, id)
	if err != nil {
		return nil, oauth.ErrInvalidClient
	}
	return &oauth.Client{
		ID:           c.ID,
		TenantID:     c.TenantID,
		RedirectURIs: c.RedirectURIs,
		Scopes:       c.Scopes,
	}, nil
}

func (a *clientStoreAdapter) ValidateRedirectURI(ctx context.Context, clientID uuid.UUID, uri string) (bool, error) {
	c, err := a.repo.GetActiveByID(ctx, clientID)
	if err != nil {
		return false, nil
	}
	for _, allowed := range c.RedirectURIs {
		if allowed == uri {
			return true, nil
		}
	}
	return false, nil
}

type codeStoreAdapter struct {
	repo *mcprepo.CodesRepo
}

func (a *codeStoreAdapter) Create(ctx context.Context, code *oauth.AuthCode) error {
	_, err := a.repo.Create(ctx, mcprepo.CreateCodeParams{
		Code:          code.Code,
		ClientID:      code.ClientID,
		RedirectURI:   code.RedirectURI,
		Scopes:        code.Scopes,
		CodeChallenge: code.CodeChallenge,
		UserID:        code.TenantID, // oauth uses TenantID, repo uses UserID
		ExpiresAt:     code.ExpiresAt,
	})
	return err
}

func (a *codeStoreAdapter) Consume(ctx context.Context, code string) (*oauth.AuthCode, error) {
	c, err := a.repo.Consume(ctx, code)
	if err != nil {
		return nil, oauth.ErrInvalidCode
	}
	return &oauth.AuthCode{
		Code:                c.Code,
		ClientID:            c.ClientID,
		TenantID:            c.UserID, // repo uses UserID, oauth uses TenantID
		RedirectURI:         c.RedirectURI,
		Scopes:              c.Scopes,
		CodeChallenge:       c.CodeChallenge,
		CodeChallengeMethod: "S256", // OAuth 2.1 mandates S256; db doesn't store this since it's the only option
		ExpiresAt:           c.ExpiresAt,
	}, nil
}

type tokenStoreAdapter struct {
	repo *mcprepo.TokensRepo
}

func repoToOAuthToken(t *mcprepo.RefreshToken) *oauth.RefreshToken {
	return &oauth.RefreshToken{
		ID: t.ID, ClientID: t.ClientID, SessionID: t.SessionID,
		TokenHash: t.TokenHash, TenantID: t.UserID, Scopes: t.Scopes, ExpiresAt: t.ExpiresAt,
	}
}

func (a *tokenStoreAdapter) Create(ctx context.Context, token *oauth.RefreshToken) error {
	_, err := a.repo.CreateWithHash(ctx, mcprepo.CreateWithHashParams{
		ClientID: token.ClientID, SessionID: token.SessionID, TokenHash: token.TokenHash,
		Scopes: token.Scopes, UserID: token.TenantID, ExpiresAt: token.ExpiresAt,
	})
	return err
}

func (a *tokenStoreAdapter) GetByHash(ctx context.Context, hash string) (*oauth.RefreshToken, error) {
	t, err := a.repo.GetByHash(ctx, hash)
	if err != nil {
		return nil, oauth.ErrInvalidRefreshToken
	}
	return repoToOAuthToken(t), nil
}

func (a *tokenStoreAdapter) Revoke(ctx context.Context, id uuid.UUID) error {
	return a.repo.Revoke(ctx, id)
}

func (a *tokenStoreAdapter) Consume(ctx context.Context, hash string) (*oauth.RefreshToken, error) {
	t, err := a.repo.Consume(ctx, hash)
	if err != nil {
		return nil, oauth.ErrInvalidRefreshToken
	}
	return repoToOAuthToken(t), nil
}

func (a *tokenStoreAdapter) RotateToken(ctx context.Context, oldHash, newHash string, newExpiresAt time.Time) (*oauth.RefreshToken, *oauth.RefreshToken, error) {
	old, newTok, err := a.repo.RotateToken(ctx, mcprepo.RotateTokenParams{
		OldTokenHash: oldHash, NewTokenHash: newHash, NewExpiresAt: newExpiresAt,
	})
	if err != nil {
		return nil, nil, oauth.ErrInvalidRefreshToken
	}
	return repoToOAuthToken(old), repoToOAuthToken(newTok), nil
}

type sessionStoreAdapter struct {
	repo *mcprepo.SessionsRepo
}

func (a *sessionStoreAdapter) Create(ctx context.Context, session *oauth.Session) error {
	s, err := a.repo.Create(ctx, mcprepo.CreateSessionParams{
		ClientID: session.ClientID,
		TenantID: session.TenantID,
		Scopes:   session.Scopes,
	})
	if err != nil {
		return err
	}
	session.ID = s.ID
	return nil
}

func (a *sessionStoreAdapter) Touch(ctx context.Context, id uuid.UUID) error {
	return a.repo.Touch(ctx, id)
}

func (a *sessionStoreAdapter) IsActive(ctx context.Context, id uuid.UUID) (bool, error) {
	return a.repo.IsActive(ctx, id)
}

type clientValidatorAdapter struct {
	repo *mcprepo.ClientsRepo
}

func (a *clientValidatorAdapter) IsRevoked(ctx context.Context, clientID uuid.UUID) (bool, error) {
	return a.repo.IsRevoked(ctx, clientID)
}

type sessionValidatorAdapter struct {
	repo *mcprepo.SessionsRepo
}

func (a *sessionValidatorAdapter) IsActive(ctx context.Context, sessionID uuid.UUID) (bool, error) {
	return a.repo.IsActive(ctx, sessionID)
}

type feedbackReaderAdapter struct {
	repo     *feedback.FeedbackRepo
	tenantID string
}

func (a *feedbackReaderAdapter) ListForConsole(ctx context.Context, tenantID string, opts feedback.ConsoleListOpts) ([]feedback.ConsoleListRow, error) {
	return a.repo.ListForConsole(ctx, tenantID, opts)
}

func (a *feedbackReaderAdapter) GetForConsole(ctx context.Context, tenantID string, id int64) (*feedback.ConsoleDetailRow, error) {
	return a.repo.GetForConsole(ctx, tenantID, id)
}
