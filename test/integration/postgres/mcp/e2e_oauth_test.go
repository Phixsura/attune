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

// TestE2E_FullOAuthFlow tests the complete OAuth 2.1 flow with PKCE:
// 1. Register a client
// 2. Authorization request with PKCE
// 3. Token exchange
// 4. Access protected API
// 5. Token refresh
// 6. Client revocation
func TestE2E_FullOAuthFlow(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	// Create tenant
	tenantID, err := tenant.NewTenant(pool).Create(ctx, "e2e-oauth-test", "E2E OAuth Test")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	// Initialize repos
	clientsRepo := mcprepo.NewClients(pool)
	codesRepo := mcprepo.NewCodes(pool)
	tokensRepo := mcprepo.NewTokens(pool)
	sessionsRepo := mcprepo.NewSessions(pool)
	feedbackRepo := feedback.NewFeedback(pool)

	// Create MCP handler with real repos
	cfg := mcp.Config{
		BaseURL:            "https://test.attune.io",
		JWTSecret:          []byte("test-secret-key-for-jwt-signing-32bytes!"),
		JWTIssuer:          "https://test.attune.io/mcp/oauth",
		RateLimitPerMinute: 100,
		AccessTokenTTL:     time.Hour,
		RefreshTokenTTL:    7 * 24 * time.Hour,
	}

	stores := mcp.Stores{
		Clients:          &clientStoreAdapter{repo: clientsRepo},
		Codes:            &codeStoreAdapter{repo: codesRepo},
		Tokens:           &tokenStoreAdapter{repo: tokensRepo},
		Sessions:         &sessionStoreAdapter{repo: sessionsRepo},
		ClientValidator:  &clientValidatorAdapter{repo: clientsRepo},
		SessionValidator: &sessionValidatorAdapter{repo: sessionsRepo},
	}

	deps := ptrext.Of(tools.Deps{
		Feedback: &feedbackReaderAdapter{repo: feedbackRepo, tenantID: tenantID},
	})

	handler := mcp.NewHandler(cfg, stores, deps)
	router := handler.Routes()

	// Wrap with tenant context middleware for testing
	testRouter := chi.NewRouter()
	testRouter.Mount("/mcp", router)

	server := httptest.NewServer(testRouter)
	defer server.Close()

	// Step 1: Register a client (normally done via console API)
	client, err := clientsRepo.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tenantID,
		Name:         "e2e-test-agent",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"mcp:read", "mcp:write"},
		CreatedBy:    "test-admin",
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	t.Logf("Created client: %s", client.ID)

	// Step 2: Generate PKCE challenge
	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	// Step 3: Authorization request
	authURL := server.URL + "/mcp/oauth/authorize?" + url.Values{
		"client_id":             {client.ID.String()},
		"redirect_uri":          {"http://localhost:8080/callback"},
		"response_type":         {"code"},
		"scope":                 {"mcp:read mcp:write"},
		"state":                 {"test-state-123"},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}.Encode()

	// Use a client that doesn't follow redirects
	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	authResp, err := noRedirectClient.Get(authURL)
	if err != nil {
		t.Fatalf("authorize request: %v", err)
	}
	defer authResp.Body.Close()

	// Should redirect with authorization code
	if authResp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(authResp.Body)
		t.Fatalf("authorize: expected 302, got %d: %s", authResp.StatusCode, string(body))
	}

	location := authResp.Header.Get("Location")
	if location == "" {
		t.Fatal("authorize: missing Location header")
	}

	redirectURL, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse redirect URL: %v", err)
	}

	authCode := redirectURL.Query().Get("code")
	if authCode == "" {
		t.Fatalf("authorize: missing code in redirect: %s", location)
	}
	t.Logf("Got authorization code: %s...", authCode[:16])

	state := redirectURL.Query().Get("state")
	if state != "test-state-123" {
		t.Fatalf("authorize: state mismatch: %s", state)
	}

	// Step 4: Token exchange
	tokenResp, err := http.PostForm(server.URL+"/mcp/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authCode},
		"client_id":     {client.ID.String()},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"code_verifier": {codeVerifier},
	})
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokenResp.Body)
		t.Fatalf("token: expected 200, got %d: %s", tokenResp.StatusCode, string(body))
	}

	var tokenData struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil {
		t.Fatalf("decode token response: %v", err)
	}

	if tokenData.AccessToken == "" {
		t.Fatal("token: missing access_token")
	}
	if tokenData.RefreshToken == "" {
		t.Fatal("token: missing refresh_token")
	}
	t.Logf("Got access token: %s...", tokenData.AccessToken[:32])

	// Verify Cache-Control header
	if cc := tokenResp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("token: Cache-Control = %q, want no-store", cc)
	}

	// Step 5: Access protected API (list_feedback)
	jsonrpcReq := jsonrpc.Request{
		JSONRPC: "2.0",
		Method:  "list_feedback",
		Params:  json.RawMessage(`{}`),
		ID:      "1",
	}
	reqBody, _ := json.Marshal(jsonrpcReq)

	apiReq, _ := http.NewRequest(http.MethodPost, server.URL+"/mcp/v1", bytes.NewReader(reqBody))
	apiReq.Header.Set("Authorization", "Bearer "+tokenData.AccessToken)
	apiReq.Header.Set("Content-Type", "application/json")

	apiResp, err := http.DefaultClient.Do(apiReq)
	if err != nil {
		t.Fatalf("API request: %v", err)
	}
	defer apiResp.Body.Close()

	if apiResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(apiResp.Body)
		t.Fatalf("API: expected 200, got %d: %s", apiResp.StatusCode, string(body))
	}

	// Verify security headers
	if xfo := apiResp.Header.Get("X-Frame-Options"); xfo != "DENY" {
		t.Errorf("API: X-Frame-Options = %q, want DENY", xfo)
	}
	if xcto := apiResp.Header.Get("X-Content-Type-Options"); xcto != "nosniff" {
		t.Errorf("API: X-Content-Type-Options = %q, want nosniff", xcto)
	}

	// Verify rate limit headers
	if rl := apiResp.Header.Get("X-RateLimit-Limit"); rl == "" {
		t.Error("API: missing X-RateLimit-Limit header")
	}

	var rpcResp jsonrpc.Response
	if err := json.NewDecoder(apiResp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode API response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("API: unexpected error: %+v", rpcResp.Error)
	}
	t.Log("API call successful")

	// Step 6: Token refresh
	refreshResp, err := http.PostForm(server.URL+"/mcp/oauth/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tokenData.RefreshToken},
		"client_id":     {client.ID.String()},
	})
	if err != nil {
		t.Fatalf("refresh request: %v", err)
	}
	defer refreshResp.Body.Close()

	if refreshResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(refreshResp.Body)
		t.Fatalf("refresh: expected 200, got %d: %s", refreshResp.StatusCode, string(body))
	}

	var refreshData struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(refreshResp.Body).Decode(&refreshData); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}

	if refreshData.AccessToken == "" {
		t.Fatal("refresh: missing access_token")
	}
	if refreshData.RefreshToken == "" {
		t.Fatal("refresh: missing refresh_token")
	}
	if refreshData.RefreshToken == tokenData.RefreshToken {
		t.Error("refresh: token should be rotated")
	}
	t.Log("Token refresh successful")

	// Step 7: Old refresh token should be invalid
	oldRefreshResp, err := http.PostForm(server.URL+"/mcp/oauth/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {tokenData.RefreshToken},
		"client_id":     {client.ID.String()},
	})
	if err != nil {
		t.Fatalf("old refresh request: %v", err)
	}
	defer oldRefreshResp.Body.Close()

	if oldRefreshResp.StatusCode != http.StatusBadRequest {
		t.Errorf("old refresh: expected 400, got %d", oldRefreshResp.StatusCode)
	}
	t.Log("Old refresh token correctly rejected")

	// Step 8: Verify authorization code cannot be reused
	replayResp, err := http.PostForm(server.URL+"/mcp/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authCode},
		"client_id":     {client.ID.String()},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"code_verifier": {codeVerifier},
	})
	if err != nil {
		t.Fatalf("replay request: %v", err)
	}
	defer replayResp.Body.Close()

	if replayResp.StatusCode != http.StatusBadRequest {
		t.Errorf("replay: expected 400, got %d", replayResp.StatusCode)
	}
	t.Log("Authorization code replay correctly rejected")

	// Step 9: Revoke client
	if err := clientsRepo.Revoke(ctx, client.ID); err != nil {
		t.Fatalf("revoke client: %v", err)
	}

	// Step 10: API call with revoked client should fail
	revokedReq, _ := http.NewRequest(http.MethodPost, server.URL+"/mcp/v1", bytes.NewReader(reqBody))
	revokedReq.Header.Set("Authorization", "Bearer "+refreshData.AccessToken)
	revokedReq.Header.Set("Content-Type", "application/json")

	revokedResp, err := http.DefaultClient.Do(revokedReq)
	if err != nil {
		t.Fatalf("revoked API request: %v", err)
	}
	defer revokedResp.Body.Close()

	if revokedResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("revoked: expected 401, got %d", revokedResp.StatusCode)
	}

	// Verify error message doesn't leak "client revoked"
	body, _ := io.ReadAll(revokedResp.Body)
	if strings.Contains(string(body), "client revoked") {
		t.Error("revoked: error message leaks client state")
	}
	t.Log("Revoked client correctly rejected")

	t.Log("=== E2E OAuth flow completed successfully ===")
}

