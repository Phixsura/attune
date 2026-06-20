// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var (
	ErrInvalidRequest      = errors.New("invalid request")
	ErrInvalidClient       = errors.New("invalid client")
	ErrInvalidRedirectURI  = errors.New("invalid redirect uri")
	ErrInvalidGrant        = errors.New("invalid grant")
	ErrInvalidCode         = errors.New("invalid or expired code")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	ErrInvalidScope        = errors.New("requested scope exceeds client authorization")
	ErrPKCERequired        = errors.New("PKCE required")
	ErrPKCEFailed          = errors.New("PKCE verification failed")
)

// ClientStore provides OAuth client operations.
type ClientStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*Client, error)
	ValidateRedirectURI(ctx context.Context, clientID uuid.UUID, uri string) (bool, error)
}

// CodeStore provides authorization code operations.
type CodeStore interface {
	Create(ctx context.Context, code *AuthCode) error
	Consume(ctx context.Context, code string) (*AuthCode, error)
}

// TokenStore provides refresh token operations.
type TokenStore interface {
	Create(ctx context.Context, token *RefreshToken) error
	GetByHash(ctx context.Context, hash string) (*RefreshToken, error)
	Revoke(ctx context.Context, id uuid.UUID) error
	// Consume atomically retrieves and revokes a refresh token (for rotation).
	Consume(ctx context.Context, hash string) (*RefreshToken, error)
	// RotateToken atomically consumes old token and creates new one in a transaction.
	// Returns (oldToken, newToken, error). If any step fails, neither change commits.
	RotateToken(ctx context.Context, oldHash, newHash string, newExpiresAt time.Time) (*RefreshToken, *RefreshToken, error)
}

// SessionStore provides MCP session operations.
type SessionStore interface {
	Create(ctx context.Context, session *Session) error
	Touch(ctx context.Context, id uuid.UUID) error
	IsActive(ctx context.Context, id uuid.UUID) (bool, error)
}

// Client represents an OAuth client (public client, no secret - uses PKCE).
type Client struct {
	ID           uuid.UUID
	TenantID     string
	Name         string
	RedirectURIs []string
	Scopes       []string
	CreatedAt    time.Time
}

// AuthCode represents an authorization code.
type AuthCode struct {
	Code                string
	ClientID            uuid.UUID
	TenantID            string
	RedirectURI         string
	Scopes              []string
	CodeChallenge       string
	CodeChallengeMethod string
	ExpiresAt           time.Time
	CreatedAt           time.Time
}

// RefreshToken represents a refresh token.
type RefreshToken struct {
	ID        uuid.UUID
	TokenHash string
	ClientID  uuid.UUID
	TenantID  string
	SessionID uuid.UUID
	Scopes    []string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// Session represents an MCP session.
type Session struct {
	ID        uuid.UUID
	ClientID  uuid.UUID
	TenantID  string
	Scopes    []string
	ExpiresAt time.Time
	CreatedAt time.Time
	LastUsed  time.Time
}

// AuthServer handles OAuth 2.1 authorization.
type AuthServer struct {
	clients      ClientStore
	codes        CodeStore
	tokens       TokenStore
	sessions     SessionStore
	signer       *JWTSigner
	baseURL      string
	codeLifetime time.Duration
	accessTTL    time.Duration
	refreshTTL   time.Duration
}

// AuthServerConfig holds auth server configuration.
type AuthServerConfig struct {
	BaseURL      string
	CodeLifetime time.Duration
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
}

// NewAuthServer creates a new OAuth authorization server.
func NewAuthServer(
	clients ClientStore,
	codes CodeStore,
	tokens TokenStore,
	sessions SessionStore,
	signer *JWTSigner,
	cfg AuthServerConfig,
) *AuthServer {
	if cfg.CodeLifetime == 0 {
		cfg.CodeLifetime = 10 * time.Minute
	}
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = 1 * time.Hour
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 7 * 24 * time.Hour // 7 days per industry best practice
	}
	return ptrext.Of(AuthServer{
		clients:      clients,
		codes:        codes,
		tokens:       tokens,
		sessions:     sessions,
		signer:       signer,
		baseURL:      cfg.BaseURL,
		codeLifetime: cfg.CodeLifetime,
		accessTTL:    cfg.AccessTTL,
		refreshTTL:   cfg.RefreshTTL,
	})
}

// AuthorizeRequest represents an authorization request.
type AuthorizeRequest struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	Scope               string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

// AuthorizeResponse represents an authorization response.
type AuthorizeResponse struct {
	Code        string
	State       string
	RedirectURI string
}

