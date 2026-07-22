// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures
package console

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/handlers/console/apikey"
	consoleauditlog "github.com/Phixsura/attune/internal/handlers/console/auditlog"
	"github.com/Phixsura/attune/internal/handlers/console/auth"
	"github.com/Phixsura/attune/internal/handlers/console/clusters"
	consolecustomerrequest "github.com/Phixsura/attune/internal/handlers/console/customerrequest"
	"github.com/Phixsura/attune/internal/handlers/console/digestsubscription"
	"github.com/Phixsura/attune/internal/handlers/console/enrichconfig"
	consoleenrichmentruntime "github.com/Phixsura/attune/internal/handlers/console/enrichmentruntime"
	consoleexternalsync "github.com/Phixsura/attune/internal/handlers/console/externalsync"
	"github.com/Phixsura/attune/internal/handlers/console/feedback"
	"github.com/Phixsura/attune/internal/handlers/console/feedbackjob"
	consolegdpr "github.com/Phixsura/attune/internal/handlers/console/gdpr"
	consoleguardpolicy "github.com/Phixsura/attune/internal/handlers/console/guardpolicy"
	consoleinbound "github.com/Phixsura/attune/internal/handlers/console/inbound"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	consolellmconfig "github.com/Phixsura/attune/internal/handlers/console/llmconfig"
	consolemcpclient "github.com/Phixsura/attune/internal/handlers/console/mcpclient"
	"github.com/Phixsura/attune/internal/handlers/console/me"
	"github.com/Phixsura/attune/internal/handlers/console/member"
	"github.com/Phixsura/attune/internal/handlers/console/notifytarget"
	consoleoidc "github.com/Phixsura/attune/internal/handlers/console/oidc"
	consoleoutbox "github.com/Phixsura/attune/internal/handlers/console/outbox"
	consolepublicvisibility "github.com/Phixsura/attune/internal/handlers/console/publicvisibility"
	consolerequestnotification "github.com/Phixsura/attune/internal/handlers/console/requestnotification"
	"github.com/Phixsura/attune/internal/handlers/console/system"
	consoletag "github.com/Phixsura/attune/internal/handlers/console/tag"
	consoletagassignment "github.com/Phixsura/attune/internal/handlers/console/tagassignment"
	"github.com/Phixsura/attune/internal/handlers/console/usage"
	consoleworkflow "github.com/Phixsura/attune/internal/handlers/console/workflow"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	"github.com/Phixsura/attune/internal/repo/admin"
)

// passAdminReader is a stub adminReader that always returns an admin
// so the requireAdmin/requireAdminStrict/requireViewer middleware lets
// requests through to the handler closures.
type passAdminReader struct{}

func (passAdminReader) GetByID(_ context.Context, _ string) (admin.Admin, error) {
	return admin.Admin{ID: "user-http-test", Role: "admin"}, nil
}

// dispatchRouter builds a Router with zero-value handler structs for every
// subsystem so that mount* methods can register routes. Handler methods
// will panic on nil service dependencies, but middleware.Recoverer converts
// that to a 500. The closure code paths in router.go are covered before the
// panic. The admins field is set to passAdminReader so the legacy admin
// middleware forwards requests to the handler closures.
func dispatchRouter() *Router {
	return ptrext.Of(Router{
		login:                &auth.Handler{},
		changePassword:       &auth.ChangePasswordHandler{},
		me:                   &me.MeHandler{},
		auditLog:             &consoleauditlog.Handler{},
		apiKeys:              &apikey.APIKeysHandler{},
		notifyTargets:        &notifytarget.NotifyTargetsHandler{},
		digestSubscription:   &digestsubscription.Handler{},
		feedback:             &feedback.FeedbackHandler{},
		feedbackBatch:        &feedback.BatchHandler{},
		feedbackSearch:       &feedback.SearchHandler{},
		qualityActions:       &feedback.QualityActionHandler{},
		customerRequests:     &consolecustomerrequest.Handler{},
		publicVisibility:     &consolepublicvisibility.Handler{},
		requestNotifications: &consolerequestnotification.Handler{},
		feedbackJob:          &feedbackjob.Handler{},
		gdpr:                 &consolegdpr.Handler{},
		usage:                &usage.UsageHandler{},
		enrichConfig:         &enrichconfig.Handler{},
		enrichmentRuntime:    &consoleenrichmentruntime.Handler{},
		externalSync:         &consoleexternalsync.Handler{},
		guardPolicies:        &consoleguardpolicy.Handler{},
		inbound:              &consoleinbound.Handler{},
		llmConfig:            &consolellmconfig.Handler{},
		clusters:             &clusters.ClustersHandler{},
		outbox:               &consoleoutbox.Handler{},
		tags:                 &consoletag.Handler{},
		tagAssignments:       &consoletagassignment.Handler{},
		workflow:             &consoleworkflow.Handler{},
		members:              &member.Handler{},
		admins:               passAdminReader{},
	})
}

// authedHTTPReq builds an httptest request with a session.AuthCtx injected into
// the context, bypassing the cookie-based RequireSession middleware.
func authedHTTPReq(method, path string, body string) *http.Request {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req.WithContext(session.WithAuthCtx(req.Context(), ptrext.Of(session.AuthCtx{
		TenantID: "tenant-http-test",
		UserID:   "user-http-test",
		UserType: "admin",
	})))
}

// serveAndAssertDispatched sends an HTTP request through a chi.Router
// with Recoverer, verifying the closure code path executes. Any status
// other than 404 (route not found) or 405 (method not allowed) proves
// the route was matched and the dispatcher closure ran.
func serveAndAssertDispatched(t *testing.T, mux chi.Router, method, path, body string) {
	t.Helper()
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authedHTTPReq(method, path, body))
	require.NotEqual(t, http.StatusNotFound, w.Code,
		"route %s %s returned 404 — route not mounted", method, path)
	require.NotEqual(t, http.StatusMethodNotAllowed, w.Code,
		"route %s %s returned 405 — method not allowed", method, path)
}