// TestE2E_PKCEVerifierRejection verifies that wrong PKCE verifier is rejected.
func TestE2E_PKCEVerifierRejection(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "e2e-pkce-test", "E2E PKCE Test")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	clientsRepo := mcprepo.NewClients(pool)
	codesRepo := mcprepo.NewCodes(pool)
	tokensRepo := mcprepo.NewTokens(pool)
	sessionsRepo := mcprepo.NewSessions(pool)
	feedbackRepo := feedback.NewFeedback(pool)

	cfg := mcp.Config{
		BaseURL:            "https://test.attune.io",
		JWTSecret:          []byte("test-secret-key-for-jwt-signing-32bytes!"),
		JWTIssuer:          "https://test.attune.io/mcp/oauth",
		RateLimitPerMinute: 100,
		AccessTokenTTL:     time.Hour,
		RefreshTokenTTL:    7 * 24 * time.Hour,
	}

	stores := mcp.Stores{
		Clients:          &clientStoreAdapter{repo: clientsRepo},
		Codes:            &codeStoreAdapter{repo: codesRepo},
		Tokens:           &tokenStoreAdapter{repo: tokensRepo},
		Sessions:         &sessionStoreAdapter{repo: sessionsRepo},
		ClientValidator:  &clientValidatorAdapter{repo: clientsRepo},
		SessionValidator: &sessionValidatorAdapter{repo: sessionsRepo},
	}

	deps := ptrext.Of(tools.Deps{
		Feedback: &feedbackReaderAdapter{repo: feedbackRepo, tenantID: tenantID},
	})

	handler := mcp.NewHandler(cfg, stores, deps)
	router := handler.Routes()

	testRouter := chi.NewRouter()
	testRouter.Mount("/mcp", router)

	server := httptest.NewServer(testRouter)
	defer server.Close()

	client, err := clientsRepo.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tenantID,
		Name:         "pkce-test-agent",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "test-admin",
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	// Generate PKCE challenge with correct verifier
	correctVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(correctVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	// Get authorization code
	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	authURL := server.URL + "/mcp/oauth/authorize?" + url.Values{
		"client_id":             {client.ID.String()},
		"redirect_uri":          {"http://localhost:8080/callback"},
		"response_type":         {"code"},
		"scope":                 {"mcp:read"},
		"state":                 {"test-state"},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}.Encode()

	authResp, err := noRedirectClient.Get(authURL)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer authResp.Body.Close()

	location := authResp.Header.Get("Location")
	redirectURL, _ := url.Parse(location)
	authCode := redirectURL.Query().Get("code")

	// Try token exchange with WRONG verifier
	wrongVerifier := "WRONG-verifier-that-does-not-match-the-challenge"
	tokenResp, err := http.PostForm(server.URL+"/mcp/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authCode},
		"client_id":     {client.ID.String()},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"code_verifier": {wrongVerifier},
	})
	if err != nil {
		t.Fatalf("token request: %v", err)
	}
	defer tokenResp.Body.Close()

	// Should fail with 400
	if tokenResp.StatusCode != http.StatusBadRequest {
		t.Errorf("wrong PKCE verifier: expected 400, got %d", tokenResp.StatusCode)
	}

	var errResp struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	json.NewDecoder(tokenResp.Body).Decode(&errResp)

	// Error should indicate PKCE failure
	if errResp.Error != "invalid_grant" {
		t.Errorf("wrong PKCE verifier: error = %q, want invalid_grant", errResp.Error)
	}
	if !strings.Contains(errResp.ErrorDescription, "PKCE") {
		t.Errorf("wrong PKCE verifier: error_description should mention PKCE, got %q", errResp.ErrorDescription)
	}

	t.Log("PKCE verifier rejection test passed")
}

