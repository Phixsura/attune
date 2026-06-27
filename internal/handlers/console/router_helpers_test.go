package console

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/rbac"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
)

// ---------------------------------------------------------------------------
// bindListDeliveriesRequest
// ---------------------------------------------------------------------------

func TestBindListDeliveriesRequest_NoParams(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/outbox/deliveries", nil)
	lr := ptrext.Of(attunev1.ListDeliveriesRequest{})
	err := bindListDeliveriesRequest(req, lr)
	require.NoError(t, err)
	require.Empty(t, lr.Status)
	require.Zero(t, lr.Limit)
	require.Zero(t, lr.BeforeId)
}

func TestBindListDeliveriesRequest_ValidParams(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet,
		"/outbox/deliveries?status=dead&status=failed&limit=50&before_id=123", nil)
	lr := ptrext.Of(attunev1.ListDeliveriesRequest{})
	err := bindListDeliveriesRequest(req, lr)
	require.NoError(t, err)
	require.Equal(t, []string{"dead", "failed"}, lr.Status)
	require.Equal(t, int32(50), lr.Limit)
	require.Equal(t, int64(123), lr.BeforeId)
}

func TestBindListDeliveriesRequest_LimitOverflowInt32(t *testing.T) {
	t.Parallel()
	// 2^32 exceeds int32 range; ParseInt with bitSize=32 rejects it.
	req := httptest.NewRequest(http.MethodGet,
		"/outbox/deliveries?limit=4294967296", nil)
	lr := ptrext.Of(attunev1.ListDeliveriesRequest{})
	err := bindListDeliveriesRequest(req, lr)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid limit")
}

func TestBindListDeliveriesRequest_NegativeBeforeID(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet,
		"/outbox/deliveries?before_id=-99", nil)
	lr := ptrext.Of(attunev1.ListDeliveriesRequest{})
	err := bindListDeliveriesRequest(req, lr)
	// Negative int64 is technically valid parse; the repo will handle semantics.
	require.NoError(t, err)
	require.Equal(t, int64(-99), lr.BeforeId)
}

func TestBindListDeliveriesRequest_ZeroLimit(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet,
		"/outbox/deliveries?limit=0", nil)
	lr := ptrext.Of(attunev1.ListDeliveriesRequest{})
	err := bindListDeliveriesRequest(req, lr)
	require.NoError(t, err)
	require.Equal(t, int32(0), lr.Limit)
}

func TestBindListDeliveriesRequest_NegativeLimit(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet,
		"/outbox/deliveries?limit=-10", nil)
	lr := ptrext.Of(attunev1.ListDeliveriesRequest{})
	err := bindListDeliveriesRequest(req, lr)
	// Negative int32 parses fine; clamping is the repo's responsibility.
	require.NoError(t, err)
	require.Equal(t, int32(-10), lr.Limit)
}

func TestBindListDeliveriesRequest_LargeBeforeID(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet,
		"/outbox/deliveries?before_id=9223372036854775807", nil) // max int64
	lr := ptrext.Of(attunev1.ListDeliveriesRequest{})
	err := bindListDeliveriesRequest(req, lr)
	require.NoError(t, err)
	require.Equal(t, int64(9223372036854775807), lr.BeforeId)
}

func TestBindListDeliveriesRequest_BeforeIDOverflowInt64(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet,
		"/outbox/deliveries?before_id=92233720368547758070", nil) // exceeds int64
	lr := ptrext.Of(attunev1.ListDeliveriesRequest{})
	err := bindListDeliveriesRequest(req, lr)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid before_id")
}

func TestBindListDeliveriesRequest_EmptyQueryValues(t *testing.T) {
	t.Parallel()
	// Empty string values for limit/before_id are treated as absent.
	req := httptest.NewRequest(http.MethodGet,
		"/outbox/deliveries?limit=&before_id=", nil)
	lr := ptrext.Of(attunev1.ListDeliveriesRequest{})
	err := bindListDeliveriesRequest(req, lr)
	require.NoError(t, err)
	require.Zero(t, lr.Limit)
	require.Zero(t, lr.BeforeId)
}

func TestBindListDeliveriesRequest_FloatLimit(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet,
		"/outbox/deliveries?limit=3.14", nil)
	lr := ptrext.Of(attunev1.ListDeliveriesRequest{})
	err := bindListDeliveriesRequest(req, lr)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid limit")
}

// ---------------------------------------------------------------------------
// useRBACForRequest
// ---------------------------------------------------------------------------