// validateAuthRequest performs input length and format validation.
func validateAuthRequest(req AuthorizeRequest) error {
	if len(req.ClientID) > 36 || len(req.RedirectURI) > 2048 ||
		len(req.Scope) > 1024 || len(req.State) > 256 || len(req.CodeChallenge) > 128 {
		return ErrInvalidRequest
	}
	if req.ResponseType != "code" {
		return errors.New("unsupported response_type")
	}
	if req.CodeChallenge == "" {
		return ErrPKCERequired
	}
	if req.CodeChallengeMethod != "S256" {
		return errors.New("code_challenge_method must be S256")
	}
	return nil
}

// Authorize handles the authorization request.
func (s *AuthServer) Authorize(ctx context.Context, req AuthorizeRequest) (*AuthorizeResponse, error) {
	const where = "oauth.AuthServer.Authorize"

	clientID, err := uuid.Parse(req.ClientID)
	if err != nil {
		return nil, ErrInvalidClient
	}

	client, err := s.clients.GetByID(ctx, clientID)
	if err != nil {
		logext.Warnf(ctx, "[%s] client not found,client_id:%s", where, req.ClientID)
		return nil, ErrInvalidClient
	}

	valid, err := s.clients.ValidateRedirectURI(ctx, clientID, req.RedirectURI)
	if err != nil || !valid {
		logext.Warnf(ctx, "[%s] invalid redirect_uri,client_id:%s,uri:%s", where, req.ClientID, req.RedirectURI)
		return nil, ErrInvalidRedirectURI
	}

	if err := validateAuthRequest(req); err != nil {
		return nil, err
	}

	scopes := parseScopes(req.Scope)
	if len(scopes) == 0 {
		scopes = client.Scopes
	} else if !scopesAllowed(scopes, client.Scopes) {
		logext.Warnf(ctx, "[%s] scope exceeds client authorization,client_id:%s,requested:%v,allowed:%v",
			where, req.ClientID, scopes, client.Scopes)
		return nil, ErrInvalidScope
	}

	code := generateRandomString()
	authCode := ptrext.Of(AuthCode{
		Code:                code,
		ClientID:            clientID,
		TenantID:            client.TenantID,
		RedirectURI:         req.RedirectURI,
		Scopes:              scopes,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		ExpiresAt:           time.Now().Add(s.codeLifetime),
		CreatedAt:           time.Now(),
	})

	if err := s.codes.Create(ctx, authCode); err != nil {
		logext.Errorf(ctx, "[%s] code create failed,err:%v", where, err)
		return nil, errors.New("authorization failed")
	}

	logext.Infof(ctx, "[%s] code issued,client_id:%s,scopes:%v", where, req.ClientID, scopes)

	return ptrext.Of(AuthorizeResponse{
		Code:        code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
	}), nil
}

// TokenRequest represents a token request.
type TokenRequest struct {
	GrantType    string
	Code         string
	RedirectURI  string
	ClientID     string
	CodeVerifier string
	RefreshToken string
}

// TokenResponse represents a token response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// Token handles the token request.
func (s *AuthServer) Token(ctx context.Context, req TokenRequest) (*TokenResponse, error) {
	switch req.GrantType {
	case "authorization_code":
		return s.tokenFromCode(ctx, req)
	case "refresh_token":
		return s.tokenFromRefresh(ctx, req)
	default:
		return nil, ErrInvalidGrant
	}
}

