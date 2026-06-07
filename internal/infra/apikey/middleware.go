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
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/logext"
	"github.com/Phixsura/attune/internal/respond"
)

// Error codes returned in the unified ErrorResponse envelope.
// Kept here so handlers / clients can grep one place for the contract.
const (
	errCodeMissingAPIKey = "unauthenticated"
	errCodeInvalidAPIKey = "unauthenticated"
	errCodeLookupFailed  = "internal"
)

// Verifier is the dependency middleware needs from the service layer.
// Implemented by *service.APIKeys; declared here as an interface so
// infra/apikey doesn't depend on internal/service (one-way arrows).
type Verifier interface {
	Lookup(ctx context.Context, raw string) (tenantID string, keyID uuid.UUID, err error)
}

type ctxKey int

const (
	ctxTenantID ctxKey = iota
	ctxKeyID
)

// Middleware authenticates requests using the X-API-Key header. The
// authenticated tenant id and key id are stored in the request context
// and read back by handlers via TenantIDFromContext / KeyIDFromContext.
//
// Failure modes, all emitting the unified ErrorResponse envelope
// {code, message, requestId}:
// - missing / malformed header → 401 code=unauthenticated message="missing or malformed api key"
// - unknown / revoked key → 401 code=unauthenticated message="invalid api key"
// - unexpected DB / IO failure → 500 code=internal message="api key lookup failed"
func Middleware(v Verifier) func(http.Handler) http.Handler {
	const where = "apikey.Middleware"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			raw := r.Header.Get("X-API-Key")
			if raw == "" || !strings.HasPrefix(raw, domain.APIKeyPrefix) {
				logext.Warnf(ctx, "[%s] reject: missing/malformed key,path:%s", where, r.URL.Path)
				respond.Error(ctx, w, http.StatusUnauthorized,
					errCodeMissingAPIKey, "missing or malformed api key")
				return
			}
			tid, kid, err := v.Lookup(r.Context(), raw)
			if err != nil {
				status := http.StatusUnauthorized
				code := errCodeInvalidAPIKey
				msg := "invalid api key"
				if !errors.Is(err, domain.ErrInvalidAPIKey) {
					status = http.StatusInternalServerError
					code = errCodeLookupFailed
					msg = "api key lookup failed"
					logext.Errorf(ctx, "[%s] Lookup failed,path:%s,err:%+v",
						where, r.URL.Path, err.Error())
				} else {
					logext.Warnf(ctx, "[%s] reject: invalid key,path:%s", where, r.URL.Path)
				}
				respond.Error(ctx, w, status, code, msg)
				return
			}
			newCtx := context.WithValue(r.Context(), ctxTenantID, tid)
			newCtx = context.WithValue(newCtx, ctxKeyID, kid)
			// hot path: success not logged (CLAUDE.md: tight-loop / hot-path silent)
			next.ServeHTTP(w, r.WithContext(newCtx))
		})
	}
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
