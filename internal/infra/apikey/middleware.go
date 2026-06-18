// Package apikey is the HTTP-layer adapter for external API key
// authentication: a chi-compatible middleware plus context-key helpers
// for handlers to read the authenticated identity. The credential
// store (Generate / Issue / Lookup) lives in service.APIKeys; this
// package is intentionally thin and HTTP-only so service can be swapped
// or unit-tested without spinning up an HTTP server.
package apikey

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

// Verifier is the dependency middleware needs from the service layer.
// Implemented by *service.APIKeys; declared here as an interface so
// infra/apikey doesn't depend on internal/service (one-way arrows).
type Verifier interface {
	Lookup(ctx context.Context, raw string) (tenantID string, keyID uuid.UUID, err error)
	LookupWithScopes(ctx context.Context, raw string) (tenantID string, keyID uuid.UUID, scopes []domain.Scope, err error)
	LookupWithScopesAndIP(ctx context.Context, raw, clientIP string) (tenantID string, keyID uuid.UUID, scopes []domain.Scope, err error)
}

// AuthCtx is the request-scoped API key identity populated by Middleware.
// Handlers can read it via FromContext.
type AuthCtx struct {
	TenantID string
	KeyID    uuid.UUID
	Scopes   []domain.Scope
}

type ctxKey int

const (
	ctxTenantID ctxKey = iota
	ctxKeyID
	ctxAuth
)

// Middleware authenticates requests using the X-API-Key header. The
// authenticated tenant id and key id are stored in the request context
// and read back by handlers via TenantIDFromContext / KeyIDFromContext.
//
// Failure modes, all emitting the unified ErrorResponse envelope
// {code, message, requestId}:
// - missing / malformed header → 401 code=UNAUTHORIZED message="missing or malformed api key"
// - unknown / revoked key → 401 code=UNAUTHORIZED message="invalid api key"
// - expired key → 401 code=UNAUTHORIZED message="api key expired"
// - IP not in allowlist → 403 code=FORBIDDEN message="ip not in allowlist"
// - unexpected DB / IO failure → 500 code=INTERNAL message="api key lookup failed"
//
// The previous spelling `unauthenticated` was an outlier vs the console
// envelope's `unauthorized` — normalized to the proto-defined UNAUTHORIZED
// during the dispatcher migration (proposal 2026-06-09-http-dispatcher.md Commit 5 audit).
// Middleware authenticates with no trusted reverse proxy in front of attune:
// X-Forwarded-For is ignored and the IP allowlist matches the direct peer. Use
// MiddlewareWithProxies when attune sits behind a known proxy tier.
func Middleware(v Verifier) func(http.Handler) http.Handler {
	return MiddlewareWithProxies(v, 0)
}

