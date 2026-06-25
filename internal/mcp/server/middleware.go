// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/mcp/oauth"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ClientValidator checks if a client is still valid (not revoked).
type ClientValidator interface {
	IsRevoked(ctx context.Context, clientID uuid.UUID) (bool, error)
}

// SessionValidator checks if a session is still active.
type SessionValidator interface {
	IsActive(ctx context.Context, sessionID uuid.UUID) (bool, error)
}

// AuthMiddleware validates JWT access tokens.
type AuthMiddleware struct {
	signer              *oauth.JWTSigner
	clients             ClientValidator
	sessions            SessionValidator
	resourceMetadataURL string
}

// NewAuthMiddleware creates a new auth middleware.
func NewAuthMiddleware(
	signer *oauth.JWTSigner,
	clients ClientValidator,
	sessions SessionValidator,
	resourceMetadataURL string,
) *AuthMiddleware {
	return ptrext.Of(AuthMiddleware{
		signer:              signer,
		clients:             clients,
		sessions:            sessions,
		resourceMetadataURL: strings.TrimSpace(resourceMetadataURL),
	})
}

// Wrap wraps an HTTP handler with authentication.
func (m *AuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("WWW-Authenticate", BearerChallenge(m.resourceMetadataURL))
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			w.Header().Set("WWW-Authenticate", BearerChallenge(m.resourceMetadataURL))
			http.Error(w, "invalid authorization header", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := m.signer.Verify(token)
		if err != nil {
			logext.Warnf(ctx, "mcp auth failed: %v", err)
			w.Header().Set("WWW-Authenticate", BearerChallenge(
				m.resourceMetadataURL,
				BearerChallengeParam("error", "invalid_token"),
			))
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}

		if m.clients != nil {
			revoked, err := m.clients.IsRevoked(ctx, claims.ClientID)
			if err != nil {
				logext.Warnf(ctx, "mcp client check failed: %v", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if revoked {
				logext.Warnf(ctx, "mcp client revoked: %s", claims.ClientID)
				w.Header().Set("WWW-Authenticate", BearerChallenge(
					m.resourceMetadataURL,
					BearerChallengeParam("error", "invalid_token"),
				))
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}
		}

		if m.sessions != nil {
			active, err := m.sessions.IsActive(ctx, claims.SessionID)
			if err != nil {
				logext.Warnf(ctx, "mcp session check failed: %v", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if !active {
				logext.Warnf(ctx, "mcp session closed: %s", claims.SessionID)
				w.Header().Set("WWW-Authenticate", BearerChallenge(
					m.resourceMetadataURL,
					BearerChallengeParam("error", "invalid_token"),
				))
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}
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
				w.Header().Set("WWW-Authenticate", BearerChallenge("",
					BearerChallengeParam("error", "insufficient_scope"),
					BearerChallengeParam("scope", scope),
				))
				http.Error(w, "insufficient scope", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BearerChallengeParam formats one Bearer auth-param.
func BearerChallengeParam(key, value string) string {
	return fmt.Sprintf(`%s="%s"`, key, value)
}

// BearerChallenge builds a WWW-Authenticate Bearer challenge.
func BearerChallenge(resourceMetadataURL string, params ...string) string {
	all := make([]string, 0, len(params)+1)
	if resourceMetadataURL = strings.TrimSpace(resourceMetadataURL); resourceMetadataURL != "" {
		all = append(all, BearerChallengeParam("resource_metadata", resourceMetadataURL))
	}
	all = append(all, params...)
	if len(all) == 0 {
		return "Bearer"
	}
	return "Bearer " + strings.Join(all, ", ")
}
