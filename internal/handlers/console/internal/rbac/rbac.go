// SPDX-License-Identifier: Apache-2.0

// Package rbac provides role-based access control middleware for console handlers.
package rbac

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/tenantmember"
)

type roleCtxKey struct{}

// roleStore reads a member's effective role. *tenantmember.Repo satisfies
// it; the interface lets tests inject a fake without a database.
type roleStore interface {
	GetRole(ctx context.Context, tenantID, memberType, userID string) (domain.Role, error)
}

// Middleware provides role-based access control for console routes.
type Middleware struct {
	members roleStore
	cache   *roleCache
}

// NewMiddleware creates a new RBAC middleware.
func NewMiddleware(members roleStore) *Middleware {
	return ptrext.Of(Middleware{
		members: members,
		cache:   newRoleCache(5 * time.Minute),
	})
}

// RequireRole creates middleware that checks the user has at least the required role.
func (m *Middleware) RequireRole(required domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			auth := session.FromContext(ctx)

			cacheKey := fmt.Sprintf("%s:%s:%s", auth.TenantID, auth.UserType, auth.UserID)
			role, ok := m.cache.Get(cacheKey)
			if !ok {
				var err error
				role, err = m.members.GetRole(ctx, auth.TenantID, auth.UserType, auth.UserID)
				if err != nil {
					m.handleRoleError(ctx, w, err, auth, required)
					return
				}
				m.cache.Set(cacheKey, role)
			}

			if !role.AtLeast(required) {
				m.denyAccess(ctx, w, auth, role, required)
				return
			}

			ctx = withRole(ctx, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRoleStrict bypasses cache — use for sensitive operations.
func (m *Middleware) RequireRoleStrict(required domain.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			auth := session.FromContext(ctx)

			role, err := m.members.GetRole(ctx, auth.TenantID, auth.UserType, auth.UserID)
			if err != nil {
				m.handleRoleError(ctx, w, err, auth, required)
				return
			}

			if !role.AtLeast(required) {
				m.denyAccess(ctx, w, auth, role, required)
				return
			}

			ctx = withRole(ctx, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin is a convenience method for admin-only routes (cached).
func (m *Middleware) RequireAdmin() func(http.Handler) http.Handler {
	return m.RequireRole(domain.RoleAdmin)
}

// RequireAdminStrict bypasses cache — use for API keys, LLM config, member ops.
func (m *Middleware) RequireAdminStrict() func(http.Handler) http.Handler {
	return m.RequireRoleStrict(domain.RoleAdmin)
}

// RequireMember is a convenience method for member+ routes.
func (m *Middleware) RequireMember() func(http.Handler) http.Handler {
	return m.RequireRole(domain.RoleMember)
}

// RequireViewer is a convenience method for viewer+ routes (all authenticated users).
func (m *Middleware) RequireViewer() func(http.Handler) http.Handler {
	return m.RequireRole(domain.RoleViewer)
}

// InvalidateCache clears the cached role for a user (call on role change).
func (m *Middleware) InvalidateCache(tenantID, memberType, userID string) {
	m.cache.Delete(fmt.Sprintf("%s:%s:%s", tenantID, memberType, userID))
}

func (m *Middleware) handleRoleError(ctx context.Context, w http.ResponseWriter, err error, auth *session.AuthCtx, required domain.Role) {
	if errors.Is(err, tenantmember.ErrNotFound) {
		metrics.AuthzDeniedTotal.WithLabelValues("unknown", required.String()).Inc()
		logext.Warnf(ctx, "[rbac] deny: not a tenant member,user_id:%s,type:%s",
			auth.UserID, auth.UserType)
		dispatcher.Reject(ctx, w, http.StatusForbidden,
			attunev1.ErrorCode_FORBIDDEN, "not a tenant member")
		return
	}
	logext.Errorf(ctx, "[rbac] role lookup failed,user_id:%s,err:%s", auth.UserID, err.Error())
	dispatcher.Reject(ctx, w, http.StatusInternalServerError,
		attunev1.ErrorCode_INTERNAL, "failed to verify membership")
}

func (m *Middleware) denyAccess(ctx context.Context, w http.ResponseWriter, auth *session.AuthCtx, role, required domain.Role) {
	metrics.AuthzDeniedTotal.WithLabelValues(role.String(), required.String()).Inc()
	logext.Warnf(ctx, "[rbac] deny,user_id:%s,role:%s,required:%s",
		auth.UserID, role, required)
	dispatcher.Reject(ctx, w, http.StatusForbidden,
		attunev1.ErrorCode_FORBIDDEN,
		fmt.Sprintf("requires %s role or higher", required))
}

// FromContext returns the role from request context, or RoleViewer if missing.
func FromContext(ctx context.Context) domain.Role {
	role, ok := ctx.Value(roleCtxKey{}).(domain.Role)
	if !ok {
		return domain.RoleViewer
	}
	return role
}

func withRole(ctx context.Context, role domain.Role) context.Context {
	return context.WithValue(ctx, roleCtxKey{}, role)
}

// roleCache is a simple TTL cache for role lookups.
type roleCache struct {
	mu      sync.RWMutex
	entries map[string]cacheEntry
	ttl     time.Duration
}

type cacheEntry struct {
	role   domain.Role
	expiry time.Time
}

func newRoleCache(ttl time.Duration) *roleCache {
	return ptrext.Of(roleCache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
	})
}

func (c *roleCache) Get(key string) (domain.Role, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiry) {
		return "", false
	}
	return entry.role, true
}

func (c *roleCache) Set(key string, role domain.Role) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{
		role:   role,
		expiry: time.Now().Add(c.ttl),
	}
}

func (c *roleCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}
