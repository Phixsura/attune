package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	consoleenrichmentruntime "github.com/Phixsura/attune/internal/handlers/console/enrichmentruntime"
	"github.com/Phixsura/attune/internal/handlers/console/internal/rbac"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
	enrichrepo "github.com/Phixsura/attune/internal/repo/enrichmentruntime"
	enrichruntimesvc "github.com/Phixsura/attune/internal/service/enrichruntime"
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
	seenRole := domain.Role("")
	req := httptest.NewRequest(http.MethodGet, "/llm/channels", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "user-1"})))
	w := httptest.NewRecorder()

	router.requireAdmin(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		called = true
		seenRole = rbac.FromContext(req.Context())
	})).ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, domain.RoleAdmin, seenRole)
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

func TestRequireAdminLegacyInjectsAdminRoleIntoContext(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{admins: roleAdminReader{row: admin.Admin{ID: "user-1", Role: "admin"}}})
	seenRole := domain.Role("")
	w := httptest.NewRecorder()

	router.requireAdmin(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		seenRole = rbac.FromContext(req.Context())
	})).ServeHTTP(w, legacyAdminReq())

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, domain.RoleAdmin, seenRole)
}

type fakeRuntimeService struct {
	resetCalled    bool
	rollbackCalled bool
}

func (f *fakeRuntimeService) Get(context.Context) (enrichruntimesvc.ReadModel, error) {
	return enrichruntimesvc.ReadModel{}, nil
}

func (f *fakeRuntimeService) Update(context.Context, uint64, enrichrepo.Spec, string, enrichruntimesvc.MutationActor) (enrichruntimesvc.ReadModel, error) {
	return enrichruntimesvc.ReadModel{}, nil
}

func (f *fakeRuntimeService) Reset(context.Context, uint64, []string, bool, string, enrichruntimesvc.MutationActor) (enrichruntimesvc.ReadModel, error) {
	f.resetCalled = true
	return enrichruntimesvc.ReadModel{}, nil
}

func (f *fakeRuntimeService) Rollback(context.Context, uint64, uint64, string, enrichruntimesvc.MutationActor) (enrichruntimesvc.ReadModel, error) {
	f.rollbackCalled = true
	return enrichruntimesvc.ReadModel{}, nil
}

func runtimeAuthedReq(method, path string, body any, role domain.Role) *http.Request {
	var reader *strings.Reader
	if body == nil {
		reader = strings.NewReader("")
	} else {
		raw, _ := json.Marshal(body)
		reader = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserID:   "user-1",
		UserType: "oidc_user",
		StepUpAt: ptrext.Of(time.Now()),
	})))
	if role != "" {
		req = req.WithContext(rbac.WithRole(req.Context(), role))
	}
	return req
}

func TestEnrichmentRuntimeLegacyResetRouteRequiresAdmin(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(fakeRuntimeService{})
	router := ptrext.Of(Router{
		rbac:              rbac.NewMiddleware(fakeRoleStore{role: domain.RoleViewer}),
		enrichmentRuntime: consoleenrichmentruntime.NewHandler(svc, 15*time.Minute),
	})
	mux := chi.NewRouter()
	router.mountEnrichmentRuntime(mux)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, runtimeAuthedReq(http.MethodPost, "/enrichment-runtime:reset", attunev1.ResetEnrichmentRuntimeRequest{
		ExpectedVersion: 1,
		ResetAll:        true,
	}, domain.RoleViewer))

	require.Equal(t, http.StatusForbidden, w.Code)
	require.False(t, svc.resetCalled)
}

func TestEnrichmentRuntimeLegacyResetRouteAllowsAdmin(t *testing.T) {
	t.Parallel()
	svc := ptrext.Of(fakeRuntimeService{})
	router := ptrext.Of(Router{
		rbac:              rbac.NewMiddleware(fakeRoleStore{role: domain.RoleAdmin}),
		enrichmentRuntime: consoleenrichmentruntime.NewHandler(svc, 15*time.Minute),
	})
	mux := chi.NewRouter()
	router.mountEnrichmentRuntime(mux)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, runtimeAuthedReq(http.MethodPost, "/enrichment-runtime:reset", attunev1.ResetEnrichmentRuntimeRequest{
		ExpectedVersion: 1,
		ResetAll:        true,
	}, domain.RoleAdmin))

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, svc.resetCalled)
}