// newRecovererMux returns a chi.Router with Recoverer so nil-pointer panics
// from zero-value handlers produce 500 instead of crashing the test.
func newRecovererMux() chi.Router {
	mux := chi.NewRouter()
	mux.Use(middleware.Recoverer)
	return mux
}

// ---------- mountSession routes ----------

func TestRouterHTTPDispatch_Session(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountSession(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /me", http.MethodGet, "/me", ""},
		{"POST /logout", http.MethodPost, "/logout", ""},
		{"POST /me/change-password", http.MethodPost, "/me/change-password", `{}`},
		{"GET /usage", http.MethodGet, "/usage", ""},
		{"GET /llm-usage", http.MethodGet, "/llm-usage", ""},
		{"GET /quality-actions", http.MethodGet, "/quality-actions", ""},
		{"POST /quality-actions/update", http.MethodPost, "/quality-actions/update", `{}`},
		{"GET /customer-requests/", http.MethodGet, "/customer-requests/", ""},
		{"GET /customer-requests/11111111-1111-1111-1111-111111111111", http.MethodGet, "/customer-requests/11111111-1111-1111-1111-111111111111", ""},
		{"GET /customer-requests/saved-views", http.MethodGet, "/customer-requests/saved-views", ""},
		{"POST /customer-requests/saved-views", http.MethodPost, "/customer-requests/saved-views", `{"name":"Planning"}`},
		{"PUT /customer-requests/saved-views/{view_id}", http.MethodPut, "/customer-requests/saved-views/view-1", `{"name":"Planning"}`},
		{"DELETE /customer-requests/saved-views/{view_id}", http.MethodDelete, "/customer-requests/saved-views/view-1", ""},
		{"POST /customer-requests/", http.MethodPost, "/customer-requests/", `{}`},
		{"PATCH /customer-requests/11111111-1111-1111-1111-111111111111", http.MethodPatch, "/customer-requests/11111111-1111-1111-1111-111111111111", `{}`},
		{"POST /customer-requests:promote-feedback", http.MethodPost, "/customer-requests:promote-feedback", `{}`},
		{"POST /customer-requests/11111111-1111-1111-1111-111111111111/feedback", http.MethodPost, "/customer-requests/11111111-1111-1111-1111-111111111111/feedback", `{}`},
		{"DELETE /customer-requests/11111111-1111-1111-1111-111111111111/feedback/1", http.MethodDelete, "/customer-requests/11111111-1111-1111-1111-111111111111/feedback/1", ""},
		{"POST /customer-requests/11111111-1111-1111-1111-111111111111/customers", http.MethodPost, "/customer-requests/11111111-1111-1111-1111-111111111111/customers", `{}`},
		{"DELETE /customer-requests/11111111-1111-1111-1111-111111111111/customers/22222222-2222-2222-2222-222222222222", http.MethodDelete, "/customer-requests/11111111-1111-1111-1111-111111111111/customers/22222222-2222-2222-2222-222222222222", ""},
		{"POST /customer-requests/11111111-1111-1111-1111-111111111111/votes", http.MethodPost, "/customer-requests/11111111-1111-1111-1111-111111111111/votes", `{}`},
		{"DELETE /customer-requests/11111111-1111-1111-1111-111111111111/votes/22222222-2222-2222-2222-222222222222", http.MethodDelete, "/customer-requests/11111111-1111-1111-1111-111111111111/votes/22222222-2222-2222-2222-222222222222", ""},
		{"POST /customer-requests/11111111-1111-1111-1111-111111111111/notes", http.MethodPost, "/customer-requests/11111111-1111-1111-1111-111111111111/notes", `{}`},
		{"DELETE /customer-requests/11111111-1111-1111-1111-111111111111/notes/22222222-2222-2222-2222-222222222222", http.MethodDelete, "/customer-requests/11111111-1111-1111-1111-111111111111/notes/22222222-2222-2222-2222-222222222222", ""},
		{"POST /customer-requests/11111111-1111-1111-1111-111111111111:merge", http.MethodPost, "/customer-requests/11111111-1111-1111-1111-111111111111:merge", `{}`},
		{"POST /customer-requests/11111111-1111-1111-1111-111111111111/issue-links", http.MethodPost, "/customer-requests/11111111-1111-1111-1111-111111111111/issue-links", `{}`},
		{"DELETE /customer-requests/11111111-1111-1111-1111-111111111111/issue-links/22222222-2222-2222-2222-222222222222", http.MethodDelete, "/customer-requests/11111111-1111-1111-1111-111111111111/issue-links/22222222-2222-2222-2222-222222222222", ""},
		{"POST /customer-requests/11111111-1111-1111-1111-111111111111/issue-links/22222222-2222-2222-2222-222222222222:record-sync", http.MethodPost, "/customer-requests/11111111-1111-1111-1111-111111111111/issue-links/22222222-2222-2222-2222-222222222222:record-sync", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountLLMConfig routes ----------

func TestRouterHTTPDispatch_LLMConfig(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountLLMConfig(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /llm/channels", http.MethodGet, "/llm/channels", ""},
		{"POST /llm/channels", http.MethodPost, "/llm/channels", `{}`},
		{"GET /llm/channels/ch1", http.MethodGet, "/llm/channels/ch1", ""},
		{"PATCH /llm/channels/ch1", http.MethodPatch, "/llm/channels/ch1", `{}`},
		{"DELETE /llm/channels/ch1", http.MethodDelete, "/llm/channels/ch1", ""},
		{"POST /llm/channels/ch1/test", http.MethodPost, "/llm/channels/ch1/test", `{}`},
		{"GET /llm/channels/ch1/models", http.MethodGet, "/llm/channels/ch1/models", ""},
		// LLM Abilities
		{"GET /llm/channels/ch1/abilities", http.MethodGet, "/llm/channels/ch1/abilities", ""},
		{"PUT /llm/channels/ch1/abilities", http.MethodPut, "/llm/channels/ch1/abilities", `{}`},
		{"POST /llm/channels/ch1/abilities/delete", http.MethodPost, "/llm/channels/ch1/abilities/delete", `{}`},
		// LLM Routes
		{"GET /llm/routes", http.MethodGet, "/llm/routes", ""},
		{"PUT /llm/routes", http.MethodPut, "/llm/routes", `{}`},
		{"POST /llm/routes/delete", http.MethodPost, "/llm/routes/delete", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountAPIKeys routes ----------

func TestRouterHTTPDispatch_APIKeys(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountAPIKeys(mux)

	cases := []struct {
		name, method, path, body string
	}{
		// Core routes
		{"GET /api-keys/", http.MethodGet, "/api-keys/", ""},
		{"POST /api-keys/", http.MethodPost, "/api-keys/", `{}`},
		{"DELETE /api-keys/k1", http.MethodDelete, "/api-keys/k1", ""},
		{"GET /api-keys/scopes", http.MethodGet, "/api-keys/scopes", ""},
		{"GET /api-keys/presets", http.MethodGet, "/api-keys/presets", ""},
		{"GET /api-keys/expiring", http.MethodGet, "/api-keys/expiring", ""},
		{"GET /api-keys/expiring?within=7d", http.MethodGet, "/api-keys/expiring?within=7d", ""},
		{"GET /api-keys/event-subscriptions", http.MethodGet, "/api-keys/event-subscriptions", ""},
		{"POST /api-keys/event-subscriptions", http.MethodPost, "/api-keys/event-subscriptions", `{}`},
		{"GET /api-keys/leaks", http.MethodGet, "/api-keys/leaks", ""},
		// Advanced routes
		{"POST /api-keys/k1/rotate", http.MethodPost, "/api-keys/k1/rotate", `{}`},
		{"GET /api-keys/k1/logs", http.MethodGet, "/api-keys/k1/logs", ""},
		{"GET /api-keys/k1/logs?limit=10", http.MethodGet, "/api-keys/k1/logs?limit=10", ""},
		{"PATCH /api-keys/k1/environment", http.MethodPatch, "/api-keys/k1/environment", `{}`},
		{"GET /api-keys/policy", http.MethodGet, "/api-keys/policy", ""},
		{"PUT /api-keys/policy", http.MethodPut, "/api-keys/policy", `{}`},
		{"GET /api-keys/approvals", http.MethodGet, "/api-keys/approvals", ""},
		{"GET /api-keys/approvals?status=pending", http.MethodGet, "/api-keys/approvals?status=pending", ""},
		{"POST /api-keys/approvals", http.MethodPost, "/api-keys/approvals", `{}`},
		{"POST /api-keys/approvals/a1/review", http.MethodPost, "/api-keys/approvals/a1/review", `{}`},
		{"GET /api-keys/analytics", http.MethodGet, "/api-keys/analytics", ""},
		{"GET /api-keys/analytics?start=2025-01-01&end=2025-12-31", http.MethodGet, "/api-keys/analytics?start=2025-01-01&end=2025-12-31", ""},
		// Per-key basic routes
		{"GET /api-keys/k1/tags", http.MethodGet, "/api-keys/k1/tags", ""},
		{"PUT /api-keys/k1/tags", http.MethodPut, "/api-keys/k1/tags", `{}`},
		{"PUT /api-keys/k1/budget", http.MethodPut, "/api-keys/k1/budget", `{}`},
		{"POST /api-keys/k1/temp-token", http.MethodPost, "/api-keys/k1/temp-token", `{}`},
		{"POST /api-keys/k1/project", http.MethodPost, "/api-keys/k1/project", `{}`},
		{"GET /api-keys/k1/analytics", http.MethodGet, "/api-keys/k1/analytics", ""},
		{"GET /api-keys/k1/analytics?start=2025-01-01&end=2025-12-31", http.MethodGet, "/api-keys/k1/analytics?start=2025-01-01&end=2025-12-31", ""},
		// Per-key security routes
		{"GET /api-keys/k1/rotation-schedule", http.MethodGet, "/api-keys/k1/rotation-schedule", ""},
		{"POST /api-keys/k1/rotation-schedule", http.MethodPost, "/api-keys/k1/rotation-schedule", `{}`},
		{"GET /api-keys/k1/unused-scopes", http.MethodGet, "/api-keys/k1/unused-scopes", ""},
		{"GET /api-keys/k1/signing-keys", http.MethodGet, "/api-keys/k1/signing-keys", ""},
		{"POST /api-keys/k1/signing-keys", http.MethodPost, "/api-keys/k1/signing-keys", `{}`},
		{"GET /api-keys/k1/health", http.MethodGet, "/api-keys/k1/health", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountAPIKeyRelatedResources routes ----------

func TestRouterHTTPDispatch_APIKeyRelated(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountAPIKeyRelatedResources(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /service-accounts/", http.MethodGet, "/service-accounts/", ""},
		{"POST /service-accounts/", http.MethodPost, "/service-accounts/", `{}`},
		{"PATCH /service-accounts/{id}", http.MethodPatch, "/service-accounts/sa-1", `{}`},
		{"DELETE /service-accounts/{id}", http.MethodDelete, "/service-accounts/sa-1", ""},
		{"GET /projects/", http.MethodGet, "/projects/", ""},
		{"POST /projects/", http.MethodPost, "/projects/", `{}`},
		{"GET /oauth2/clients/", http.MethodGet, "/oauth2/clients/", ""},
		{"POST /oauth2/clients/", http.MethodPost, "/oauth2/clients/", `{}`},
		{"GET /secret-managers/", http.MethodGet, "/secret-managers/", ""},
		{"POST /secret-managers/", http.MethodPost, "/secret-managers/", `{}`},
		{"GET /managed-identities/", http.MethodGet, "/managed-identities/", ""},
		{"POST /managed-identities/", http.MethodPost, "/managed-identities/", `{}`},
		{"GET /siem-integrations/", http.MethodGet, "/siem-integrations/", ""},
		{"POST /siem-integrations/", http.MethodPost, "/siem-integrations/", `{}`},
		{"GET /ai-agents/", http.MethodGet, "/ai-agents/", ""},
		{"POST /ai-agents/", http.MethodPost, "/ai-agents/", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountAuditLog routes ----------

func TestRouterHTTPDispatch_AuditLog(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountAuditLog(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /audit-log/", http.MethodGet, "/audit-log/", ""},
		{"GET /audit-log/export.csv", http.MethodGet, "/audit-log/export.csv", ""},
		{"GET /audit-log/views/", http.MethodGet, "/audit-log/views/", ""},
		{"POST /audit-log/views/", http.MethodPost, "/audit-log/views/", `{"name":"View 1"}`},
		{"PUT /audit-log/views/{id}", http.MethodPut, "/audit-log/views/view-1", `{"name":"View 1","state":{}}`},
		{"DELETE /audit-log/views/{id}", http.MethodDelete, "/audit-log/views/view-1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountGDPR routes ----------

func TestRouterHTTPDispatch_GDPR(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountGDPR(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /gdpr/requests", http.MethodGet, "/gdpr/requests", ""},
		{"GET /gdpr/operations", http.MethodGet, "/gdpr/operations", ""},
		{"POST /gdpr/step-up/verify", http.MethodPost, "/gdpr/step-up/verify", `{}`},
		{"POST /gdpr/requests/r1/cancel", http.MethodPost, "/gdpr/requests/r1/cancel", ""},
		{"POST /gdpr/export", http.MethodPost, "/gdpr/export", `{}`},
		{"GET /gdpr/exports/j1", http.MethodGet, "/gdpr/exports/j1", ""},
		{"GET /gdpr/exports/j1/download", http.MethodGet, "/gdpr/exports/j1/download", ""},
		{"POST /gdpr/exports/j1/revoke", http.MethodPost, "/gdpr/exports/j1/revoke", ""},
		{"POST /gdpr/delete", http.MethodPost, "/gdpr/delete", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountNotifyTargets routes ----------

func TestRouterHTTPDispatch_NotifyTargets(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountNotifyTargets(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /notify-targets/", http.MethodGet, "/notify-targets/", ""},
		{"POST /notify-targets/", http.MethodPost, "/notify-targets/", `{}`},
		{"PATCH /notify-targets/n1", http.MethodPatch, "/notify-targets/n1", `{}`},
		{"DELETE /notify-targets/n1", http.MethodDelete, "/notify-targets/n1", ""},
		{"POST /notify-targets/n1/test", http.MethodPost, "/notify-targets/n1/test", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountDigestSubscription routes ----------

func TestRouterHTTPDispatch_DigestSubscription(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountDigestSubscription(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /digest-subscription", http.MethodGet, "/digest-subscription", ""},
		{"PUT /digest-subscription", http.MethodPut, "/digest-subscription", `{}`},
		{"DELETE /digest-subscription", http.MethodDelete, "/digest-subscription", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountFeedback routes ----------

func TestRouterHTTPDispatch_Feedback(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountFeedback(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /feedback/", http.MethodGet, "/feedback/", ""},
		{"GET /feedback/stats", http.MethodGet, "/feedback/stats", ""},
		{"GET /feedback/1", http.MethodGet, "/feedback/1", ""},
		{"POST /feedback/1/reply-draft/regenerate", http.MethodPost, "/feedback/1/reply-draft/regenerate", ""},
		{"POST /feedback/1/retry-enrichment", http.MethodPost, "/feedback/1/retry-enrichment", ""},
		{"POST /feedback/1/transition", http.MethodPost, "/feedback/1/transition", `{}`},
		{"GET /feedback/1/audit", http.MethodGet, "/feedback/1/audit", ""},
		{"GET /feedback/1/audit?cursor=c&limit=10", http.MethodGet, "/feedback/1/audit?cursor=c&limit=10", ""},
		// Batch routes
		{"POST /feedback/batch", http.MethodPost, "/feedback/batch", `{}`},
		{"POST /feedback/search", http.MethodPost, "/feedback/search", `{}`},
		// Tag routes
		{"POST /feedback/1/tags", http.MethodPost, "/feedback/1/tags", `{}`},
		{"DELETE /feedback/1/tags/t1", http.MethodDelete, "/feedback/1/tags/t1", ""},
		// Batch tags
		{"POST /feedback/batch/tags", http.MethodPost, "/feedback/batch/tags", `{}`},
		// Batch transition
		{"POST /feedback/transition/batch", http.MethodPost, "/feedback/transition/batch", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountEnrichConfig routes ----------

func TestRouterHTTPDispatch_EnrichConfig(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountEnrichConfig(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /enrich-config/", http.MethodGet, "/enrich-config/", ""},
		{"PUT /enrich-config/", http.MethodPut, "/enrich-config/", `{}`},
		{"POST /enrich-config/preview", http.MethodPost, "/enrich-config/preview", `{}`},
		{"POST /enrich-config/eval-suggestions:analyze", http.MethodPost, "/enrich-config/eval-suggestions:analyze", `{}`},
		{"POST /enrich-config/promote", http.MethodPost, "/enrich-config/promote", `{}`},
		{"POST /enrich-config/versions/v1:activate", http.MethodPost, "/enrich-config/versions/v1:activate", ""},
		{"GET /enrich-config/versions", http.MethodGet, "/enrich-config/versions", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountEnrichmentRuntime routes ----------

func TestRouterHTTPDispatch_EnrichmentRuntime(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountEnrichmentRuntime(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /enrichment-runtime/", http.MethodGet, "/enrichment-runtime/", ""},
		{"PUT /enrichment-runtime/", http.MethodPut, "/enrichment-runtime/", `{}`},
		{"POST /enrichment-runtime/reset", http.MethodPost, "/enrichment-runtime/reset", `{}`},
		{"POST /enrichment-runtime/rollback", http.MethodPost, "/enrichment-runtime/rollback", `{}`},
		// Legacy colon-action routes
		{"POST /enrichment-runtime:reset", http.MethodPost, "/enrichment-runtime:reset", `{}`},
		{"POST /enrichment-runtime:rollback", http.MethodPost, "/enrichment-runtime:rollback", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountGuardPolicies routes ----------

func TestRouterHTTPDispatch_GuardPolicies(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountGuardPolicies(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /guard-policies/", http.MethodGet, "/guard-policies/", ""},
		{"POST /guard-policies/", http.MethodPost, "/guard-policies/", `{}`},
		{"PUT /guard-policies/", http.MethodPut, "/guard-policies/", `{}`},
		{"PATCH /guard-policies/g1", http.MethodPatch, "/guard-policies/g1", `{}`},
		{"DELETE /guard-policies/g1", http.MethodDelete, "/guard-policies/g1", ""},
		{"POST /guard-policies/effective", http.MethodPost, "/guard-policies/effective", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountInbound routes ----------

func TestRouterHTTPDispatch_Inbound(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountInbound(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /inbound/sources/", http.MethodGet, "/inbound/sources/", ""},
		{"POST /inbound/sources/", http.MethodPost, "/inbound/sources/", `{}`},
		{"POST /inbound/sources/test-connection", http.MethodPost, "/inbound/sources/test-connection", `{}`},
		{"POST /inbound/sources/slack/discover", http.MethodPost, "/inbound/sources/slack/discover", `{}`},
		{"GET /inbound/sources/s1", http.MethodGet, "/inbound/sources/s1", ""},
		{"DELETE /inbound/sources/s1", http.MethodDelete, "/inbound/sources/s1", ""},
		{"POST /inbound/sources/s1/rotate-secret", http.MethodPost, "/inbound/sources/s1/rotate-secret", ""},
		{"POST /inbound/sources/s1/pause", http.MethodPost, "/inbound/sources/s1/pause", ""},
		{"POST /inbound/sources/s1/resume", http.MethodPost, "/inbound/sources/s1/resume", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountExternalSync routes ----------

func TestRouterHTTPDispatch_ExternalSync(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountExternalSync(mux)

	connectionID := "11111111-1111-4111-8111-111111111111"
	installationID := "77777777-7777-4777-8777-777777777777"
	mappingID := "22222222-2222-4222-8222-222222222222"
	runID := "33333333-3333-4333-8333-333333333333"
	failureID := "44444444-4444-4444-8444-444444444444"
	conflictID := "55555555-5555-4555-8555-555555555555"
	eventID := "66666666-6666-4666-8666-666666666666"

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /external-sync/provider-installations", http.MethodGet, "/external-sync/provider-installations", ""},
		{"POST /external-sync/provider-installations", http.MethodPost, "/external-sync/provider-installations", `{}`},
		{"DELETE /external-sync/provider-installations/{id}", http.MethodDelete, "/external-sync/provider-installations/" + installationID, ""},
		{"POST /external-sync/provider-installations/{id}:qualify", http.MethodPost, "/external-sync/provider-installations/" + installationID + ":qualify", ""},
		{"GET /external-sync/provider-installations/{id}/resources", http.MethodGet, "/external-sync/provider-installations/" + installationID + "/resources", ""},
		{
			"POST /external-sync/provider-installations/{id}/resources:select",
			http.MethodPost,
			"/external-sync/provider-installations/" + installationID + "/resources:select",
			`{}`,
		},
		{"GET /external-sync/connections", http.MethodGet, "/external-sync/connections", ""},
		{"POST /external-sync/connections", http.MethodPost, "/external-sync/connections", `{}`},
		{"PATCH /external-sync/connections/{id}", http.MethodPatch, "/external-sync/connections/" + connectionID, `{}`},
		{"DELETE /external-sync/connections/{id}", http.MethodDelete, "/external-sync/connections/" + connectionID, ""},
		{"POST /external-sync/connections/{id}:test", http.MethodPost, "/external-sync/connections/" + connectionID + ":test", ""},
		{"POST /external-sync/connections/{id}:resume", http.MethodPost, "/external-sync/connections/" + connectionID + ":resume", ""},
		{"POST /external-sync/connections/{id}:qualify", http.MethodPost, "/external-sync/connections/" + connectionID + ":qualify", ""},
		{"GET /external-sync/connections/{id}/schema", http.MethodGet, "/external-sync/connections/" + connectionID + "/schema", ""},
		{"GET /external-sync/mappings", http.MethodGet, "/external-sync/mappings?connection_id=" + connectionID, ""},
		{"PUT /external-sync/mappings/{id}", http.MethodPut, "/external-sync/mappings/" + mappingID, `{}`},
		{"POST /external-sync/mappings/{id}:preview", http.MethodPost, "/external-sync/mappings/" + mappingID + ":preview", `{}`},
		{"POST /external-sync/mappings/{id}:reset-cursor", http.MethodPost, "/external-sync/mappings/" + mappingID + ":reset-cursor", ""},
		{"POST /external-sync/mappings/{id}:backfill", http.MethodPost, "/external-sync/mappings/" + mappingID + ":backfill", `{}`},
		{"POST /external-sync/runs", http.MethodPost, "/external-sync/runs", `{}`},
		{
			"GET /external-sync/runs with filters",
			http.MethodGet,
			"/external-sync/runs?connection_id=" + connectionID + "&mapping_id=" + mappingID + "&status=failed&before_id=" + runID + "&limit=25",
			"",
		},
		{"GET /external-sync/runs/{id}", http.MethodGet, "/external-sync/runs/" + runID, ""},
		{"POST /external-sync/records:timeline", http.MethodPost, "/external-sync/records:timeline", `{}`},
		{"POST /external-sync/runs/{id}:retry", http.MethodPost, "/external-sync/runs/" + runID + ":retry", ""},
		{"POST /external-sync/failures/{id}:retry", http.MethodPost, "/external-sync/failures/" + failureID + ":retry", ""},
		{"POST /external-sync/conflicts/{id}:resolve", http.MethodPost, "/external-sync/conflicts/" + conflictID + ":resolve", `{}`},
		{"POST /external-sync/conflicts:batch-resolve", http.MethodPost, "/external-sync/conflicts:batch-resolve", `{}`},
		{"GET /external-sync/events", http.MethodGet, "/external-sync/events?connection_id=" + connectionID + "&status=received&before_id=" + eventID + "&limit=25", ""},
		{"GET /external-sync/events/{id}", http.MethodGet, "/external-sync/events/" + eventID, ""},
		{"POST /external-sync/events/{id}:replay", http.MethodPost, "/external-sync/events/" + eventID + ":replay", ""},
		{"GET /external-sync/health", http.MethodGet, "/external-sync/health", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountPublicVisibility routes ----------

func TestRouterHTTPDispatch_PublicVisibility(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountPublicVisibility(mux)

	id := "11111111-1111-1111-1111-111111111111"
	cases := []struct {
		name, method, path, body string
	}{
		{"GET /public-visibility/policy", http.MethodGet, "/public-visibility/policy", ""},
		{"PUT /public-visibility/policy", http.MethodPut, "/public-visibility/policy", `{}`},
		{"GET /public-visibility/moderation", http.MethodGet, "/public-visibility/moderation", ""},
		{"GET /public-visibility/views", http.MethodGet, "/public-visibility/views", ""},
		{"POST /public-visibility/views", http.MethodPost, "/public-visibility/views", `{}`},
		{"PUT /public-visibility/views/{id}", http.MethodPut, "/public-visibility/views/view-1", `{}`},
		{"DELETE /public-visibility/views/{id}", http.MethodDelete, "/public-visibility/views/view-1", ""},
		{"GET /public-visibility/requests/{request_id}/profile", http.MethodGet, "/public-visibility/requests/" + id + "/profile", ""},
		{"PUT /public-visibility/requests/{request_id}/profile", http.MethodPut, "/public-visibility/requests/" + id + "/profile", `{}`},
		{"POST /public-visibility/moderation/{id}:approve", http.MethodPost, "/public-visibility/moderation/" + id + ":approve", `{}`},
		{"POST /public-visibility/moderation/{id}:reject", http.MethodPost, "/public-visibility/moderation/" + id + ":reject", `{}`},
		{"POST /public-visibility/moderation/{id}:hide", http.MethodPost, "/public-visibility/moderation/" + id + ":hide", `{}`},
		{"POST /public-visibility/moderation/{id}:mark-spam", http.MethodPost, "/public-visibility/moderation/" + id + ":mark-spam", `{}`},
		{"POST /public-visibility/moderation/{id}:restore", http.MethodPost, "/public-visibility/moderation/" + id + ":restore", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountRequestNotifications routes ----------

func TestRouterHTTPDispatch_RequestNotifications(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountRequestNotifications(mux)

	id := "11111111-1111-1111-1111-111111111111"
	cases := []struct {
		name, method, path, body string
	}{
		{"GET /request-notifications/settings", http.MethodGet, "/request-notifications/settings", ""},
		{"PUT /request-notifications/settings", http.MethodPut, "/request-notifications/settings", `{}`},
		{"GET /request-notifications/sender", http.MethodGet, "/request-notifications/sender", ""},
		{"PUT /request-notifications/sender", http.MethodPut, "/request-notifications/sender", `{}`},
		{"POST /request-notifications/sender:verify", http.MethodPost, "/request-notifications/sender:verify", `{"id":"` + id + `"}`},
		{"GET /request-notifications/webhook-targets", http.MethodGet, "/request-notifications/webhook-targets", ""},
		{"POST /request-notifications/webhook-targets", http.MethodPost, "/request-notifications/webhook-targets", `{}`},
		{"PATCH /request-notifications/webhook-targets/{id}", http.MethodPatch, "/request-notifications/webhook-targets/" + id, `{}`},
		{"DELETE /request-notifications/webhook-targets/{id}", http.MethodDelete, "/request-notifications/webhook-targets/" + id, ""},
		{"POST /request-notifications/webhook-targets/{id}:test", http.MethodPost, "/request-notifications/webhook-targets/" + id + ":test", `{}`},
		{"POST /request-notifications/preview", http.MethodPost, "/request-notifications/preview", `{}`},
		{"POST /request-notifications/publish", http.MethodPost, "/request-notifications/publish", `{}`},
		{"GET /request-notifications/deliveries", http.MethodGet, "/request-notifications/deliveries?status=failed&limit=25&before_id=99&request_id=" + id + "&channel=webhook", ""},
		{"POST /request-notifications/deliveries/{id}:retry", http.MethodPost, "/request-notifications/deliveries/1:retry", `{}`},
		{"GET /request-notifications/requests/{request_id}/subscribers", http.MethodGet, "/request-notifications/requests/" + id + "/subscribers", ""},
		{"POST /request-notifications/subscribers/{contact_id}:suppress", http.MethodPost, "/request-notifications/subscribers/" + id + ":suppress", `{}`},
		{"POST /request-notifications/provider-events:suppress", http.MethodPost, "/request-notifications/provider-events:suppress", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountClusters routes ----------

func TestRouterHTTPDispatch_Clusters(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountClusters(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /clusters/", http.MethodGet, "/clusters/", ""},
		{"GET /clusters/c1/members", http.MethodGet, "/clusters/c1/members", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountOutbox routes ----------

func TestRouterHTTPDispatch_Outbox(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountOutbox(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /outbox/deliveries", http.MethodGet, "/outbox/deliveries", ""},
		{"GET /outbox/deliveries?status=dead&limit=50&before_id=123", http.MethodGet, "/outbox/deliveries?status=dead&limit=50&before_id=123", ""},
		{"POST /outbox/1/retry", http.MethodPost, "/outbox/1/retry", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountTags routes ----------

func TestRouterHTTPDispatch_Tags(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountTags(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /tags/", http.MethodGet, "/tags/", ""},
		{"GET /tags/?include_archived=true", http.MethodGet, "/tags/?include_archived=true", ""},
		{"POST /tags/", http.MethodPost, "/tags/", `{}`},
		{"PATCH /tags/t1", http.MethodPatch, "/tags/t1", `{}`},
		{"DELETE /tags/t1", http.MethodDelete, "/tags/t1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountJobs routes ----------

func TestRouterHTTPDispatch_Jobs(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountJobs(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /jobs/", http.MethodGet, "/jobs/", ""},
		{"GET /jobs/j1", http.MethodGet, "/jobs/j1", ""},
		{"POST /jobs/j1/cancel", http.MethodPost, "/jobs/j1/cancel", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountWorkflow routes ----------

func TestRouterHTTPDispatch_Workflow(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountWorkflow(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /workflow/states", http.MethodGet, "/workflow/states", ""},
		{"GET /workflow/states?include_archived=true", http.MethodGet, "/workflow/states?include_archived=true", ""},
		{"POST /workflow/states", http.MethodPost, "/workflow/states", `{}`},
		{"PATCH /workflow/states/s1", http.MethodPatch, "/workflow/states/s1", `{}`},
		{"DELETE /workflow/states/s1", http.MethodDelete, "/workflow/states/s1", ""},
		{"GET /workflow/transitions", http.MethodGet, "/workflow/transitions", ""},
		{"PUT /workflow/transitions", http.MethodPut, "/workflow/transitions", `{}`},
		{"POST /workflow/seed", http.MethodPost, "/workflow/seed", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountMembers routes ----------

func TestRouterHTTPDispatch_Members(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountMembers(mux)

	cases := []struct {
		name, method, path, body string
	}{
		{"GET /members/", http.MethodGet, "/members/", ""},
		{"POST /members/", http.MethodPost, "/members/", `{}`},
		{"PATCH /members/m1", http.MethodPatch, "/members/m1", `{}`},
		{"DELETE /members/m1", http.MethodDelete, "/members/m1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, tc.body)
		})
	}
}

// ---------- mountExternalSync provider routes ----------

func TestRouterHTTPDispatch_ExternalSyncProviders(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := newRecovererMux()
	r.mountExternalSync(mux)

	serveAndAssertDispatched(t, mux, http.MethodGet, "/external-sync/providers", "")
}

// ---------- authProviders handler ----------

func TestRouterHTTPDispatch_AuthProviders(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	mux := chi.NewRouter()
	mux.Get("/auth/providers", r.authProviders)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/providers", nil)
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Type"), "application/json")
}

// ---------- bindListDeliveriesRequest ----------

func TestBindListDeliveriesRequest(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		query string
		err   bool
	}{
		{"empty query", "", false},
		{"with status", "?status=dead&status=failed", false},
		{"with limit", "?limit=50", false},
		{"with before_id", "?before_id=123", false},
		{"with all params", "?status=dead&limit=25&before_id=99", false},
		{"invalid limit", "?limit=abc", true},
		{"invalid before_id", "?before_id=xyz", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/outbox/deliveries"+tc.query, nil)
			var dest attunev1.ListDeliveriesRequest
			err := bindListDeliveriesRequest(req, &dest) // ptrext:allow out-parameter
			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ---------- mountOIDC routes ----------

func TestRouterHTTPDispatch_OIDC(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	r.oidc = &consoleoidc.Handler{}
	mux := newRecovererMux()
	r.mountOIDC(mux)

	cases := []struct {
		name, method, path string
	}{
		{"GET /auth/oidc/start", http.MethodGet, "/auth/oidc/start"},
		{"GET /auth/oidc/callback", http.MethodGet, "/auth/oidc/callback"},
		{"GET /auth/oidc/health", http.MethodGet, "/auth/oidc/health"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serveAndAssertDispatched(t, mux, tc.method, tc.path, "")
		})
	}
}

// ---------- mountPreflight route ----------

func TestRouterHTTPDispatch_Preflight(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	r.preflight = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.releaseInfo = system.NewReleaseHandler(nil)
	mux := newRecovererMux()
	r.mountPreflight(mux)

	serveAndAssertDispatched(t, mux, http.MethodGet, "/system/preflight", "")
	serveAndAssertDispatched(t, mux, http.MethodGet, "/system/release", "")
}

func TestRouterHTTPDispatch_ReleaseInfo(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	r.releaseInfo = system.NewReleaseHandler(nil)
	mux := newRecovererMux()
	r.mountPreflight(mux)

	serveAndAssertDispatched(t, mux, http.MethodGet, "/system/release", "")
}

// ---------- authProviders with OIDC ----------

func TestRouterHTTPDispatch_AuthProviders_WithOIDC(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	r.oidc = &consoleoidc.Handler{}
	mux := newRecovererMux()
	mux.Get("/auth/providers", r.authProviders)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/providers", nil)
	mux.ServeHTTP(w, req)

	// Zero-value OIDC handler panics calling h.svc.OIDCOnly(); Recoverer
	// catches it. We verify the route was matched (not 404), which exercises
	// the r.oidc != nil branch in authProviders.
	require.NotEqual(t, http.StatusNotFound, w.Code)
}

// ---------- mountFeedbackTagRoutes nil guard ----------

func TestRouterHTTPDispatch_FeedbackTagRoutes_NilGuard(t *testing.T) {
	t.Parallel()
	// When tagAssignments is nil, mountFeedbackTagRoutes should not register
	// tag routes on the feedback router.
	r := ptrext.Of(Router{
		feedback:       &feedback.FeedbackHandler{},
		feedbackBatch:  &feedback.BatchHandler{},
		feedbackSearch: &feedback.SearchHandler{},
		admins:         passAdminReader{},
	})
	mux := newRecovererMux()
	r.mountFeedback(mux)

	// The tag-specific routes should return 404/405 (not mounted).
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authedHTTPReq(http.MethodPost, "/feedback/1/tags", `{}`))
	// Route is not mounted, so chi returns 405 (pattern matches but POST is not handled)
	// or 404; either proves the nil guard works.
	require.True(t, w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed,
		"tag route should not be mounted when tagAssignments is nil, got %d", w.Code)
}

// ---------- mountSession with changePassword nil ----------

func TestRouterHTTPDispatch_Session_NoChangePassword(t *testing.T) {
	t.Parallel()
	r := dispatchRouter()
	r.changePassword = nil
	mux := newRecovererMux()
	r.mountSession(mux)

	// The change-password route should not be mounted.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authedHTTPReq(http.MethodPost, "/me/change-password", `{}`))
	require.True(t, w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed,
		"change-password should not be mounted when handler is nil, got %d", w.Code)
}

// ---------- setter functions ----------

func TestRouterSetters(t *testing.T) {
	t.Parallel()
	r := ptrext.Of(Router{})

	t.Run("SetOutboxHandler", func(t *testing.T) {
		t.Parallel()
		h := &consoleoutbox.Handler{}
		r2 := ptrext.Of(Router{})
		r2.SetOutboxHandler(h)
		require.NotNil(t, r2.outbox)
	})
	t.Run("SetMCPClientHandler", func(t *testing.T) {
		t.Parallel()
		r2 := ptrext.Of(Router{})
		r2.SetMCPClientHandler(&consolemcpclient.Handler{})
		require.NotNil(t, r2.mcpClients)
	})
	t.Run("SetPreflightHandler", func(t *testing.T) {
		t.Parallel()
		h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
		r.SetPreflightHandler(h)
		require.NotNil(t, r.preflight)
	})
	t.Run("SetExternalSyncHandler", func(t *testing.T) {
		t.Parallel()
		r2 := ptrext.Of(Router{})
		r2.SetExternalSyncHandler(&consoleexternalsync.Handler{})
		require.NotNil(t, r2.externalSync)
	})
	t.Run("SetRecoveryHandler", func(t *testing.T) {
		t.Parallel()
		r2 := ptrext.Of(Router{})
		r2.SetRecoveryHandler(http.NotFoundHandler())
		require.NotNil(t, r2.recovery)
	})
	t.Run("SetReleaseInfoHandler", func(t *testing.T) {
		t.Parallel()
		r2 := ptrext.Of(Router{})
		r2.SetReleaseInfoHandler(http.NotFoundHandler())
		require.NotNil(t, r2.releaseInfo)
	})
	t.Run("SetBreakGlassHandler", func(t *testing.T) {
		t.Parallel()
		r2 := ptrext.Of(Router{})
		r2.SetBreakGlassHandler(&auth.BreakGlassHandler{})
		require.NotNil(t, r2.breakglass)
	})
	t.Run("SetBreakGlassAPIHandler", func(t *testing.T) {
		t.Parallel()
		r2 := ptrext.Of(Router{})
		r2.SetBreakGlassAPIHandler(&auth.BreakGlassAPIHandler{})
		require.NotNil(t, r2.breakglassAPI)
	})
	t.Run("SetSSOCutoverHandler", func(t *testing.T) {
		t.Parallel()
		r2 := ptrext.Of(Router{})
		r2.SetSSOCutoverHandler(&auth.SSOCutoverHandler{})
		require.NotNil(t, r2.ssoCutover)
	})
	t.Run("SetQualityActionHandler", func(t *testing.T) {
		t.Parallel()
		r2 := ptrext.Of(Router{})
		r2.SetQualityActionHandler(&feedback.QualityActionHandler{})
		require.NotNil(t, r2.qualityActions)
	})
	t.Run("SetCustomerRequestHandler", func(t *testing.T) {
		t.Parallel()
		r2 := ptrext.Of(Router{})
		r2.SetCustomerRequestHandler(&consolecustomerrequest.Handler{})
		require.NotNil(t, r2.customerRequests)
	})
	t.Run("SetPublicVisibilityHandler", func(t *testing.T) {
		t.Parallel()
		r2 := ptrext.Of(Router{})
		r2.SetPublicVisibilityHandler(&consolepublicvisibility.Handler{})
		require.NotNil(t, r2.publicVisibility)
	})
	t.Run("SetRequestNotificationHandler", func(t *testing.T) {
		t.Parallel()
		r2 := ptrext.Of(Router{})
		r2.SetRequestNotificationHandler(&consolerequestnotification.Handler{})
		require.NotNil(t, r2.requestNotifications)
	})
}

// ---------- nil-guard branches for optional handlers ----------

func TestRouterHTTPDispatch_NilGuards(t *testing.T) {
	t.Parallel()
	// A Router with all optional handlers nil exercises the early-return
	// branches in each mount function.
	r := ptrext.Of(Router{})
	mux := newRecovererMux()

	// Each mount function should be a no-op when its handler is nil.
	r.mountLLMConfig(mux)
	r.mountAuditLog(mux)
	r.mountGDPR(mux)
	r.mountInbound(mux)
	r.mountClusters(mux)
	r.mountOutbox(mux)
	r.mountTags(mux)
	r.mountJobs(mux)
	r.mountWorkflow(mux)
	r.mountMembers(mux)
	r.mountPreflight(mux)
	r.mountMCPClients(mux)
	r.mountOIDC(mux)
	r.mountExternalSync(mux)
	r.mountBreakGlass(mux)
	r.mountPublicVisibility(mux)
	r.mountRequestNotifications(mux)

	// Verify no routes were registered.
	routeCount := 0
	_ = chi.Walk(mux, func(_, _ string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routeCount++
		return nil
	})
	require.Equal(t, 0, routeCount, "nil handlers should not register any routes")
}
