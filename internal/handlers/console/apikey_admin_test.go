package console

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// scopedVerifier is a stub apikey.Verifier that authenticates every request as a
// fixed tenant/key carrying a fixed scope set, so a test can drive the scope
// gate without a database or real key store.
type scopedVerifier struct {
	tenant string
	key    uuid.UUID
	scopes []domain.Scope
	rpm    *int // per-key rate_limit_rpm (nil = no per-key cap)
}

func (v scopedVerifier) Lookup(context.Context, string) (string, uuid.UUID, error) {
	return v.tenant, v.key, nil
}

func (v scopedVerifier) LookupWithScopes(context.Context, string) (string, uuid.UUID, []domain.Scope, error) {
	return v.tenant, v.key, v.scopes, nil
}

func (v scopedVerifier) LookupWithScopesAndIP(context.Context, string, string) (string, uuid.UUID, []domain.Scope, *int, error) {
	return v.tenant, v.key, v.scopes, v.rpm, nil
}

// disabledLimiter is a no-op per-key limiter for tests that don't exercise rate
// limiting (every request is admitted).
func disabledLimiter() *ratelimit.PerKeyLimiter { return ratelimit.NewPerKey(true, nil) }

// TestApikeyToSessionMapsIdentity verifies the apikey→session adapter maps the
// authenticated key identity onto the AuthCtx the console handlers consume, with
// the actor recorded as "apikey:<keyID>".
func TestApikeyToSessionMapsIdentity(t *testing.T) {
	t.Parallel()
	keyID := "11111111-1111-1111-1111-111111111111"
	ctx := apikey.WithAuthForTest(context.Background(), "tenant-7", keyID, []domain.Scope{domain.ScopeTagsWrite})
	req := httptest.NewRequest(http.MethodGet, "/tags", nil).WithContext(ctx)

	auth, err := apikeyToSession(req)
	require.NoError(t, err)
	require.Equal(t, "tenant-7", auth.TenantID)
	require.Equal(t, "apikey:"+keyID, auth.UserID)
	require.Equal(t, "api_key", auth.UserType)
	require.NotNil(t, auth.StepUpAt)
}

// TestApikeyToSessionFailsClosedWithoutAuth verifies the adapter fails closed
// (401, no panic) if ever reached without the API-key middleware having run.
func TestApikeyToSessionFailsClosedWithoutAuth(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/tags", nil) // bare context, no auth
	auth, err := apikeyToSession(req)
	require.Nil(t, auth)
	require.Error(t, err)
}

