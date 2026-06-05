// Package console implements Stage B 自服务控制台's HTTP handlers
// (mounted under /fb/v1/console). Endpoints + schemas are locked by
// openapi.yaml in this directory; this Go package is its implementation.
package console

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/logext"
)

// Cookie + header names. attune_session is HttpOnly; csrf token lives
// in the /me response body and is mirrored by SPA into X-CSRF-Token.
const (
	SessionCookieName = "attune_session"
	OAuthStateCookie  = "attune_oauth_state"
	CSRFHeader        = "X-CSRF-Token"

	sessionTTL    = 7 * 24 * time.Hour
	oauthStateTTL = 10 * time.Minute
)

// sessionPayload is what gets HMAC-signed into the cookie. UserID is the
// surrogate uuid from tenant_users (NOT the Lark open_id, kept server-
// side). ExpiresAt is unix seconds — keep this struct small, it's
// base64-roundtripped on every request.
type sessionPayload struct {
	TenantID  string `json:"t"`
	UserID    string `json:"u"`
	ExpiresAt int64  `json:"e"`
}

// AuthCtx is the request-scoped session info populated by the
// RequireSession middleware. Handlers read it via FromContext().
type AuthCtx struct {
	TenantID string
	UserID   string
	ExpAt    time.Time
}

type ctxKey struct{}

// Signer signs and verifies session + CSRF tokens with one HMAC key.
// It is safe for concurrent use.
//
// Insecure=true drops the `Secure` cookie flag — required for plain-HTTP
// dev/preview deployments (e.g. IP-only prod 闭环 before TLS lands).
// MUST be false in any real TLS-fronted production.
type Signer struct {
	key      []byte
	insecure bool
}

func NewSigner(key string, insecure bool) (*Signer, error) {
	if len(key) < 32 {
		return nil, fmt.Errorf("console_session_key must be at least 32 bytes (got %d)", len(key))
	}
	return &Signer{key: []byte(key), insecure: insecure}, nil
}

// Insecure reports whether this Signer was created with the
// HTTP-only escape hatch (drops Secure cookie flag).
func (s *Signer) Insecure() bool { return s.insecure }

// SignSession returns the cookie value: base64(payload).base64(hmac).
func (s *Signer) SignSession(p sessionPayload) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}
	enc := base64.RawURLEncoding.EncodeToString(raw)
	mac := s.hmac(enc)
	return enc + "." + mac, nil
}

// VerifySession reverses SignSession. Returns the payload on success.
func (s *Signer) VerifySession(cookie string) (*sessionPayload, error) {
	parts := strings.SplitN(cookie, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("malformed session")
	}
	want := s.hmac(parts[0])
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return nil, errors.New("session signature mismatch")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode session payload: %w", err)
	}
	var p sessionPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("unmarshal session payload: %w", err)
	}
	if time.Now().Unix() > p.ExpiresAt {
		return nil, errors.New("session expired")
	}
	return &p, nil
}

// CSRFToken derives a token from the user id alone. Stable over the
// session's lifetime (no per-request rotation — see openapi.yaml note
// on SessionMe.csrf_token); the SPA stores it in memory and sends it on
// every non-GET via X-CSRF-Token.
func (s *Signer) CSRFToken(userID string) string {
	return s.hmac("csrf:" + userID)
}

// VerifyCSRF returns true if header matches expected for this user.
func (s *Signer) VerifyCSRF(userID, header string) bool {
	return header != "" && hmac.Equal([]byte(s.CSRFToken(userID)), []byte(header))
}

func (s *Signer) hmac(input string) string {
	h := hmac.New(sha256.New, s.key)
	h.Write([]byte(input))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// IssueSessionCookie writes the session cookie on a response. Used by
// the OAuth callback after a successful install/login.
func (s *Signer) IssueSessionCookie(w http.ResponseWriter, tenantID, userID string) error {
	val, err := s.SignSession(sessionPayload{
		TenantID:  tenantID,
		UserID:    userID,
		ExpiresAt: time.Now().Add(sessionTTL).Unix(),
	})
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		Secure:   !s.insecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionTTL),
	})
	return nil
}

// ClearSessionCookie expires the session cookie. Used by POST /logout.
// Mirrors IssueSessionCookie's Secure setting so the browser matches
// the cookie identity (path/secure must match for SetCookie to overwrite).
func (s *Signer) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   !s.insecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// RequireSession is the chi-style middleware that gates all /console
// endpoints except /install/*. Sets AuthCtx in request context on
// success; returns 401 (with JSON error envelope) on failure.
func (s *Signer) RequireSession(next http.Handler) http.Handler {
	const where = "console.RequireSession"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ck, err := r.Cookie(SessionCookieName)
		if err != nil {
			logext.Warnf(ctx, "[%s] reject: missing cookie,path:%s", where, r.URL.Path)
			writeError(w, http.StatusUnauthorized, "unauthorized", "未登录或 session 已过期")
			return
		}
		p, err := s.VerifySession(ck.Value)
		if err != nil {
			logext.Warnf(ctx, "[%s] reject: verify session failed,path:%s,err:%s",
				where, r.URL.Path, err.Error())
			writeError(w, http.StatusUnauthorized, "unauthorized", "session 校验失败")
			return
		}
		// CSRF check for state-changing methods. GET/HEAD bypass.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !s.VerifyCSRF(p.UserID, r.Header.Get(CSRFHeader)) {
				logext.Warnf(ctx, "[%s] reject: csrf invalid,path:%s,user_id:%s",
					where, r.URL.Path, p.UserID)
				writeError(w, http.StatusForbidden, "csrf_invalid", "CSRF token 无效")
				return
			}
		}
		newCtx := context.WithValue(r.Context(), ctxKey{}, &AuthCtx{
			TenantID: p.TenantID,
			UserID:   p.UserID,
			ExpAt:    time.Unix(p.ExpiresAt, 0),
		})
		next.ServeHTTP(w, r.WithContext(newCtx))
	})
}

// FromContext returns the auth ctx from a request handled by
// RequireSession. Panics if missing — that's a programming error
// (handler attached outside the middleware).
func FromContext(ctx context.Context) *AuthCtx {
	v, _ := ctx.Value(ctxKey{}).(*AuthCtx)
	if v == nil {
		panic("console: AuthCtx missing — handler not behind RequireSession")
	}
	return v
}

// writeError emits the {code, message, request_id} envelope from
// openapi.yaml.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    code,
		"message": msg,
	})
}
