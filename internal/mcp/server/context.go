// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/mcp/oauth"
)

type contextKey int

const (
	ctxKeyClaims contextKey = iota
)

// WithClaims adds the access token claims to the context.
func WithClaims(ctx context.Context, claims *oauth.AccessTokenClaims) context.Context {
	return context.WithValue(ctx, ctxKeyClaims, claims)
}

// ClaimsFromContext retrieves the access token claims from the context.
func ClaimsFromContext(ctx context.Context) *oauth.AccessTokenClaims {
	claims, _ := ctx.Value(ctxKeyClaims).(*oauth.AccessTokenClaims)
	return claims
}

// TenantIDFromContext retrieves the tenant ID from the context claims.
func TenantIDFromContext(ctx context.Context) string {
	claims := ClaimsFromContext(ctx)
	if claims == nil {
		return ""
	}
	return claims.TenantID
}

// ClientIDFromContext retrieves the client ID from the context claims.
func ClientIDFromContext(ctx context.Context) uuid.UUID {
	claims := ClaimsFromContext(ctx)
	if claims == nil {
		return uuid.Nil
	}
	return claims.ClientID
}

// SessionIDFromContext retrieves the session ID from the context claims.
func SessionIDFromContext(ctx context.Context) uuid.UUID {
	claims := ClaimsFromContext(ctx)
	if claims == nil {
		return uuid.Nil
	}
	return claims.SessionID
}

// ScopesFromContext retrieves the scopes from the context claims.
func ScopesFromContext(ctx context.Context) []string {
	claims := ClaimsFromContext(ctx)
	if claims == nil {
		return nil
	}
	return claims.Scopes
}

// HasScope checks if the context has the given scope.
func HasScope(ctx context.Context, scope string) bool {
	scopes := ScopesFromContext(ctx)
	for _, s := range scopes {
		if s == scope {
			return true
		}
		if s == "mcp:write" && scope == "mcp:read" {
			return true
		}
	}
	return false
}