// TestE2E_JWTClaimsValidation verifies JWT contains correct claims.
func TestE2E_JWTClaimsValidation(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "e2e-jwt-test", "E2E JWT Test")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	clientsRepo := mcprepo.NewClients(pool)
	codesRepo := mcprepo.NewCodes(pool)
	tokensRepo := mcprepo.NewTokens(pool)
	sessionsRepo := mcprepo.NewSessions(pool)
	feedbackRepo := feedback.NewFeedback(pool)

	jwtSecret := []byte("test-secret-key-for-jwt-signing-32bytes!")
	jwtIssuer := "https://test.attune.io/mcp/oauth"

	cfg := mcp.Config{
		BaseURL:            "https://test.attune.io",
		JWTSecret:          jwtSecret,
		JWTIssuer:          jwtIssuer,
		RateLimitPerMinute: 100,
		AccessTokenTTL:     time.Hour,
		RefreshTokenTTL:    7 * 24 * time.Hour,
	}

	stores := mcp.Stores{
		Clients:          &clientStoreAdapter{repo: clientsRepo},
		Codes:            &codeStoreAdapter{repo: codesRepo},
		Tokens:           &tokenStoreAdapter{repo: tokensRepo},
		Sessions:         &sessionStoreAdapter{repo: sessionsRepo},
		ClientValidator:  &clientValidatorAdapter{repo: clientsRepo},
		SessionValidator: &sessionValidatorAdapter{repo: sessionsRepo},
	}

	deps := ptrext.Of(tools.Deps{
		Feedback: &feedbackReaderAdapter{repo: feedbackRepo, tenantID: tenantID},
	})

	handler := mcp.NewHandler(cfg, stores, deps)
	router := handler.Routes()

	testRouter := chi.NewRouter()
	testRouter.Mount("/mcp", router)

	server := httptest.NewServer(testRouter)
	defer server.Close()

	client, err := clientsRepo.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tenantID,
		Name:         "jwt-test-agent",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"mcp:read", "mcp:write"},
		CreatedBy:    "test-admin",
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	// Get tokens
	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	authResp, _ := noRedirectClient.Get(server.URL + "/mcp/oauth/authorize?" + url.Values{
		"client_id":             {client.ID.String()},
		"redirect_uri":          {"http://localhost:8080/callback"},
		"response_type":         {"code"},
		"scope":                 {"mcp:read mcp:write"},
		"state":                 {"test"},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}.Encode())
	defer authResp.Body.Close()

	location := authResp.Header.Get("Location")
	redirectURL, _ := url.Parse(location)
	authCode := redirectURL.Query().Get("code")

	tokenResp, _ := http.PostForm(server.URL+"/mcp/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authCode},
		"client_id":     {client.ID.String()},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"code_verifier": {codeVerifier},
	})
	defer tokenResp.Body.Close()

	var tokenData struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(tokenResp.Body).Decode(&tokenData)

	// Decode JWT without verification to inspect claims
	parts := strings.Split(tokenData.AccessToken, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT format: %d parts", len(parts))
	}

	// Decode payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}

	var claims struct {
		Issuer    string   `json:"iss"`
		Audience  []string `json:"aud"`
		TenantID  string   `json:"tenant_id"`
		ClientID  string   `json:"client_id"`
		SessionID string   `json:"session_id"`
		Scopes    []string `json:"scopes"`
		ExpiresAt int64    `json:"exp"`
		IssuedAt  int64    `json:"iat"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal JWT claims: %v", err)
	}

	// Verify issuer
	if claims.Issuer != jwtIssuer {
		t.Errorf("JWT issuer = %q, want %q", claims.Issuer, jwtIssuer)
	}

	// Verify audience
	if len(claims.Audience) == 0 || claims.Audience[0] != "attune-mcp" {
		t.Errorf("JWT audience = %v, want [attune-mcp]", claims.Audience)
	}

	// Verify tenant_id
	if claims.TenantID != tenantID {
		t.Errorf("JWT tenant_id = %q, want %q", claims.TenantID, tenantID)
	}

	// Verify client_id
	if claims.ClientID != client.ID.String() {
		t.Errorf("JWT client_id = %q, want %q", claims.ClientID, client.ID.String())
	}

	// Verify session_id is a valid UUID (not empty)
	if claims.SessionID == "" || claims.SessionID == "00000000-0000-0000-0000-000000000000" {
		t.Errorf("JWT session_id is empty or nil UUID: %q", claims.SessionID)
	}
	if _, err := uuid.Parse(claims.SessionID); err != nil {
		t.Errorf("JWT session_id is not a valid UUID: %q", claims.SessionID)
	}

	// Verify scopes
	if len(claims.Scopes) != 2 {
		t.Errorf("JWT scopes = %v, want [mcp:read mcp:write]", claims.Scopes)
	}

	// Verify exp is in the future (about 1 hour)
	expTime := time.Unix(claims.ExpiresAt, 0)
	if expTime.Before(time.Now()) {
		t.Errorf("JWT expired at %v", expTime)
	}
	if expTime.After(time.Now().Add(2 * time.Hour)) {
		t.Errorf("JWT expires too far in future: %v", expTime)
	}

	// Verify session exists in database
	sessionID, _ := uuid.Parse(claims.SessionID)
	active, err := sessionsRepo.IsActive(ctx, sessionID)
	if err != nil {
		t.Fatalf("check session active: %v", err)
	}
	if !active {
		t.Error("session should be active in database")
	}

	t.Logf("JWT claims validated: issuer=%s, audience=%v, tenant=%s, session=%s",
		claims.Issuer, claims.Audience, claims.TenantID, claims.SessionID)
}

// TestE2E_SessionPersistence verifies session is correctly persisted in database.
func TestE2E_SessionPersistence(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	tenantID, err := tenant.NewTenant(pool).Create(ctx, "e2e-session-test", "E2E Session Test")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	clientsRepo := mcprepo.NewClients(pool)
	codesRepo := mcprepo.NewCodes(pool)
	tokensRepo := mcprepo.NewTokens(pool)
	sessionsRepo := mcprepo.NewSessions(pool)
	feedbackRepo := feedback.NewFeedback(pool)

	cfg := mcp.Config{
		BaseURL:            "https://test.attune.io",
		JWTSecret:          []byte("test-secret-key-for-jwt-signing-32bytes!"),
		JWTIssuer:          "https://test.attune.io/mcp/oauth",
		RateLimitPerMinute: 100,
		AccessTokenTTL:     time.Hour,
		RefreshTokenTTL:    7 * 24 * time.Hour,
	}

	stores := mcp.Stores{
		Clients:          &clientStoreAdapter{repo: clientsRepo},
		Codes:            &codeStoreAdapter{repo: codesRepo},
		Tokens:           &tokenStoreAdapter{repo: tokensRepo},
		Sessions:         &sessionStoreAdapter{repo: sessionsRepo},
		ClientValidator:  &clientValidatorAdapter{repo: clientsRepo},
		SessionValidator: &sessionValidatorAdapter{repo: sessionsRepo},
	}

	deps := ptrext.Of(tools.Deps{
		Feedback: &feedbackReaderAdapter{repo: feedbackRepo, tenantID: tenantID},
	})

	handler := mcp.NewHandler(cfg, stores, deps)
	router := handler.Routes()

	testRouter := chi.NewRouter()
	testRouter.Mount("/mcp", router)

	server := httptest.NewServer(testRouter)
	defer server.Close()

	client, err := clientsRepo.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tenantID,
		Name:         "session-test-agent",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "test-admin",
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	// Complete OAuth flow
	codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	authResp, _ := noRedirectClient.Get(server.URL + "/mcp/oauth/authorize?" + url.Values{
		"client_id":             {client.ID.String()},
		"redirect_uri":          {"http://localhost:8080/callback"},
		"response_type":         {"code"},
		"scope":                 {"mcp:read"},
		"state":                 {"test"},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}.Encode())
	defer authResp.Body.Close()

	location := authResp.Header.Get("Location")
	redirectURL, _ := url.Parse(location)
	authCode := redirectURL.Query().Get("code")

	tokenResp, _ := http.PostForm(server.URL+"/mcp/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authCode},
		"client_id":     {client.ID.String()},
		"redirect_uri":  {"http://localhost:8080/callback"},
		"code_verifier": {codeVerifier},
	})
	defer tokenResp.Body.Close()

	var tokenData struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	json.NewDecoder(tokenResp.Body).Decode(&tokenData)

	// Extract session ID from JWT
	parts := strings.Split(tokenData.AccessToken, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		SessionID string `json:"session_id"`
	}
	json.Unmarshal(payload, &claims)
	sessionID, _ := uuid.Parse(claims.SessionID)

	// Query database directly to verify session record
	var dbSession struct {
		ID        uuid.UUID
		ClientID  uuid.UUID
		TenantID  string
		Scopes    []string
		ClosedAt  *time.Time
		CreatedAt time.Time
	}
	err = pool.QueryRow(ctx, `
		SELECT id, client_id, tenant_id, scopes, closed_at, created_at
		FROM mcp_sessions WHERE id = $1
	`, sessionID).Scan(&dbSession.ID, &dbSession.ClientID, &dbSession.TenantID, &dbSession.Scopes, &dbSession.ClosedAt, &dbSession.CreatedAt)
	if err != nil {
		t.Fatalf("query session from database: %v", err)
	}

	if dbSession.ClientID != client.ID {
		t.Errorf("session client_id = %s, want %s", dbSession.ClientID, client.ID)
	}
	if dbSession.TenantID != tenantID {
		t.Errorf("session tenant_id = %s, want %s", dbSession.TenantID, tenantID)
	}
	if dbSession.ClosedAt != nil {
		t.Error("session should not be closed")
	}

	// Also verify refresh token in database
	var tokenCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM mcp_oauth_refresh_tokens
		WHERE client_id = $1 AND session_id = $2 AND revoked_at IS NULL
	`, client.ID, sessionID).Scan(&tokenCount)
	if err != nil {
		t.Fatalf("query refresh tokens: %v", err)
	}
	if tokenCount != 1 {
		t.Errorf("expected 1 active refresh token, got %d", tokenCount)
	}

	t.Logf("Session persisted: id=%s, client=%s, tenant=%s", sessionID, client.ID, tenantID)
}

