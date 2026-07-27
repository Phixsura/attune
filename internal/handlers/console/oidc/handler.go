// SPDX-License-Identifier: Apache-2.0

// Package oidc provides HTTP handlers for OIDC SSO authentication.
package oidc

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/crypto"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/service/oidcauth"
)

const (
	oidcStateCookie    = "attune_oidc_state"
	stateTTL           = 10 * time.Minute
	clockSkewTolerance = 30 // seconds
)

// responseWriterAdapter adapts http.ResponseWriter to session.cookieSink.
type responseWriterAdapter struct {
	w http.ResponseWriter
}

func (a responseWriterAdapter) SetCookie(c *http.Cookie) {
	http.SetCookie(a.w, c)
}

// OIDCState holds cross-request state (encrypted in cookie).
type OIDCState struct {
	State        string `json:"s"`
	PKCEVerifier string `json:"v"`
	Nonce        string `json:"n"`
	ReturnURL    string `json:"r"`
	ExpiresAt    int64  `json:"e"`
}

// Handler owns OIDC HTTP endpoints.
type Handler struct {
	svc     *oidcauth.Service
	signer  *session.Signer
	aead    *crypto.AEAD
	baseURL string
}

// NewHandler creates an OIDC handler.
// Returns nil if any required dependency is nil.
func NewHandler(svc *oidcauth.Service, signer *session.Signer, aead *crypto.AEAD, baseURL string) *Handler {
	if svc == nil || signer == nil || aead == nil {
		return nil
	}
	return ptrext.Of(Handler{
		svc:     svc,
		signer:  signer,
		aead:    aead,
		baseURL: strings.TrimRight(baseURL, "/"),
	})
}

// Start handles GET /fb/v1/console/auth/oidc/start
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	const where = "oidc.Handler.Start"
	ctx := r.Context()

	// Generate state (32 bytes)
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		logext.Errorf(ctx, "[%s] rand failed,err:%s", where, err.Error())
		h.redirectError(w, r, "internal_error")
		return
	}
	state := base64.RawURLEncoding.EncodeToString(stateBytes)

	// Generate PKCE verifier (32 bytes → ~43 chars base64)
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		logext.Errorf(ctx, "[%s] rand failed,err:%s", where, err.Error())
		h.redirectError(w, r, "internal_error")
		return
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	// Generate nonce (32 bytes)
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		logext.Errorf(ctx, "[%s] rand failed,err:%s", where, err.Error())
		h.redirectError(w, r, "internal_error")
		return
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)

	// Validate return_url before storing
	returnURL := r.URL.Query().Get("return_url")
	if returnURL != "" && !h.isValidReturnURL(returnURL) {
		logext.Warnf(ctx, "[%s] invalid return_url rejected,url:%s", where, returnURL)
		returnURL = "" // silently ignore, don't block login
	}

	// Store state in encrypted cookie
	oidcState := OIDCState{
		State:        state,
		PKCEVerifier: verifier,
		Nonce:        nonce,
		ReturnURL:    returnURL,
		ExpiresAt:    time.Now().Add(stateTTL).Unix(),
	}
	if err := h.setStateCookie(w, oidcState); err != nil {
		logext.Errorf(ctx, "[%s] cookie failed,err:%s", where, err.Error())
		h.redirectError(w, r, "internal_error")
		return
	}

	// Build authorization URL
	authURL := h.svc.AuthCodeURL(state, verifier, nonce)

	logext.Infof(ctx, "[%s] redirecting to IdP,return_url:%s", where, returnURL)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// Callback handles GET /fb/v1/console/auth/oidc/callback
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	const where = "oidc.Handler.Callback"
	ctx := r.Context()
	loginStart := time.Now()

	// Retrieve state from cookie
	oidcState, err := h.getStateCookie(r)
	if err != nil {
		logext.Warnf(ctx, "[%s] invalid state cookie,err:%s", where, err.Error())
		metrics.OIDCLoginTotal.WithLabelValues("state_invalid").Inc()
		h.redirectError(w, r, "state_expired")
		return
	}

	// Check expiry with tolerance
	if time.Now().Unix() > oidcState.ExpiresAt+clockSkewTolerance {
		logext.Warnf(ctx, "[%s] state expired", where)
		metrics.OIDCLoginTotal.WithLabelValues("state_expired").Inc()
		h.redirectError(w, r, "state_expired")
		return
	}

	// Validate state (constant-time)
	queryState := r.URL.Query().Get("state")
	if !h.svc.ValidateState(queryState, oidcState.State) {
		logext.Warnf(ctx, "[%s] state mismatch", where)
		metrics.OIDCLoginTotal.WithLabelValues("state_mismatch").Inc()
		h.redirectError(w, r, "state_mismatch")
		return
	}

	// Check for IdP error
	if errCode := r.URL.Query().Get("error"); errCode != "" {
		errDesc := r.URL.Query().Get("error_description")
		logext.Warnf(ctx, "[%s] IdP error,code:%s,desc:%s", where, errCode, errDesc)
		metrics.OIDCLoginTotal.WithLabelValues("idp_error").Inc()

		// Map common errors to user-friendly codes
		userCode := "auth_failed"
		if errCode == "access_denied" {
			userCode = "user_cancelled"
		}
		h.redirectError(w, r, userCode)
		return
	}

	// Get authorization code
	code := r.URL.Query().Get("code")
	if code == "" {
		logext.Warnf(ctx, "[%s] missing code", where)
		metrics.OIDCLoginTotal.WithLabelValues("missing_code").Inc()
		h.redirectError(w, r, "auth_failed")
		return
	}

	// Exchange and verify
	claims, err := h.svc.ExchangeAndVerify(ctx, code, oidcState.PKCEVerifier, oidcState.Nonce)
	if err != nil {
		h.redirectError(w, r, "token_failed")
		return
	}

	// Check allowed groups
	if !h.svc.CheckAllowedGroups(claims.Groups) {
		logext.Warnf(ctx, "[%s] user not in allowed groups,sub:%s", where, claims.Subject)
		metrics.OIDCLoginTotal.WithLabelValues("group_denied").Inc()
		h.redirectError(w, r, "not_in_group")
		return
	}

	// Map role
	role := h.svc.MapRole(claims.Groups)

	// Find or create user
	user, err := h.svc.FindOrCreateUser(ctx, claims, role)
	if err != nil {
		metrics.OIDCLoginTotal.WithLabelValues("user_sync_failed").Inc()
		h.redirectError(w, r, "internal_error")
		return
	}

	tenantID := h.svc.ResolveDefaultTenant(ctx)
	if err := h.svc.EnsureMembership(ctx, tenantID, user.ID, role); err != nil {
		metrics.OIDCLoginTotal.WithLabelValues("user_sync_failed").Inc()
		h.redirectError(w, r, "internal_error")
		return
	}

	// Clear state cookie
	h.clearStateCookie(w)

	// Issue session (with UserType)
	if err := h.signer.IssueSessionCookieWithType(responseWriterAdapter{w}, tenantID, user.ID, "oidc"); err != nil {
		logext.Errorf(ctx, "[%s] session issue failed,err:%s", where, err.Error())
		metrics.OIDCLoginTotal.WithLabelValues("session_failed").Inc()
		h.redirectError(w, r, "internal_error")
		return
	}

	// Success
	metrics.OIDCLoginTotal.WithLabelValues("success").Inc()
	metrics.OIDCLoginDuration.Observe(time.Since(loginStart).Seconds())

	returnURL := "/console/"
	if oidcState.ReturnURL != "" && h.isValidReturnURL(oidcState.ReturnURL) {
		returnURL = oidcState.ReturnURL
	}

	logext.Infof(ctx, "[%s] login success,sub:%s,role:%s", where, claims.Subject, role)
	http.Redirect(w, r, returnURL, http.StatusFound)
}