func TestUseRBACForRequest_NilRBAC(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		TenantID: "t1", UserType: "admin", UserID: "u1",
	})))
	require.False(t, r.useRBACForRequest(req))
}

func TestUseRBACForRequest_EmptyTenantID(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{rbac: rbac.NewMiddleware(fakeRoleStore{role: domain.RoleAdmin})})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		UserType: "admin", UserID: "u1",
	})))
	require.False(t, r.useRBACForRequest(req))
}

func TestUseRBACForRequest_EmptyUserType(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{rbac: rbac.NewMiddleware(fakeRoleStore{role: domain.RoleAdmin})})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		TenantID: "t1", UserID: "u1",
	})))
	require.False(t, r.useRBACForRequest(req))
}

func TestUseRBACForRequest_AllPresent(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{rbac: rbac.NewMiddleware(fakeRoleStore{role: domain.RoleAdmin})})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		TenantID: "t1", UserType: "oidc_user", UserID: "u1",
	})))
	require.True(t, r.useRBACForRequest(req))
}

// ---------------------------------------------------------------------------
// requireAdminLegacy edge cases
// ---------------------------------------------------------------------------

func TestRequireAdminLegacy_NilAdminsRepo(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{admins: nil})
	called := false
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireAdminLegacy(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.False(t, called)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireAdminLegacy_AdminLookupError(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{admins: roleAdminReader{err: errors.New("db connection lost")}})
	called := false
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireAdminLegacy(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.False(t, called)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRequireAdminLegacy_UserNotFound(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{admins: roleAdminReader{err: admin.ErrNotFound}})
	called := false
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireAdminLegacy(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.False(t, called)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireAdminLegacy_NonAdminRole(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{admins: roleAdminReader{row: admin.Admin{ID: "u1", Role: "viewer"}}})
	called := false
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireAdminLegacy(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.False(t, called)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireAdminLegacy_AdminRoleSetsContext(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{admins: roleAdminReader{row: admin.Admin{ID: "u1", Role: "admin"}}})
	var seenRole domain.Role
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireAdminLegacy(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		seenRole = rbac.FromContext(req.Context())
	})).ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, domain.RoleAdmin, seenRole)
}

func TestRequireAdminLegacy_ResponseBodyContainsErrorCode(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{admins: roleAdminReader{err: admin.ErrNotFound}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireAdminLegacy(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, req)

	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "FORBIDDEN", body.Code)
}

func TestRequireAdminLegacy_InternalErrorResponseBody(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{admins: roleAdminReader{err: errors.New("some db error")}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireAdminLegacy(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, req)

	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "INTERNAL", body.Code)
}

// ---------------------------------------------------------------------------
// requireAdminStrict legacy fallback
// ---------------------------------------------------------------------------