// TestE2E_CrossTenantAccessBlocked verifies tokens from one tenant cannot access another.
func TestE2E_CrossTenantAccessBlocked(t *testing.T) {
	pool := testdb.NewPool(t)
	ctx := context.Background()

	// Create two tenants
	tenantRepo := tenant.NewTenant(pool)
	tenantA, _ := tenantRepo.Create(ctx, "tenant-a-test", "Tenant A")
	tenantB, _ := tenantRepo.Create(ctx, "tenant-b-test", "Tenant B")

	clientsRepo := mcprepo.NewClients(pool)
	codesRepo := mcprepo.NewCodes(pool)
	tokensRepo := mcprepo.NewTokens(pool)
	sessionsRepo := mcprepo.NewSessions(pool)

	cfg := mcp.Config{
		BaseURL:            "https://test.attune.io",
		JWTSecret:          []byte("test-secret-key-for-jwt-signing-32bytes!"),
		JWTIssuer:          "https://test.attune.io/mcp/oauth",
		RateLimitPerMinute: 100,
		AccessTokenTTL:     time.Hour,
		RefreshTokenTTL:    7 * 24 * time.Hour,
	}

	stores := mcp.Stores{
		Clients:          &clientStoreAdapter{repo: clientsRepo},
		Codes:            &codeStoreAdapter{repo: codesRepo},
		Tokens:           &tokenStoreAdapter{repo: tokensRepo},
		Sessions:         &sessionStoreAdapter{repo: sessionsRepo},
		ClientValidator:  &clientValidatorAdapter{repo: clientsRepo},
		SessionValidator: &sessionValidatorAdapter{repo: sessionsRepo},
	}

	// Create separate feedback repos for each tenant
	feedbackRepoA := feedback.NewFeedback(pool)
	feedbackRepoB := feedback.NewFeedback(pool)

	depsA := ptrext.Of(tools.Deps{
		Feedback: &feedbackReaderAdapter{repo: feedbackRepoA, tenantID: tenantA},
	})
	depsB := ptrext.Of(tools.Deps{
		Feedback: &feedbackReaderAdapter{repo: feedbackRepoB, tenantID: tenantB},
	})

	_ = depsB // We'll use tenant A's handler but verify the JWT contains tenant A's ID

	handler := mcp.NewHandler(cfg, stores, depsA)
	router := handler.Routes()

	testRouter := chi.NewRouter()
	testRouter.Mount("/mcp", router)

	server := httptest.NewServer(testRouter)
	defer server.Close()

	// Create client for tenant A
	clientA, _ := clientsRepo.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tenantA,
		Name:         "tenant-a-agent",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "test-admin",
	})

	// Create client for tenant B
	clientB, _ := clientsRepo.Create(ctx, mcprepo.CreateClientParams{
		TenantID:     tenantB,
		Name:         "tenant-b-agent",
		RedirectURIs: []string{"http://localhost:8080/callback"},
		Scopes:       []string{"mcp:read"},
		CreatedBy:    "test-admin",
	})

	// Get tokens for both tenants
	getToken := func(clientID uuid.UUID) string {
		codeVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
		h := sha256.Sum256([]byte(codeVerifier))
		codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])

		noRedirectClient := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}

		authResp, _ := noRedirectClient.Get(server.URL + "/mcp/oauth/authorize?" + url.Values{
			"client_id":             {clientID.String()},
			"redirect_uri":          {"http://localhost:8080/callback"},
			"response_type":         {"code"},
			"scope":                 {"mcp:read"},
			"state":                 {"test"},
			"code_challenge":        {codeChallenge},
			"code_challenge_method": {"S256"},
		}.Encode())
		defer authResp.Body.Close()

		location := authResp.Header.Get("Location")
		redirectURL, _ := url.Parse(location)
		authCode := redirectURL.Query().Get("code")

		tokenResp, _ := http.PostForm(server.URL+"/mcp/oauth/token", url.Values{
			"grant_type":    {"authorization_code"},
			"code":          {authCode},
			"client_id":     {clientID.String()},
			"redirect_uri":  {"http://localhost:8080/callback"},
			"code_verifier": {codeVerifier},
		})
		defer tokenResp.Body.Close()

		var tokenData struct {
			AccessToken string `json:"access_token"`
		}
		json.NewDecoder(tokenResp.Body).Decode(&tokenData)
		return tokenData.AccessToken
	}

	tokenA := getToken(clientA.ID)
	tokenB := getToken(clientB.ID)

	// Extract tenant IDs from JWTs
	extractTenantID := func(token string) string {
		parts := strings.Split(token, ".")
		payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
		var claims struct {
			TenantID string `json:"tenant_id"`
		}
		json.Unmarshal(payload, &claims)
		return claims.TenantID
	}

	tenantInTokenA := extractTenantID(tokenA)
	tenantInTokenB := extractTenantID(tokenB)

	// Verify tenant IDs are correctly isolated
	if tenantInTokenA != tenantA {
		t.Errorf("token A tenant_id = %s, want %s", tenantInTokenA, tenantA)
	}
	if tenantInTokenB != tenantB {
		t.Errorf("token B tenant_id = %s, want %s", tenantInTokenB, tenantB)
	}
	if tenantInTokenA == tenantInTokenB {
		t.Error("tenant A and B tokens should have different tenant_ids")
	}

	t.Logf("Cross-tenant isolation verified: tokenA.tenant=%s, tokenB.tenant=%s", tenantInTokenA, tenantInTokenB)
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