// Health handles GET /fb/v1/console/auth/oidc/health
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"healthy"}`))
}

// ProviderName returns the IdP display name for the login UI.
func (h *Handler) ProviderName() string {
	return h.svc.ProviderName()
}

// OIDCOnly returns whether local login should be hidden.
func (h *Handler) OIDCOnly() bool {
	return h.svc.OIDCOnly()
}

// redirectError redirects to login error page with error code and trace info.
func (h *Handler) redirectError(w http.ResponseWriter, r *http.Request, code string) {
	ctx := r.Context()
	requestID := r.Header.Get("X-Request-Id")

	// Extract trace ID from OTel span context
	var traceID string
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		traceID = span.SpanContext().TraceID().String()
	}

	redirectURL := fmt.Sprintf("/console/login/error?code=%s&request_id=%s&trace_id=%s",
		url.QueryEscape(code), url.QueryEscape(requestID), url.QueryEscape(traceID))
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

// isValidReturnURL validates return URL to prevent open redirect.
func (h *Handler) isValidReturnURL(u string) bool {
	if u == "" {
		return false
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	// Only allow relative paths (no scheme, no host)
	return parsed.Scheme == "" && parsed.Host == "" && strings.HasPrefix(parsed.Path, "/")
}

// setStateCookie encrypts and stores state in cookie.
func (h *Handler) setStateCookie(w http.ResponseWriter, state OIDCState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	encrypted, err := h.aead.Encrypt(data)
	if err != nil {
		return err
	}

	http.SetCookie(w, ptrext.Of(http.Cookie{
		Name:     oidcStateCookie,
		Value:    encrypted,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(stateTTL.Seconds()),
	}))
	return nil
}

// getStateCookie retrieves and decrypts state from cookie.
func (h *Handler) getStateCookie(r *http.Request) (OIDCState, error) {
	cookie, err := r.Cookie(oidcStateCookie)
	if err != nil {
		return OIDCState{}, err
	}

	data, err := h.aead.Decrypt(cookie.Value)
	if err != nil {
		return OIDCState{}, err
	}

	var state OIDCState
	if err := json.Unmarshal(data, &state); err != nil {
		return OIDCState{}, err
	}
	return state, nil
}

// clearStateCookie removes state cookie.
func (h *Handler) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, ptrext.Of(http.Cookie{
		Name:     oidcStateCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}))
}