func (s *AuthServer) tokenFromCode(ctx context.Context, req TokenRequest) (*TokenResponse, error) {
	const where = "oauth.AuthServer.tokenFromCode"

	authCode, err := s.codes.Consume(ctx, req.Code)
	if err != nil {
		logext.Warnf(ctx, "[%s] code consume failed,err:%v", where, err)
		return nil, ErrInvalidCode
	}

	if time.Now().After(authCode.ExpiresAt) {
		logext.Warnf(ctx, "[%s] code expired,code:%s,expires_at:%v", where, req.Code[:8], authCode.ExpiresAt)
		return nil, ErrInvalidCode
	}

	reqClientID, err := uuid.Parse(req.ClientID)
	if err != nil || reqClientID != authCode.ClientID {
		logext.Warnf(ctx, "[%s] client_id mismatch,req:%s,code:%s", where, req.ClientID, authCode.ClientID)
		return nil, ErrInvalidClient
	}

	if authCode.RedirectURI != req.RedirectURI {
		return nil, ErrInvalidRedirectURI
	}

	if !VerifyCodeChallenge(authCode.CodeChallenge, req.CodeVerifier, authCode.CodeChallengeMethod) {
		logext.Warnf(ctx, "[%s] PKCE verification failed,client_id:%s", where, authCode.ClientID)
		return nil, ErrPKCEFailed
	}

	session := ptrext.Of(Session{
		ClientID:  authCode.ClientID,
		TenantID:  authCode.TenantID,
		Scopes:    authCode.Scopes,
		ExpiresAt: time.Now().Add(s.refreshTTL),
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	})
	if err := s.sessions.Create(ctx, session); err != nil {
		logext.Errorf(ctx, "[%s] session create failed: %v", where, err)
		return nil, ErrInvalidGrant
	}
	// session.ID is set by Create - use it from here on

	accessToken, err := s.signer.Sign(AccessTokenClaims{
		TenantID:  authCode.TenantID,
		ClientID:  authCode.ClientID,
		SessionID: session.ID,
		Scopes:    authCode.Scopes,
	}, s.accessTTL)
	if err != nil {
		logext.Errorf(ctx, "[%s] access token sign failed: %v", where, err)
		return nil, ErrInvalidGrant
	}

	refreshToken := generateRandomString()
	rt := ptrext.Of(RefreshToken{
		ID:        uuid.New(),
		TokenHash: hashToken(refreshToken),
		ClientID:  authCode.ClientID,
		TenantID:  authCode.TenantID,
		SessionID: session.ID,
		Scopes:    authCode.Scopes,
		ExpiresAt: time.Now().Add(s.refreshTTL),
		CreatedAt: time.Now(),
	})
	if err := s.tokens.Create(ctx, rt); err != nil {
		logext.Errorf(ctx, "[%s] refresh token create failed: %v", where, err)
		return nil, ErrInvalidGrant
	}

	logext.Infof(ctx, "[%s] tokens issued,client_id:%s,session_id:%s", where, authCode.ClientID, session.ID)

	return ptrext.Of(TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(s.accessTTL.Seconds()),
		RefreshToken: refreshToken,
		Scope:        strings.Join(authCode.Scopes, " "),
	}), nil
}

func (s *AuthServer) tokenFromRefresh(ctx context.Context, req TokenRequest) (*TokenResponse, error) {
	const where = "oauth.AuthServer.tokenFromRefresh"

	// Client ID is required for public clients (PKCE flow)
	if req.ClientID == "" {
		logext.Warnf(ctx, "[%s] missing client_id", where)
		return nil, ErrInvalidClient
	}
	reqClientID, err := uuid.Parse(req.ClientID)
	if err != nil {
		logext.Warnf(ctx, "[%s] invalid client_id format,req:%s", where, req.ClientID)
		return nil, ErrInvalidClient
	}

	oldHash := hashToken(req.RefreshToken)

	// Validate token exists and get metadata (without consuming yet)
	rt, err := s.tokens.GetByHash(ctx, oldHash)
	if err != nil {
		logext.Warnf(ctx, "[%s] refresh token not found", where)
		return nil, ErrInvalidRefreshToken
	}

	if reqClientID != rt.ClientID {
		logext.Warnf(ctx, "[%s] client_id mismatch,req:%s,token:%s", where, req.ClientID, rt.ClientID)
		return nil, ErrInvalidClient
	}

	// Verify session is still active before issuing new tokens
	active, err := s.sessions.IsActive(ctx, rt.SessionID)
	if err != nil || !active {
		logext.Warnf(ctx, "[%s] session not active,session_id:%s", where, rt.SessionID)
		return nil, ErrInvalidRefreshToken
	}

	// Generate new refresh token
	newRefreshToken := generateRandomString()
	newHash := hashToken(newRefreshToken)
	newExpiresAt := time.Now().Add(s.refreshTTL)

	// Atomic rotation: consume old + create new in single transaction.
	// If new token creation fails, old token is NOT revoked.
	_, _, err = s.tokens.RotateToken(ctx, oldHash, newHash, newExpiresAt)
	if err != nil {
		logext.Errorf(ctx, "[%s] token rotation failed: %v", where, err)
		return nil, ErrInvalidRefreshToken
	}

	// Touch session after successful rotation
	if err := s.sessions.Touch(ctx, rt.SessionID); err != nil {
		logext.Warnf(ctx, "[%s] session touch failed,session_id:%s,err:%v", where, rt.SessionID, err)
	}

	accessToken, err := s.signer.Sign(AccessTokenClaims{
		TenantID:  rt.TenantID,
		ClientID:  rt.ClientID,
		SessionID: rt.SessionID,
		Scopes:    rt.Scopes,
	}, s.accessTTL)
	if err != nil {
		logext.Errorf(ctx, "[%s] access token sign failed: %v", where, err)
		return nil, ErrInvalidGrant
	}

	logext.Infof(ctx, "[%s] tokens refreshed,client_id:%s,session_id:%s", where, rt.ClientID, rt.SessionID)

	return ptrext.Of(TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(s.accessTTL.Seconds()),
		RefreshToken: newRefreshToken,
		Scope:        strings.Join(rt.Scopes, " "),
	}), nil
}

