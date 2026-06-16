// SPDX-License-Identifier: Apache-2.0

package rbac

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestFromContext(t *testing.T) {
	t.Run("returns viewer when no role in context", func(t *testing.T) {
		ctx := context.Background()
		role := FromContext(ctx)
		assert.Equal(t, domain.RoleViewer, role)
	})

	t.Run("returns role from context", func(t *testing.T) {
		ctx := withRole(context.Background(), domain.RoleAdmin)
		role := FromContext(ctx)
		assert.Equal(t, domain.RoleAdmin, role)
	})
}

func TestRoleCache(t *testing.T) {
	t.Run("get returns false for missing key", func(t *testing.T) {
		cache := newRoleCache(0) // TTL doesn't matter for this test
		_, ok := cache.Get("missing")
		assert.False(t, ok)
	})

	t.Run("set and get", func(t *testing.T) {
		cache := newRoleCache(60_000_000_000) // 1 minute
		cache.Set("key", domain.RoleAdmin)
		role, ok := cache.Get("key")
		assert.True(t, ok)
		assert.Equal(t, domain.RoleAdmin, role)
	})

	t.Run("delete removes entry", func(t *testing.T) {
		cache := newRoleCache(60_000_000_000)
		cache.Set("key", domain.RoleAdmin)
		cache.Delete("key")
		_, ok := cache.Get("key")
		assert.False(t, ok)
	})

	t.Run("expired entry returns false", func(t *testing.T) {
		cache := newRoleCache(0) // 0 TTL = instant expiry
		cache.Set("key", domain.RoleAdmin)
		_, ok := cache.Get("key")
		assert.False(t, ok)
	})
}

// Note: Since Middleware expects *tenantmember.Repo, we can't easily mock it
// without interfaces. Instead, we test the role cache and FromContext which
// don't need the repo, and leave integration tests for the full middleware.

func TestWithRole(t *testing.T) {
	ctx := context.Background()

	// Initially no role
	assert.Equal(t, domain.RoleViewer, FromContext(ctx))

	// Add admin role
	ctx = withRole(ctx, domain.RoleAdmin)
	assert.Equal(t, domain.RoleAdmin, FromContext(ctx))

	// Override with member
	ctx = withRole(ctx, domain.RoleMember)
	assert.Equal(t, domain.RoleMember, FromContext(ctx))
}

// TestMiddlewareIntegration tests the middleware with a mock HTTP handler.
// Note: This requires the session context to be set up correctly.
func TestMiddlewareContextPropagation(t *testing.T) {
	// Create a handler that reads the role from context
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := FromContext(r.Context())
		_, _ = w.Write([]byte(role.String()))
	})

	// Without middleware, should return viewer (default)
	t.Run("without middleware returns viewer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, "viewer", rec.Body.String())
	})

	// With role set in context
	t.Run("with role in context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx := withRole(req.Context(), domain.RoleAdmin)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		assert.Equal(t, "admin", rec.Body.String())
	})
}

func TestInvalidateCache(t *testing.T) {
	// Create middleware with a fresh cache
	m := ptrext.Of(Middleware{
		cache: newRoleCache(60_000_000_000),
	})

	// Set a cached role
	key := "tenant-1:oidc_user:user-1"
	m.cache.Set(key, domain.RoleAdmin)

	// Verify it's cached
	role, ok := m.cache.Get(key)
	require.True(t, ok)
	assert.Equal(t, domain.RoleAdmin, role)

	// Invalidate
	m.InvalidateCache("tenant-1", "oidc_user", "user-1")

	// Verify it's gone
	_, ok = m.cache.Get(key)
	assert.False(t, ok)
}

func TestSessionContextIntegration(t *testing.T) {
	// Test that FromContext works correctly with session.AuthCtx
	auth := ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserType: "oidc_user",
		UserID:   "user-1",
	})

	ctx := session.WithAuthCtx(context.Background(), auth)
	retrievedAuth := session.FromContext(ctx)

	require.NotNil(t, retrievedAuth)
	assert.Equal(t, "tenant-1", retrievedAuth.TenantID)
	assert.Equal(t, "oidc_user", retrievedAuth.UserType)
	assert.Equal(t, "user-1", retrievedAuth.UserID)
}
