package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/rbac"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/admin"
)

func TestRequireAdminRejectsNonAdminRole(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{admins: roleAdminReader{row: admin.Admin{ID: "user-1", Role: "viewer"}}})
	called := false
	req := httptest.NewRequest(http.MethodGet, "/llm/channels", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "user-1"})))
	w := httptest.NewRecorder()

	router.requireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.False(t, called)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireAdminAllowsAdminRole(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{admins: roleAdminReader{row: admin.Admin{ID: "user-1", Role: "admin"}}})
	called := false
	req := httptest.NewRequest(http.MethodGet, "/llm/channels", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "user-1"})))
	w := httptest.NewRecorder()

	router.requireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

type roleAdminReader struct {
	row admin.Admin
	err error
}

func (r roleAdminReader) GetByID(context.Context, string) (admin.Admin, error) {
	if r.err != nil {
		return admin.Admin{}, r.err
	}
	return r.row, nil
}

// fakeRoleStore satisfies the rbac middleware's roleStore dependency so the
// router's RBAC-backed branch (r.rbac != nil) can be exercised.
type fakeRoleStore struct{ role domain.Role }

func (f fakeRoleStore) GetRole(context.Context, string, string, string) (domain.Role, error) {
	return f.role, nil
}

func rbacRouter(role domain.Role) *Router {
	return ptrext.Of(Router{rbac: rbac.NewMiddleware(fakeRoleStore{role: role})})
}

func authedReq() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/llm/channels", nil)
	return req.WithContext(session.WithAuthCtx(req.Context(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserType: "oidc_user", UserID: "user-1"})))
}

func legacyAdminReq() *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/llm/channels", nil)
	return req.WithContext(session.WithAuthCtx(req.Context(),
		ptrext.Of(session.AuthCtx{UserID: "user-1"})))
}

func TestRequireAdmin_RBACBranchAllowsAdmin(t *testing.T) {
	t.Parallel()
	called := false
	w := httptest.NewRecorder()
	rbacRouter(domain.RoleAdmin).requireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, authedReq())

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRequireAdmin_RBACBranchDeniesViewer(t *testing.T) {
	t.Parallel()
	called := false
	w := httptest.NewRecorder()
	rbacRouter(domain.RoleViewer).requireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, authedReq())

	require.False(t, called)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireAdminStrict_RBACBranchDeniesViewer(t *testing.T) {
	t.Parallel()
	called := false
	w := httptest.NewRecorder()
	rbacRouter(domain.RoleViewer).requireAdminStrict(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, authedReq())

	require.False(t, called)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireViewer_RBACBranchAllowsViewer(t *testing.T) {
	t.Parallel()
	called := false
	w := httptest.NewRecorder()
	rbacRouter(domain.RoleViewer).requireViewer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, authedReq())

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRequireViewer_LegacyFallbackPassesThrough(t *testing.T) {
	t.Parallel()
	// No rbac and no admins repo: viewer is the baseline, so the legacy
	// fallback simply forwards to the next handler.
	called := false
	w := httptest.NewRecorder()
	ptrext.Of(Router{}).requireViewer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, authedReq())

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRequireAdmin_RBACRouterFallsBackToLegacyAdminSession(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{
		rbac:   rbac.NewMiddleware(fakeRoleStore{role: domain.RoleViewer}),
		admins: roleAdminReader{row: admin.Admin{ID: "user-1", Role: "admin"}},
	})
	called := false
	w := httptest.NewRecorder()

	router.requireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, legacyAdminReq())

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRequireViewer_RBACRouterFallsBackToLegacyAdminSession(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{
		rbac:   rbac.NewMiddleware(fakeRoleStore{role: domain.RoleViewer}),
		admins: roleAdminReader{row: admin.Admin{ID: "user-1", Role: "admin"}},
	})
	called := false
	w := httptest.NewRecorder()

	router.requireViewer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, legacyAdminReq())

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}
