// Package session holds the cookie-based session machinery shared by
// every console handler: cookie names, the Signer (HMAC), AuthCtx,
// RequireSession middleware, and FromContext.
//
// Lives under handlers/console/internal/ so it's only importable from
// within handlers/console/. Below the handler subpackages in the import
// graph: each handler imports session (and respond), but neither
// imports any handler. The root handlers/console package wires them
// together via router.go.
package session

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

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Cookie + header names. attune_session is HttpOnly; csrf token lives
// in the /me response body and is mirrored by SPA into X-CSRF-Token.
const (
	SessionCookieName = "attune_session"
	CSRFHeader        = "X-CSRF-Token"

	SessionTTL = 7 * 24 * time.Hour
)

// Payload is what gets HMAC-signed into the cookie. UserID is the
// surrogate uuid from tenant_users (NOT any upstream open_id, kept server-
// side). ExpiresAt is unix seconds — keep this struct small, it's
// base64-roundtripped on every request.
type Payload struct {
	TenantID  string `json:"t"`
	UserID    string `json:"u"`
	UserType  string `json:"y,omitempty"` // "admin" | "oidc" (empty = admin for backwards compat)
	IssuedAt  int64  `json:"i,omitempty"`
	StepUpAt  int64  `json:"s,omitempty"`
	ExpiresAt int64  `json:"e"`
}

// AuthCtx is the request-scoped session info populated by the
// RequireSession middleware. Handlers read it via FromContext().
type AuthCtx struct {
	TenantID string
	UserID   string
	UserType string // "admin" | "oidc" (empty = admin)
	IssuedAt time.Time
	StepUpAt *time.Time
	ExpAt    time.Time
}

type ctxKey struct{}

// Signer signs and verifies session + CSRF tokens with one HMAC key.
// It is safe for concurrent use.
//
// Session cookies are always `Secure` (review H1 / spec §Security).
// Plain-HTTP deployments must front attune with a reverse-proxy
// terminating TLS; the previous `Insecure` escape hatch was a real
// risk of being left on in production and is removed.
type Signer struct {
	key []byte
}

// NewSigner builds a Signer from a >=32-byte secret.
func NewSigner(key string) (*Signer, error) {
	if len(key) < 32 {
		return nil, fmt.Errorf("console_session_key must be at least 32 bytes (got %d)", len(key))
	}
	return ptrext.Of(Signer{key: []byte(key)}), nil
}

// SignSession returns the cookie value: base64(payload).base64(hmac).
func (s *Signer) SignSession(p Payload) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("marshal session: %w", err)
	}
	enc := base64.RawURLEncoding.EncodeToString(raw)
	mac := s.hmac(enc)
	return enc + "." + mac, nil
}

// VerifySession reverses SignSession. Returns the payload on success.
func (s *Signer) VerifySession(cookie string) (*Payload, error) {
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
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("unmarshal session payload: %w", err)
	}
	if time.Now().Unix() > p.ExpiresAt {
		return nil, errors.New("session expired")
	}
	return ptrext.Of(p), nil
}

// CSRFToken derives a token from the user id alone. Stable over the
// session's lifetime (no per-request rotation); the SPA stores it in
// memory and sends it on every non-GET via X-CSRF-Token.
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

type cookieSink interface {
	SetCookie(*http.Cookie)
}

// IssueSessionCookie writes the session cookie after a successful login (admin user type).
func (s *Signer) IssueSessionCookie(sink cookieSink, tenantID, userID string) error {
	return s.IssueSessionCookieWithType(sink, tenantID, userID, "")
}

// IssueSessionCookieWithType writes the session cookie with explicit user type.
func (s *Signer) IssueSessionCookieWithType(sink cookieSink, tenantID, userID, userType string) error {
	now := time.Now()
	val, err := s.SignSession(Payload{
		TenantID:  tenantID,
		UserID:    userID,
		UserType:  userType,
		IssuedAt:  now.Unix(),
		StepUpAt:  now.Unix(),
		ExpiresAt: now.Add(SessionTTL).Unix(),
	})
	if err != nil {
		return err
	}
	sink.SetCookie(ptrext.Of(http.Cookie{
		Name:     SessionCookieName,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  now.Add(SessionTTL),
	}))
	return nil
}