// TestAPIKeyAdminRoutesScopeGated drives every mounted tag/workflow route with a
// key that holds only ingest:write and asserts each is rejected at the scope
// gate (403 FORBIDDEN) before any handler — and therefore any DB — is reached.
// A nil pool is safe precisely because the request never gets past RequireScope.
func TestAPIKeyAdminRoutesScopeGated(t *testing.T) {
	t.Parallel()
	v := scopedVerifier{tenant: "tenant-1", key: uuid.Nil, scopes: []domain.Scope{domain.ScopeIngestWrite}}
	r := chi.NewRouter()
	MountAPIKeyAdminRoutes(r, nil, v, 0, disabledLimiter(), APIKeyAdminRouteOptions{})

	cases := []struct {
		name, method, path string
	}{
		{"list tags needs tags:read", http.MethodGet, "/tags"},
		{"create tag needs tags:write", http.MethodPost, "/tags"},
		{"update tag needs tags:write", http.MethodPatch, "/tags/t1"},
		{"archive tag needs tags:write", http.MethodDelete, "/tags/t1"},
		{"list states needs workflow:read", http.MethodGet, "/workflow/states"},
		{"create state needs workflow:write", http.MethodPost, "/workflow/states"},
		{"update state needs workflow:write", http.MethodPatch, "/workflow/states/s1"},
		{"archive state needs workflow:write", http.MethodDelete, "/workflow/states/s1"},
		{"list transitions needs workflow:read", http.MethodGet, "/workflow/transitions"},
		{"replace transitions needs workflow:write", http.MethodPut, "/workflow/transitions"},
		{"seed needs workflow:write", http.MethodPost, "/workflow/seed"},
		{"list audit log needs audit:read", http.MethodGet, "/audit-log"},
		{"export audit csv needs audit:read", http.MethodGet, "/audit-log/export.csv"},
		{"create audit evidence needs audit:read", http.MethodPost, "/audit-log/evidence"},
		{"get audit evidence needs audit:read", http.MethodGet, "/audit-log/evidence/job-1"},
		{"download audit evidence needs audit:read", http.MethodGet, "/audit-log/evidence/job-1/download"},
		{"list gdpr requests needs gdpr:read", http.MethodGet, "/gdpr/requests"},
		{"get gdpr operations needs gdpr:read", http.MethodGet, "/gdpr/operations"},
		{"cancel gdpr request needs gdpr:delete", http.MethodPost, "/gdpr/requests/req-1/cancel"},
		{"export gdpr needs gdpr:export", http.MethodPost, "/gdpr/export"},
		{"get gdpr export needs gdpr:read", http.MethodGet, "/gdpr/exports/job-1"},
		{"download gdpr export needs gdpr:export", http.MethodGet, "/gdpr/exports/job-1/download"},
		{"revoke gdpr export needs gdpr:export", http.MethodPost, "/gdpr/exports/job-1/revoke"},
		{"delete gdpr subject needs gdpr:delete", http.MethodPost, "/gdpr/delete"},
		{"list outbox needs notify:read", http.MethodGet, "/outbox/deliveries"},
		{"retry outbox needs notify:write", http.MethodPost, "/outbox/1/retry"},
		{"list mcp clients needs mcpclient:admin", http.MethodGet, "/mcp/clients"},
		{"create hook needs hooks:manage", http.MethodPost, "/hooks"},
		{"list hooks needs hooks:manage", http.MethodGet, "/hooks"},
		{"delete hook needs hooks:manage", http.MethodDelete, "/hooks/h1"},
		{"hook samples need hooks:manage", http.MethodGet, "/hooks/samples/feedback.created"},
		{"list requests needs requests:read", http.MethodGet, "/requests"},
		{"create request needs requests:write", http.MethodPost, "/requests"},
		{"update request needs requests:write", http.MethodPatch, "/requests/r1"},
		{"add request note needs requests:write", http.MethodPost, "/requests/r1/notes"},
		{"add feedback tag needs tags:write", http.MethodPost, "/feedback/1/tags"},
		{"remove feedback tag needs tags:write", http.MethodDelete, "/feedback/1/tags/t1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("X-API-Key", domain.APIKeyPrefix+"restricted")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusForbidden, w.Code)
			var body struct {
				Code string `json:"code"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, "FORBIDDEN", body.Code)
		})
	}
}

// TestAPIKeyAdminRoutesBindAndAuthorize drives every mounted route with a
// full-scope key so each request passes the scope gate and runs the route's
// request-binding + WithAuth (apikeyToSession) closures before the endpoint
// handler. With a nil pool the data-access handlers panic; middleware.Recoverer
// turns that into a 500 — by which point the binder + auth closures have already
// executed. We assert only that the request got PAST auth (not 401/403),
// proving every route decodes its input and authorizes via the apikey adapter.
func TestAPIKeyAdminRoutesBindAndAuthorize(t *testing.T) {
	t.Parallel()
	v := scopedVerifier{tenant: "tenant-1", key: uuid.Nil, scopes: domain.AllScopes}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	MountAPIKeyAdminRoutes(r, nil, v, 0, disabledLimiter(), APIKeyAdminRouteOptions{})

	cases := []struct {
		name, method, path, body string
	}{
		// Bodies/ids are fully valid so each request passes binding + the
		// handler's input validation and actually reaches the (nil-pool)
		// data-access layer — see the docstring.
		{"list tags", http.MethodGet, "/tags?include_archived=true", ""},
		{"create tag", http.MethodPost, "/tags", `{"name":"x","color":"#3b82f6"}`},
		{"update tag", http.MethodPatch, "/tags/11111111-1111-1111-1111-111111111111", `{"name":"x","color":"#3b82f6"}`},
		{"archive tag", http.MethodDelete, "/tags/11111111-1111-1111-1111-111111111111", ""},
		{"list states", http.MethodGet, "/workflow/states?include_archived=1", ""},
		{"create state", http.MethodPost, "/workflow/states", `{"name":"s","displayName":{"entries":{"en":"S"}},"color":"#3b82f6","category":"active","position":1}`},
		{"update state", http.MethodPatch, "/workflow/states/11111111-1111-1111-1111-111111111111", `{"color":"#22c55e"}`},
		{"archive state", http.MethodDelete, "/workflow/states/11111111-1111-1111-1111-111111111111", ""},
		{"list transitions", http.MethodGet, "/workflow/transitions", ""},
		{"replace transitions", http.MethodPut, "/workflow/transitions", `{"transitions":[]}`},
		{"seed defaults", http.MethodPost, "/workflow/seed", ""},
		{"list audit log", http.MethodGet, "/audit-log?limit=5&actions=tag.create", ""},
		{"export audit csv", http.MethodGet, "/audit-log/export.csv?actions=tag.create", ""},
		{"create audit evidence", http.MethodPost, "/audit-log/evidence", `{"from":"2026-01-01T00:00:00Z","to":"2026-01-31T00:00:00Z"}`},
		{"get audit evidence", http.MethodGet, "/audit-log/evidence/job-1", ""},
		{"download audit evidence", http.MethodGet, "/audit-log/evidence/job-1/download", ""},
		{"list gdpr requests", http.MethodGet, "/gdpr/requests?limit=5&request_type=export", ""},
		{"get gdpr operations", http.MethodGet, "/gdpr/operations", ""},
		{"cancel gdpr request", http.MethodPost, "/gdpr/requests/req-1/cancel", ""},
		{"export gdpr", http.MethodPost, "/gdpr/export", `{"subjectKey":"user:123"}`},
		{"get gdpr export", http.MethodGet, "/gdpr/exports/job-1", ""},
		{"download gdpr export", http.MethodGet, "/gdpr/exports/job-1/download", ""},
		{"revoke gdpr export", http.MethodPost, "/gdpr/exports/job-1/revoke", ""},
		{"delete gdpr subject", http.MethodPost, "/gdpr/delete", `{"subjectKey":"user:123"}`},
		{"list outbox", http.MethodGet, "/outbox/deliveries?status=dead&limit=5", ""},
		{"retry outbox", http.MethodPost, "/outbox/1/retry", ""},
		{"list mcp clients", http.MethodGet, "/mcp/clients", ""},
		{"create hook", http.MethodPost, "/hooks", `{"target_url":"https://hooks.zapier.com/x","event_types":["feedback.created"],"consumer":"zapier"}`},
		{"list hooks", http.MethodGet, "/hooks", ""},
		{"delete hook", http.MethodDelete, "/hooks/11111111-1111-1111-1111-111111111111", ""},
		{"hook samples", http.MethodGet, "/hooks/samples/request.created", ""},
		{"list requests", http.MethodGet, "/requests?q=x&status=CUSTOMER_REQUEST_STATUS_OPEN&priority=CUSTOMER_REQUEST_PRIORITY_HIGH&limit=5&cursor=c1", ""},
		{"create request", http.MethodPost, "/requests", `{"title":"T","idempotency_key":"route-test-1"}`},
		{"update request", http.MethodPatch, "/requests/11111111-1111-1111-1111-111111111111", `{"status":"CUSTOMER_REQUEST_STATUS_PLANNED"}`},
		{"add request note", http.MethodPost, "/requests/11111111-1111-1111-1111-111111111111/notes", `{"body":"n","visibility":"internal"}`},
		{"add feedback tag", http.MethodPost, "/feedback/1/tags", `{"tag_name":"bug"}`},
		{"remove feedback tag", http.MethodDelete, "/feedback/1/tags/11111111-1111-1111-1111-111111111111", ""},
		{"create mcp client", http.MethodPost, "/mcp/clients", `{"name":"agent","redirect_uris":["https://example.com/callback"],"scopes":["mcp:read"]}`},
		{"get mcp client", http.MethodGet, "/mcp/clients/11111111-1111-1111-1111-111111111111", ""},
		{"revoke mcp client", http.MethodDelete, "/mcp/clients/11111111-1111-1111-1111-111111111111", ""},
		{"update mcp client", http.MethodPatch, "/mcp/clients/11111111-1111-1111-1111-111111111111", `{"tool_policy_mode":"legacy_allow_all"}`},
		{"replace mcp tool policies", http.MethodPut, "/mcp/clients/11111111-1111-1111-1111-111111111111/tool-policies", `{"policies":[]}`},
		{"revoke mcp grant", http.MethodDelete, "/mcp/clients/11111111-1111-1111-1111-111111111111/grants/22222222-2222-2222-2222-222222222222", ""},
		{"revoke mcp session", http.MethodDelete, "/mcp/clients/11111111-1111-1111-1111-111111111111/sessions/33333333-3333-3333-3333-333333333333", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var bodyReader io.Reader
			if tc.body != "" {
				bodyReader = strings.NewReader(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, bodyReader)
			req.Header.Set("X-API-Key", domain.APIKeyPrefix+"full")
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// Got past the scope gate + auth adapter into binding/handler.
			require.NotEqual(t, http.StatusUnauthorized, w.Code)
			require.NotEqual(t, http.StatusForbidden, w.Code)
		})
	}
}

// TestAPIKeyAdminRoutesPerKeyRateLimited verifies the key's own rate_limit_rpm
// is enforced on the admin surface (not just ingest): a 1-rpm key is admitted
// once, then the second request is rejected 429 by the per-key limiter before
// the handler runs. Without the limiter wired, a capped key could mutate
// tags/workflow without bound.
func TestAPIKeyAdminRoutesPerKeyRateLimited(t *testing.T) {
	t.Parallel()
	keyID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	v := scopedVerifier{tenant: "tenant-1", key: keyID, scopes: domain.AllScopes, rpm: ptrext.Of(1)}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer) // the admitted request 500s on the nil pool; the limiter is what we assert
	MountAPIKeyAdminRoutes(r, nil, v, 0, ratelimit.NewPerKey(false, nil), APIKeyAdminRouteOptions{})

	do := func() int {
		req := httptest.NewRequest(http.MethodGet, "/tags", nil)
		req.Header.Set("X-API-Key", domain.APIKeyPrefix+"capped")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	_ = do() // first request admitted (then 500s on the nil pool)
	require.Equal(t, http.StatusTooManyRequests, do())
}

// TestAPIKeyAdminRoutesRejectMissingKey verifies the mounted group is behind the
// API-key middleware: a request with no key is rejected 401 before any handler.
func TestAPIKeyAdminRoutesRejectMissingKey(t *testing.T) {
	t.Parallel()
	v := scopedVerifier{tenant: "tenant-1", key: uuid.Nil, scopes: domain.AllScopes}
	r := chi.NewRouter()
	MountAPIKeyAdminRoutes(r, nil, v, 0, disabledLimiter(), APIKeyAdminRouteOptions{})

	req := httptest.NewRequest(http.MethodGet, "/tags", nil) // no X-API-Key
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAPIKeyAdminRoutesNewManagementRoutesRequireExplicitScopes(t *testing.T) {
	t.Parallel()
	v := scopedVerifier{tenant: "tenant-1", key: uuid.Nil, scopes: nil}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	MountAPIKeyAdminRoutes(r, nil, v, 0, disabledLimiter(), APIKeyAdminRouteOptions{})

	cases := []struct {
		name, method, path string
		wantStatus         int
	}{
		{"legacy key still reaches existing tags bridge", http.MethodGet, "/tags", http.StatusInternalServerError},
		{"legacy key denied on audit", http.MethodGet, "/audit-log", http.StatusForbidden},
		{"legacy key denied on gdpr", http.MethodGet, "/gdpr/requests", http.StatusForbidden},
		{"legacy key denied on outbox", http.MethodGet, "/outbox/deliveries", http.StatusForbidden},
		{"legacy key denied on mcp clients", http.MethodGet, "/mcp/clients", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("X-API-Key", domain.APIKeyPrefix+"legacy")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.wantStatus, w.Code)
		})
	}
}

func TestAPIKeyAdminRoutesLegacyGdprAdminScopeStillSatisfiesGranularChecks(t *testing.T) {
	t.Parallel()

	v := scopedVerifier{tenant: "tenant-1", key: uuid.Nil, scopes: []domain.Scope{domain.ScopeGDPRAdmin}}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	MountAPIKeyAdminRoutes(r, nil, v, 0, disabledLimiter(), APIKeyAdminRouteOptions{})

	cases := []struct {
		name, method, path, body string
	}{
		{"read request list", http.MethodGet, "/gdpr/requests", ""},
		{"export subject", http.MethodPost, "/gdpr/export", `{"subjectKey":"user:123"}`},
		{"cancel request", http.MethodPost, "/gdpr/requests/req-1/cancel", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("X-API-Key", domain.APIKeyPrefix+"gdpr-admin")
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.NotEqual(t, http.StatusForbidden, w.Code)
			require.NotEqual(t, http.StatusUnauthorized, w.Code)
		})
	}
}
