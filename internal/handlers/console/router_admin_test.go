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
	"github.com/Phixsura/attune/internal/handlers/console/feedback"
	"github.com/Phixsura/attune/internal/handlers/console/internal/rbac"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
	enrichrepo "github.com/Phixsura/attune/internal/repo/enrichmentruntime"
	replydraftrepo "github.com/Phixsura/attune/internal/repo/replydraft"
	enrichruntimesvc "github.com/Phixsura/attune/internal/service/enrichruntime"
	replydraftsvc "github.com/Phixsura/attune/internal/service/replydraft"
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

type routeReplyWorkflow struct {
	snap            replydraftsvc.Snapshot
	approveRevision int64
	rejectRevision  int64
}

func (f *routeReplyWorkflow) Snapshot(context.Context, string, int64) (replydraftsvc.Snapshot, error) {
	return f.snap, nil
}

func (f *routeReplyWorkflow) Edit(
	context.Context, string, int64, string, int64, replydraftrepo.Actor,
) (replydraftsvc.Snapshot, error) {
	return f.snap, nil
}

func (f *routeReplyWorkflow) Approve(
	_ context.Context, _ string, _ int64, expectedRevision int64, _ replydraftrepo.Actor,
) (replydraftsvc.Snapshot, error) {
	f.approveRevision = expectedRevision
	return f.snap, nil
}

func (f *routeReplyWorkflow) Reject(
	_ context.Context, _ string, _ int64, expectedRevision int64, _ replydraftrepo.Actor,
) (replydraftsvc.Snapshot, error) {
	f.rejectRevision = expectedRevision
	return f.snap, nil
}

func (f *routeReplyWorkflow) Send(
	context.Context, string, int64, string, int64, replydraftrepo.Actor,
) (replydraftsvc.SendResult, error) {
	return replydraftsvc.SendResult{Snapshot: f.snap}, nil
}

func (f *routeReplyWorkflow) UpsertHook(
	context.Context, string, string, string, string, bool, string,
) (replydraftsvc.HookConfig, error) {
	return replydraftsvc.HookConfig{}, nil
}

func (f *routeReplyWorkflow) GetHook(context.Context, string) (replydraftsvc.HookConfig, error) {
	return replydraftsvc.HookConfig{}, nil
}

func (f *routeReplyWorkflow) DisableHook(context.Context, string, string) (replydraftsvc.HookConfig, error) {
	return replydraftsvc.HookConfig{}, nil
}

func (f *routeReplyWorkflow) ListDeliveries(context.Context, string, int) ([]replydraftrepo.DeliveryAttempt, error) {
	return nil, nil
}

func (f *routeReplyWorkflow) DeliveryHealth(context.Context, string) (replydraftrepo.DeliveryHealth, error) {
	return replydraftrepo.DeliveryHealth{}, nil
}

func (f *routeReplyWorkflow) TestHook(
	context.Context, string, string, replydraftrepo.Actor,
) (replydraftsvc.HookTestResult, error) {
	return replydraftsvc.HookTestResult{}, nil
}