// ServeAuthorize handles GET /authorize.
func (s *AuthServer) ServeAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := AuthorizeRequest{
		ClientID:            q.Get("client_id"),
		RedirectURI:         q.Get("redirect_uri"),
		ResponseType:        q.Get("response_type"),
		Scope:               q.Get("scope"),
		State:               q.Get("state"),
		CodeChallenge:       q.Get("code_challenge"),
		CodeChallengeMethod: q.Get("code_challenge_method"),
	}

	resp, err := s.Authorize(r.Context(), req)
	if err != nil {
		errorRedirect(w, r, req.RedirectURI, req.State, err)
		return
	}

	redirectWithCode(w, r, resp)
}

// ServeToken handles POST /token.
func (s *AuthServer) ServeToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10) // 64KB limit
	if err := r.ParseForm(); err != nil {
		writeTokenError(w, "invalid_request", "failed to parse form")
		return
	}

	req := TokenRequest{
		GrantType:    r.FormValue("grant_type"),
		Code:         r.FormValue("code"),
		RedirectURI:  r.FormValue("redirect_uri"),
		ClientID:     r.FormValue("client_id"),
		CodeVerifier: r.FormValue("code_verifier"),
		RefreshToken: r.FormValue("refresh_token"),
	}

	resp, err := s.Token(r.Context(), req)
	if err != nil {
		code := "invalid_grant"
		desc := "authorization failed"
		switch {
		case errors.Is(err, ErrPKCEFailed):
			code = "invalid_grant"
			desc = "PKCE verification failed"
		case errors.Is(err, ErrInvalidClient):
			code = "invalid_client"
			desc = "invalid client"
		case errors.Is(err, ErrInvalidCode):
			code = "invalid_grant"
			desc = "invalid or expired code"
		case errors.Is(err, ErrInvalidRefreshToken):
			code = "invalid_grant"
			desc = "invalid refresh token"
		case errors.Is(err, ErrInvalidScope):
			code = "invalid_scope"
			desc = "invalid scope"
		}
		writeTokenError(w, code, desc)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}

func parseScopes(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func scopesAllowed(requested, allowed []string) bool {
	allowedSet := make(map[string]bool, len(allowed))
	for _, s := range allowed {
		allowedSet[s] = true
	}
	for _, s := range requested {
		if !allowedSet[s] {
			return false
		}
	}
	return true
}

func generateRandomString() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func errorRedirect(w http.ResponseWriter, r *http.Request, redirectURI, state string, err error) {
	errCode, errDesc := mapAuthError(err)

	// Per RFC 6749 Section 4.1.2.1: If the error is due to invalid client_id or
	// redirect_uri, we MUST NOT redirect. The redirect_uri cannot be trusted
	// until it has been validated against the client's registered URIs.
	// ErrInvalidRequest is also not redirected as it occurs before validation.
	if errors.Is(err, ErrInvalidRequest) || errors.Is(err, ErrInvalidClient) || errors.Is(err, ErrInvalidRedirectURI) {
		http.Error(w, errDesc, http.StatusBadRequest)
		return
	}

	if redirectURI == "" {
		http.Error(w, errDesc, http.StatusBadRequest)
		return
	}

	u, parseErr := url.Parse(redirectURI)
	if parseErr != nil {
		http.Error(w, errDesc, http.StatusBadRequest)
		return
	}

	q := u.Query()
	q.Set("error", errCode)
	q.Set("error_description", errDesc)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()

	http.Redirect(w, r, u.String(), http.StatusFound)
}

func mapAuthError(err error) (code, description string) {
	switch {
	case errors.Is(err, ErrInvalidRequest):
		return "invalid_request", "request parameter validation failed"
	case errors.Is(err, ErrInvalidClient):
		return "invalid_request", "invalid client"
	case errors.Is(err, ErrInvalidRedirectURI):
		return "invalid_request", "invalid redirect_uri"
	case errors.Is(err, ErrPKCERequired):
		return "invalid_request", "PKCE is required"
	case errors.Is(err, ErrInvalidScope):
		return "invalid_scope", "requested scope exceeds authorization"
	default:
		return "access_denied", "authorization denied"
	}
}

func redirectWithCode(w http.ResponseWriter, r *http.Request, resp *AuthorizeResponse) {
	u, err := url.Parse(resp.RedirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}

	q := u.Query()
	q.Set("code", resp.Code)
	if resp.State != "" {
		q.Set("state", resp.State)
	}
	u.RawQuery = q.Encode()

	http.Redirect(w, r, u.String(), http.StatusFound)
}

func writeTokenError(w http.ResponseWriter, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store") // RFC 6749: all token responses MUST NOT be cached
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
		"error":             code,
		"error_description": description,
	})
}
