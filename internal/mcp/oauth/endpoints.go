// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
	ErrInvalidClient       = errors.New("invalid client")
	ErrInvalidRedirectURI  = errors.New("invalid redirect uri")
	ErrInvalidGrant        = errors.New("invalid grant")
	ErrInvalidCode         = errors.New("invalid or expired code")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
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
}

// SessionStore provides MCP session operations.
type SessionStore interface {
	Create(ctx context.Context, session *Session) error
	Touch(ctx context.Context, id uuid.UUID) error
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
		cfg.RefreshTTL = 30 * 24 * time.Hour
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

// Authorize handles the authorization request.
func (s *AuthServer) Authorize(ctx context.Context, req AuthorizeRequest) (*AuthorizeResponse, error) {
	const where = "oauth.AuthServer.Authorize"

	if req.ResponseType != "code" {
		return nil, errors.New("unsupported response_type")
	}

	if req.CodeChallenge == "" {
		return nil, ErrPKCERequired
	}
	if req.CodeChallengeMethod != "S256" {
		return nil, errors.New("code_challenge_method must be S256")
	}

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

	scopes := parseScopes(req.Scope)
	if len(scopes) == 0 {
		scopes = client.Scopes
	}

	code := generateCode()
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
		return nil, err
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

	if authCode.RedirectURI != req.RedirectURI {
		return nil, ErrInvalidRedirectURI
	}

	if !VerifyCodeChallenge(authCode.CodeChallenge, req.CodeVerifier, authCode.CodeChallengeMethod) {
		logext.Warnf(ctx, "[%s] PKCE verification failed,client_id:%s", where, authCode.ClientID)
		return nil, ErrPKCEFailed
	}

	sessionID := uuid.New()
	session := ptrext.Of(Session{
		ID:        sessionID,
		ClientID:  authCode.ClientID,
		TenantID:  authCode.TenantID,
		Scopes:    authCode.Scopes,
		ExpiresAt: time.Now().Add(s.refreshTTL),
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	})
	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, err
	}

	accessToken, err := s.signer.Sign(AccessTokenClaims{
		TenantID:  authCode.TenantID,
		ClientID:  authCode.ClientID,
		SessionID: sessionID,
		Scopes:    authCode.Scopes,
	}, s.accessTTL)
	if err != nil {
		return nil, err
	}

	refreshToken := generateToken()
	rt := ptrext.Of(RefreshToken{
		ID:        uuid.New(),
		TokenHash: hashToken(refreshToken),
		ClientID:  authCode.ClientID,
		TenantID:  authCode.TenantID,
		SessionID: sessionID,
		Scopes:    authCode.Scopes,
		ExpiresAt: time.Now().Add(s.refreshTTL),
		CreatedAt: time.Now(),
	})
	if err := s.tokens.Create(ctx, rt); err != nil {
		return nil, err
	}

	logext.Infof(ctx, "[%s] tokens issued,client_id:%s,session_id:%s", where, authCode.ClientID, sessionID)

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

	hash := hashToken(req.RefreshToken)
	rt, err := s.tokens.GetByHash(ctx, hash)
	if err != nil {
		logext.Warnf(ctx, "[%s] refresh token not found", where)
		return nil, ErrInvalidRefreshToken
	}

	if time.Now().After(rt.ExpiresAt) {
		return nil, ErrInvalidRefreshToken
	}

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
		return nil, err
	}

	logext.Infof(ctx, "[%s] access token refreshed,client_id:%s,session_id:%s", where, rt.ClientID, rt.SessionID)

	return ptrext.Of(TokenResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(s.accessTTL.Seconds()),
		Scope:       strings.Join(rt.Scopes, " "),
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
		if errors.Is(err, ErrPKCEFailed) {
			code = "invalid_grant"
		} else if errors.Is(err, ErrInvalidClient) {
			code = "invalid_client"
		}
		writeTokenError(w, code, err.Error())
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

func generateCode() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func generateToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(token string) string {
	return GenerateCodeChallenge(token)
}

func errorRedirect(w http.ResponseWriter, r *http.Request, redirectURI, state string, err error) {
	if redirectURI == "" {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	u, parseErr := url.Parse(redirectURI)
	if parseErr != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	q := u.Query()
	q.Set("error", "access_denied")
	q.Set("error_description", err.Error())
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()

	http.Redirect(w, r, u.String(), http.StatusFound)
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
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
		"error":             code,
		"error_description": description,
	})
}