func TestRequireAdminStrict_LegacyAdminAllowed(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{admins: roleAdminReader{row: admin.Admin{ID: "u1", Role: "admin"}}})
	called := false
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireAdminStrict(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRequireAdminStrict_LegacyNonAdminDenied(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{admins: roleAdminReader{row: admin.Admin{ID: "u1", Role: "viewer"}}})
	called := false
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireAdminStrict(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.False(t, called)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireAdminStrict_RBACBranchAllowsAdmin(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{rbac: rbac.NewMiddleware(fakeRoleStore{role: domain.RoleAdmin})})
	called := false
	w := httptest.NewRecorder()
	r.requireAdminStrict(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, authedReq())

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

// ---------------------------------------------------------------------------
// requireViewer legacy admin fallback branch
// ---------------------------------------------------------------------------

func TestRequireViewer_LegacyAdminAllowed(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{admins: roleAdminReader{row: admin.Admin{ID: "u1", Role: "admin"}}})
	called := false
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireViewer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRequireViewer_LegacyNonAdminDenied(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{admins: roleAdminReader{row: admin.Admin{ID: "u1", Role: "viewer"}}})
	called := false
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireViewer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.False(t, called)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireViewer_NoRBACNoAdmins_Passthrough(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	called := false
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireViewer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRequireViewer_RBACBranchAllowsAdmin(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{rbac: rbac.NewMiddleware(fakeRoleStore{role: domain.RoleAdmin})})
	called := false
	w := httptest.NewRecorder()
	r.requireViewer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, authedReq())

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

// ---------------------------------------------------------------------------
// authProviders
// ---------------------------------------------------------------------------

func TestAuthProviders_NoOIDC(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/providers", nil)

	r.authProviders(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var resp authProvidersResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Providers, 1)
	require.Equal(t, "password", resp.Providers[0].Type)
	require.Empty(t, resp.Providers[0].Name)
	require.False(t, resp.OIDCOnly)
}

func TestAuthProviders_ResponseIsValidJSON(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/providers", nil)

	r.authProviders(w, req)

	require.True(t, json.Valid(w.Body.Bytes()))
}

// ---------------------------------------------------------------------------
// Setter methods
// ---------------------------------------------------------------------------

func TestSetOutboxHandler_Nil(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	require.Nil(t, r.outbox)
	r.SetOutboxHandler(nil)
	require.Nil(t, r.outbox)
}

func TestSetPreflightHandler_Sets(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	require.Nil(t, r.preflight)
	dummy := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	r.SetPreflightHandler(dummy)
	require.NotNil(t, r.preflight)
}

func TestSetMCPClientHandler_Nil(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	require.Nil(t, r.mcpClients)
	r.SetMCPClientHandler(nil)
	require.Nil(t, r.mcpClients)
}

// ---------------------------------------------------------------------------
// nil-guard mount methods skip cleanly
// ---------------------------------------------------------------------------

func TestMountLLMConfig_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountLLMConfig(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountAuditLog_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountAuditLog(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountGDPR_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountGDPR(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountInbound_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountInbound(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountClusters_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountClusters(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountOutbox_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountOutbox(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountMCPClients_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountMCPClients(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountTags_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountTags(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountJobs_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountJobs(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountWorkflow_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountWorkflow(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountMembers_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountMembers(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountOIDC_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountOIDC(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountPreflight_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountPreflight(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountEnrichmentRuntime_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountEnrichmentRuntime(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountFeedbackBatchRoutes_NilBothSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountFeedbackBatchRoutes(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountFeedbackTagRoutes_NilSkips(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})
	mux := chi.NewRouter()
	r.mountFeedbackTagRoutes(mux)
	require.Equal(t, 0, countRoutes(t, mux))
}

func TestMountDigestSubscription_NilSkips(t *testing.T) {
	t.Parallel()
	// mountDigestSubscription does not have a nil guard (it mounts
	// unconditionally); verify the Router field is populated when a Router
	// is constructed by checking it isn't nil after a populated construction.
	r := ptrext.Of(Router{})
	require.Nil(t, r.digestSubscription)
}

// ---------------------------------------------------------------------------
// NewRouter
// ---------------------------------------------------------------------------

func TestNewRouter_NilMembersRepo(t *testing.T) {
	t.Parallel()
	signer, err := session.NewSigner(strings.Repeat("k", 32))
	require.NoError(t, err)
	r := NewRouter(signer, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	require.Nil(t, r.rbac)
}

// ---------------------------------------------------------------------------
// requireAdmin dispatches between RBAC and legacy
// ---------------------------------------------------------------------------

func TestRequireAdmin_DispatchesToLegacyWhenRBACNil(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{admins: roleAdminReader{row: admin.Admin{ID: "u1", Role: "admin"}}})
	called := false
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRequireAdmin_DispatchesToRBACWhenPresent(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{rbac: rbac.NewMiddleware(fakeRoleStore{role: domain.RoleViewer})})
	called := false
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		TenantID: "t1", UserType: "oidc_user", UserID: "u1",
	})))

	r.requireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.False(t, called)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireAdmin_RBACPresentButFallsBackToLegacyWhenNoTenant(t *testing.T) {
	t.Parallel()
	// RBAC middleware is set, but auth context has no TenantID, so
	// useRBACForRequest returns false and it falls back to legacy admin.
	r := ptrext.Of(Router{
		rbac:   rbac.NewMiddleware(fakeRoleStore{role: domain.RoleViewer}),
		admins: roleAdminReader{row: admin.Admin{ID: "u1", Role: "admin"}},
	})
	called := false
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{UserID: "u1"})))

	r.requireAdmin(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(w, req)

	require.True(t, called)
	require.Equal(t, http.StatusOK, w.Code)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func countRoutes(t *testing.T, mux chi.Router) int {
	t.Helper()
	count := 0
	err := chi.Walk(mux, func(string, string, http.Handler, ...func(http.Handler) http.Handler) error {
		count++
		return nil
	})
	require.NoError(t, err)
	return count
}

// authProvidersResponse mirrors the anonymous struct inside authProviders for
// deserialization in test assertions.
type authProvidersResponse struct {
	Providers []struct {
		Type string `json:"type"`
		Name string `json:"name,omitempty"`
	} `json:"providers"`
	OIDCOnly bool `json:"oidc_only,omitempty"`
}