// MiddlewareWithProxies is Middleware parameterized by the number of trusted
// reverse proxies (security.trusted_proxy_hops). The client IP used for the API
// key IP allowlist is resolved from X-Forwarded-For only that many hops in;
// trustedProxyHops <= 0 ignores the header entirely so a client on a direct
// connection cannot spoof an allowlisted source IP.
func MiddlewareWithProxies(v Verifier, trustedProxyHops int) func(http.Handler) http.Handler {
	const where = "apikey.Middleware"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			raw := r.Header.Get("X-API-Key")
			if raw == "" || !strings.HasPrefix(raw, domain.APIKeyPrefix) {
				logext.Warnf(ctx, "[%s] reject: missing/malformed key,path:%s", where, r.URL.Path)
				dispatcher.Reject(ctx, w, http.StatusUnauthorized,
					attunev1.ErrorCode_UNAUTHORIZED, "missing or malformed api key")
				return
			}
			clientIP := nethardening.ClientIP(r, trustedProxyHops)
			tid, kid, scopes, err := v.LookupWithScopesAndIP(ctx, raw, clientIP)
			if err != nil {
				status := http.StatusUnauthorized
				code := attunev1.ErrorCode_UNAUTHORIZED
				msg := "invalid api key"
				switch {
				case errors.Is(err, domain.ErrAPIKeyExpired):
					msg = "api key expired"
					metrics.APIKeyExpiredTotal.Inc()
					logext.Warnf(ctx, "[%s] reject: expired key,path:%s", where, r.URL.Path)
				case errors.Is(err, domain.ErrIPNotAllowed):
					status = http.StatusForbidden
					code = attunev1.ErrorCode_FORBIDDEN
					msg = "ip not in allowlist"
					metrics.APIKeyIPDeniedTotal.Inc()
					logext.Warnf(ctx, "[%s] reject: IP not allowed,path:%s,ip:%s", where, r.URL.Path, clientIP)
				case errors.Is(err, domain.ErrInvalidAPIKey):
					logext.Warnf(ctx, "[%s] reject: invalid key,path:%s", where, r.URL.Path)
				default:
					status = http.StatusInternalServerError
					code = attunev1.ErrorCode_INTERNAL
					msg = "api key lookup failed"
					logext.Errorf(ctx, "[%s] Lookup failed,path:%s,err:%+v",
						where, r.URL.Path, err.Error())
				}
				dispatcher.Reject(ctx, w, status, code, msg)
				return
			}
			newCtx := context.WithValue(r.Context(), ctxTenantID, tid)
			newCtx = context.WithValue(newCtx, ctxKeyID, kid)
			newCtx = context.WithValue(newCtx, ctxAuth, ptrext.Of(AuthCtx{
				TenantID: tid,
				KeyID:    kid,
				Scopes:   scopes,
			}))
			next.ServeHTTP(w, r.WithContext(newCtx))
		})
	}
}

// IsBrowserUserAgent checks if the User-Agent indicates a browser.
// Used to prevent secret keys from being used in frontend code.
func IsBrowserUserAgent(ua string) bool {
	ua = strings.ToLower(ua)
	browserPatterns := []string{
		"mozilla/",
		"chrome/",
		"safari/",
		"firefox/",
		"edge/",
		"opera/",
		"trident/",
		"msie ",
	}
	for _, pattern := range browserPatterns {
		if strings.Contains(ua, pattern) {
			return true
		}
	}
	return false
}

// FromContext returns the authenticated API key identity from ctx.
// Panics if missing — that's a programming error (handler not behind Middleware).
func FromContext(ctx context.Context) *AuthCtx {
	v, _ := ctx.Value(ctxAuth).(*AuthCtx)
	if v == nil {
		panic("apikey: AuthCtx missing — handler not behind Middleware")
	}
	return v
}

// FromContextSafe returns the authenticated API key identity from ctx.
// Returns nil, false if not present (session request).
func FromContextSafe(ctx context.Context) (*AuthCtx, bool) {
	v, ok := ctx.Value(ctxAuth).(*AuthCtx)
	return v, ok && v != nil
}

// WithAuthForTest creates a context with API key auth for testing.
// Only use in tests.
func WithAuthForTest(ctx context.Context, tenantID string, keyID string, scopes []domain.Scope) context.Context {
	id, _ := uuid.Parse(keyID)
	return context.WithValue(ctx, ctxAuth, ptrext.Of(AuthCtx{
		TenantID: tenantID,
		KeyID:    id,
		Scopes:   scopes,
	}))
}

// TenantIDFromContext returns the authenticated tenant id, if any.
// Tenant ids are TEXT (UUID strings stored as text — see migration 001).
func TenantIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxTenantID).(string)
	return v, ok
}

// KeyIDFromContext returns the authenticated api-key id, if any.
func KeyIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(ctxKeyID).(uuid.UUID)
	return v, ok
}

// RequireScope returns middleware that checks API key scopes.
// Must be used after Middleware. Rejects with 403 if scope missing.
func RequireScope(required domain.Scope) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			auth := FromContext(ctx)
			if !domain.HasScope(auth.Scopes, required) {
				metrics.APIKeyScopeDeniedTotal.WithLabelValues(string(required)).Inc()
				logext.Warnf(ctx, "[scope] deny,key_id:%s,required:%s", auth.KeyID, required)
				dispatcher.Reject(ctx, w, http.StatusForbidden,
					attunev1.ErrorCode_FORBIDDEN,
					fmt.Sprintf("missing scope: %s", required))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