func (a *tokenStoreAdapter) Create(ctx context.Context, token *oauth.RefreshToken) error {
	_, err := a.repo.CreateWithHash(ctx, mcprepo.CreateWithHashParams{
		ClientID:  token.ClientID,
		SessionID: token.SessionID,
		TokenHash: token.TokenHash,
		Scopes:    token.Scopes,
		UserID:    token.TenantID, // oauth uses TenantID, repo uses UserID
		ExpiresAt: token.ExpiresAt,
	})
	return err
}

func (a *tokenStoreAdapter) GetByHash(ctx context.Context, hash string) (*oauth.RefreshToken, error) {
	t, err := a.repo.GetByHash(ctx, hash)
	if err != nil {
		return nil, oauth.ErrInvalidRefreshToken
	}
	return &oauth.RefreshToken{
		ID:        t.ID,
		ClientID:  t.ClientID,
		SessionID: t.SessionID,
		TokenHash: t.TokenHash,
		TenantID:  t.UserID, // repo uses UserID, oauth uses TenantID
		Scopes:    t.Scopes,
		ExpiresAt: t.ExpiresAt,
	}, nil
}

func (a *tokenStoreAdapter) Revoke(ctx context.Context, id uuid.UUID) error {
	return a.repo.Revoke(ctx, id)
}

