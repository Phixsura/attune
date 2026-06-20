// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"
	"strings"

	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// AuthMiddleware validates JWT access tokens.
type AuthMiddleware struct {
	signer *oauth.JWTSigner
}

// NewAuthMiddleware creates a new auth middleware.
func NewAuthMiddleware(signer *oauth.JWTSigner) *AuthMiddleware {
	return ptrext.Of(AuthMiddleware{signer: signer})
}

// Wrap wraps an HTTP handler with authentication.
func (m *AuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "invalid authorization header", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := m.signer.Verify(token)
		if err != nil {
			logext.Warnf(ctx, "mcp auth failed: %v", err)
			w.Header().Set("WWW-Authenticate", "Bearer error=\"invalid_token\"")
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}

		ctx = WithClaims(ctx, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireScope creates middleware that requires a specific scope.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if !HasScope(ctx, scope) {
				http.Error(w, "insufficient scope", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