func (s *Signer) RefreshStepUpCookie(sink cookieSink, auth *AuthCtx) error {
	now := time.Now()
	issuedAt := auth.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = now
	}
	val, err := s.SignSession(Payload{
		TenantID:  auth.TenantID,
		UserID:    auth.UserID,
		UserType:  auth.UserType,
		IssuedAt:  issuedAt.Unix(),
		StepUpAt:  now.Unix(),
		ExpiresAt: auth.ExpAt.Unix(),
	})
	if err != nil {
		return err
	}
	sink.SetCookie(ptrext.Of(http.Cookie{
		Name:     SessionCookieName,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  auth.ExpAt,
	}))
	return nil
}

// ClearSessionCookie expires the session cookie. Used by POST /logout.
func (s *Signer) ClearSessionCookie(sink cookieSink) {
	sink.SetCookie(ptrext.Of(http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}))
}

// RequireSession is the chi-style middleware that gates all /console
// endpoints except /install/*. Sets AuthCtx in request context on
// success; returns dispatcher-owned error envelopes on failure.
func (s *Signer) RequireSession(next http.Handler) http.Handler {
	const where = "session.RequireSession"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		ck, err := r.Cookie(SessionCookieName)
		if err != nil {
			logext.Warnf(ctx, "[%s] reject: missing cookie,path:%s", where, r.URL.Path)
			dispatcher.Reject(ctx, w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "not logged in or session expired")
			return
		}
		p, err := s.VerifySession(ck.Value)
		if err != nil {
			logext.Warnf(ctx, "[%s] reject: verify session failed,path:%s,err:%s",
				where, r.URL.Path, err.Error())
			dispatcher.Reject(ctx, w, http.StatusUnauthorized, attunev1.ErrorCode_UNAUTHORIZED, "session verification failed")
			return
		}
		// CSRF check for state-changing methods. GET/HEAD bypass.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !s.VerifyCSRF(p.UserID, r.Header.Get(CSRFHeader)) {
				logext.Warnf(ctx, "[%s] reject: csrf invalid,path:%s,user_id:%s",
					where, r.URL.Path, p.UserID)
				dispatcher.Reject(ctx, w, http.StatusForbidden, attunev1.ErrorCode_CSRF_INVALID, "invalid CSRF token")
				return
			}
		}
		auth := ptrext.Of(AuthCtx{
			TenantID: p.TenantID,
			UserID:   p.UserID,
			UserType: p.UserType,
			IssuedAt: time.Unix(p.IssuedAt, 0),
			ExpAt:    time.Unix(p.ExpiresAt, 0),
		})
		if p.StepUpAt > 0 {
			auth.StepUpAt = ptrext.Of(time.Unix(p.StepUpAt, 0))
		}
		newCtx := context.WithValue(r.Context(), ctxKey{}, auth)
		next.ServeHTTP(w, r.WithContext(newCtx))
	})
}

// FromContext returns the auth ctx from a request handled by
// RequireSession. Panics if missing — that's a programming error
// (handler attached outside the middleware).
func FromContext(ctx context.Context) *AuthCtx {
	v, _ := ctx.Value(ctxKey{}).(*AuthCtx)
	if v == nil {
		panic("session: AuthCtx missing — handler not behind RequireSession")
	}
	return v
}

// WithAuthCtx attaches an AuthCtx to ctx. Exposed so unit tests can
// drive handler methods directly without going through the middleware.
func WithAuthCtx(ctx context.Context, ac *AuthCtx) context.Context {
	return context.WithValue(ctx, ctxKey{}, ac)
}