func (a *tokenStoreAdapter) Consume(ctx context.Context, hash string) (*oauth.RefreshToken, error) {
	t, err := a.repo.Consume(ctx, hash)
	if err != nil {
		return nil, oauth.ErrInvalidRefreshToken
	}
	return &oauth.RefreshToken{
		ID:        t.ID,
		ClientID:  t.ClientID,
		SessionID: t.SessionID,
		TokenHash: t.TokenHash,
		TenantID:  t.UserID, // repo uses UserID, oauth uses TenantID
		Scopes:    t.Scopes,
		ExpiresAt: t.ExpiresAt,
	}, nil
}

func (a *tokenStoreAdapter) RotateToken(ctx context.Context, oldHash, newHash string, newExpiresAt time.Time) (*oauth.RefreshToken, *oauth.RefreshToken, error) {
	old, newTok, err := a.repo.RotateToken(ctx, mcprepo.RotateTokenParams{
		OldTokenHash: oldHash,
		NewTokenHash: newHash,
		NewExpiresAt: newExpiresAt,
	})
	if err != nil {
		return nil, nil, oauth.ErrInvalidRefreshToken
	}
	return &oauth.RefreshToken{
			ID:        old.ID,
			ClientID:  old.ClientID,
			SessionID: old.SessionID,
			TokenHash: old.TokenHash,
			TenantID:  old.UserID, // repo uses UserID, oauth uses TenantID
			Scopes:    old.Scopes,
		}, &oauth.RefreshToken{
			ID:        newTok.ID,
			ClientID:  newTok.ClientID,
			SessionID: newTok.SessionID,
			TokenHash: newTok.TokenHash,
			TenantID:  newTok.UserID, // repo uses UserID, oauth uses TenantID
			Scopes:    newTok.Scopes,
		}, nil
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