func (f *routeReplyWorkflow) Redeliver(
	context.Context, string, string, replydraftrepo.Actor,
) (replydraftrepo.DeliveryAttempt, error) {
	return replydraftrepo.DeliveryAttempt{}, nil
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

func TestReplyDraftRoutesRequireMember(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{
		rbac:     rbac.NewMiddleware(fakeRoleStore{role: domain.RoleViewer}),
		feedback: ptrext.Of(feedback.FeedbackHandler{}),
	})
	mux := chi.NewRouter()
	router.mountFeedback(mux)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, runtimeAuthedReq(http.MethodPost, "/feedback/123/reply-draft/approve", nil, domain.RoleViewer))

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestReplyDraftRegenerateRequiresMember(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{
		rbac:     rbac.NewMiddleware(fakeRoleStore{role: domain.RoleViewer}),
		feedback: ptrext.Of(feedback.FeedbackHandler{}),
	})
	mux := chi.NewRouter()
	router.mountFeedback(mux)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, runtimeAuthedReq(http.MethodPost, "/feedback/123/reply-draft/regenerate", nil, domain.RoleViewer))

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestReplyDraftRegenerateAllowsMemberPastPermissionGate(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{
		rbac:     rbac.NewMiddleware(fakeRoleStore{role: domain.RoleMember}),
		feedback: ptrext.Of(feedback.FeedbackHandler{}),
	})
	mux := chi.NewRouter()
	router.mountFeedback(mux)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, runtimeAuthedReq(http.MethodPost, "/feedback/123/reply-draft/regenerate", nil, domain.RoleMember))

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestReplyDraftRoutesAllowMemberPastPermissionGate(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{
		rbac:     rbac.NewMiddleware(fakeRoleStore{role: domain.RoleMember}),
		feedback: ptrext.Of(feedback.FeedbackHandler{}),
	})
	mux := chi.NewRouter()
	router.mountFeedback(mux)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, runtimeAuthedReq(http.MethodPost, "/feedback/123/reply-draft/approve", nil, domain.RoleMember))

	require.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestReplyDraftApproveRejectRoutesBindExpectedRevision(t *testing.T) {
	t.Parallel()
	workflow := ptrext.Of(routeReplyWorkflow{snap: routeReplySnapshot("approved")})
	handler := feedback.NewFeedbackHandler(nil, nil)
	handler.SetReplyDraftWorkflow(workflow)
	router := ptrext.Of(Router{
		rbac:     rbac.NewMiddleware(fakeRoleStore{role: domain.RoleMember}),
		feedback: handler,
	})
	mux := chi.NewRouter()
	router.mountFeedback(mux)

	approve := httptest.NewRecorder()
	mux.ServeHTTP(approve, runtimeAuthedReq(http.MethodPost, "/feedback/123/reply-draft/approve", map[string]any{
		"expectedRevision": "42",
	}, domain.RoleMember))
	require.Equal(t, http.StatusOK, approve.Code)
	require.Equal(t, int64(42), workflow.approveRevision)

	reject := httptest.NewRecorder()
	mux.ServeHTTP(reject, runtimeAuthedReq(http.MethodPost, "/feedback/123/reply-draft/reject", map[string]any{
		"expectedRevision": "43",
	}, domain.RoleMember))
	require.Equal(t, http.StatusOK, reject.Code)
	require.Equal(t, int64(43), workflow.rejectRevision)
}

func TestReplySendHookRoutesRequireAdmin(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{
		rbac:     rbac.NewMiddleware(fakeRoleStore{role: domain.RoleMember}),
		feedback: ptrext.Of(feedback.FeedbackHandler{}),
	})
	mux := chi.NewRouter()
	router.mountReplySendHook(mux)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, runtimeAuthedReq(http.MethodPut, "/reply-send-hook", map[string]any{
		"name": "Zendesk",
		"url":  "https://hooks.example.test/replies",
	}, domain.RoleMember))

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestReplySendHookRoutesAllowAdminPastPermissionGate(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{
		rbac:     rbac.NewMiddleware(fakeRoleStore{role: domain.RoleAdmin}),
		feedback: ptrext.Of(feedback.FeedbackHandler{}),
	})
	mux := chi.NewRouter()
	router.mountReplySendHook(mux)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, runtimeAuthedReq(http.MethodPut, "/reply-send-hook", map[string]any{
		"name": "Zendesk",
		"url":  "https://hooks.example.test/replies",
	}, domain.RoleAdmin))

	require.Equal(t, http.StatusNotImplemented, w.Code)
}

func TestReplySendHookDeliveriesRejectInvalidLimit(t *testing.T) {
	t.Parallel()
	router := ptrext.Of(Router{
		rbac:     rbac.NewMiddleware(fakeRoleStore{role: domain.RoleAdmin}),
		feedback: ptrext.Of(feedback.FeedbackHandler{}),
	})
	mux := chi.NewRouter()
	router.mountReplySendHook(mux)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, runtimeAuthedReq(http.MethodGet, "/reply-send-hook/deliveries?limit=abc", nil, domain.RoleAdmin))

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "limit must be an integer")
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

func routeReplySnapshot(status string) replydraftsvc.Snapshot {
	now := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	draft := replydraftrepo.Draft{
		ID:               "11111111-1111-1111-1111-111111111111",
		TenantID:         "tenant-1",
		FeedbackID:       123,
		CycleNo:          1,
		Status:           status,
		ActiveRevisionID: "22222222-2222-2222-2222-222222222222",
		ActiveContent:    "Hello",
		Revision:         2,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	return replydraftsvc.Snapshot{Draft: ptrext.Of(draft), HookConfigured: true}
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
