// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package console

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/handlers/console/apikey"
	"github.com/Phixsura/attune/internal/handlers/console/auth"
	"github.com/Phixsura/attune/internal/handlers/console/digestsubscription"
	"github.com/Phixsura/attune/internal/handlers/console/enrichconfig"
	consoleexternalsync "github.com/Phixsura/attune/internal/handlers/console/externalsync"
	"github.com/Phixsura/attune/internal/handlers/console/feedback"
	consolegdpr "github.com/Phixsura/attune/internal/handlers/console/gdpr"
	consoleguardpolicy "github.com/Phixsura/attune/internal/handlers/console/guardpolicy"
	consoleinbound "github.com/Phixsura/attune/internal/handlers/console/inbound"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/handlers/console/me"
	"github.com/Phixsura/attune/internal/handlers/console/notifytarget"
	consoletag "github.com/Phixsura/attune/internal/handlers/console/tag"
	consoletagassignment "github.com/Phixsura/attune/internal/handlers/console/tagassignment"
	"github.com/Phixsura/attune/internal/handlers/console/usage"
	consoleworkflow "github.com/Phixsura/attune/internal/handlers/console/workflow"
)

var expectedAuthRoutes = []string{
	"POST /install/login",
	"GET /auth/providers",
	"GET /me",
	"POST /logout",
	"POST /me/change-password",
}

var expectedAPIKeyRoutes = []string{
	"GET /api-keys/",
	"POST /api-keys/",
	"DELETE /api-keys/{id}",
	"GET /api-keys/scopes",
	"GET /api-keys/presets",
	"GET /api-keys/expiring",
	"GET /api-keys/event-subscriptions",
	"POST /api-keys/event-subscriptions",
	"GET /api-keys/leaks",
	"POST /api-keys/{id}/rotate",
	"GET /api-keys/{id}/logs",
	"PATCH /api-keys/{id}/environment",
	"GET /api-keys/policy",
	"PUT /api-keys/policy",
	"GET /api-keys/approvals",
	"POST /api-keys/approvals",
	"POST /api-keys/approvals/{id}/review",
	"GET /api-keys/analytics",
	"GET /api-keys/{id}/tags",
	"PUT /api-keys/{id}/tags",
	"PUT /api-keys/{id}/budget",
	"POST /api-keys/{id}/temp-token",
	"POST /api-keys/{id}/project",
	"GET /api-keys/{id}/analytics",
	"GET /api-keys/{id}/rotation-schedule",
	"POST /api-keys/{id}/rotation-schedule",
	"GET /api-keys/{id}/unused-scopes",
	"GET /api-keys/{id}/signing-keys",
	"POST /api-keys/{id}/signing-keys",
	"GET /api-keys/{id}/health",
}

var expectedAPIKeyRelatedRoutes = []string{
	"GET /service-accounts/",
	"POST /service-accounts/",
	"PATCH /service-accounts/{id}",
	"DELETE /service-accounts/{id}",
	"GET /projects/",
	"POST /projects/",
	"GET /oauth2/clients/",
	"POST /oauth2/clients/",
	"GET /secret-managers/",
	"POST /secret-managers/",
	"GET /managed-identities/",
	"POST /managed-identities/",
	"GET /siem-integrations/",
	"POST /siem-integrations/",
	"GET /ai-agents/",
	"POST /ai-agents/",
}

var expectedGDPRRoutes = []string{
	"GET /gdpr/requests",
	"GET /gdpr/operations",
	"POST /gdpr/step-up/verify",
	"POST /gdpr/export",
	"GET /gdpr/exports/{job_id}",
	"GET /gdpr/exports/{job_id}/download",
	"POST /gdpr/exports/{job_id}/revoke",
	"POST /gdpr/delete",
	"POST /gdpr/requests/{request_id}/cancel",
}

var expectedOtherRoutes = []string{
	"GET /notify-targets/",
	"POST /notify-targets/",
	"PATCH /notify-targets/{id}",
	"DELETE /notify-targets/{id}",
	"POST /notify-targets/{id}/test",
	"GET /digest-subscription",
	"PUT /digest-subscription",
	"DELETE /digest-subscription",
	"GET /feedback/",
	"GET /feedback/stats",
	"GET /feedback/terminal-failures",
	"GET /feedback/search/quality",
	"POST /feedback/search",
	"POST /feedback/search/events",
	"GET /feedback/{id}",
	"POST /feedback/{id}/reply-draft/regenerate",
	"POST /feedback/{id}/reply-draft/edit",
	"POST /feedback/{id}/reply-draft/approve",
	"POST /feedback/{id}/reply-draft/reject",
	"POST /feedback/{id}/reply-draft/send",
	"POST /feedback/{id}/retry-enrichment",
	"GET /reply-send-hook",
	"GET /reply-send-hook/health",
	"GET /reply-send-hook/deliveries",
	"PUT /reply-send-hook",
	"POST /reply-send-hook/test",
	"POST /reply-send-hook/deliveries/{id}/redeliver",
	"DELETE /reply-send-hook",
	"GET /usage",
	"GET /llm-usage",
	"GET /classification-quality",
	"GET /classification-quality/samples",
	"GET /external-sync/providers",
	"GET /external-sync/provider-installations",
	"POST /external-sync/provider-installations",
	"DELETE /external-sync/provider-installations/{id}",
	"POST /external-sync/provider-installations/{id}:qualify",
	"GET /external-sync/provider-installations/{id}/resources",
	"POST /external-sync/provider-installations/{id}/resources:select",
	"GET /external-sync/connections",
	"POST /external-sync/connections",
	"PATCH /external-sync/connections/{id}",
	"DELETE /external-sync/connections/{id}",
	"POST /external-sync/connections/{id}:test",
	"POST /external-sync/connections/{id}:resume",
	"POST /external-sync/connections/{id}:qualify",
	"GET /external-sync/connections/{id}/schema",
	"GET /external-sync/mappings",
	"PUT /external-sync/mappings/{id}",
	"POST /external-sync/mappings/{id}:preview",
	"POST /external-sync/mappings/{id}:reset-cursor",
	"POST /external-sync/mappings/{id}:backfill",
	"POST /external-sync/runs",
	"GET /external-sync/runs",
	"GET /external-sync/runs/{id}",
	"POST /external-sync/records:timeline",
	"POST /external-sync/runs/{id}:retry",
	"POST /external-sync/failures/{id}:retry",
	"POST /external-sync/conflicts/{id}:resolve",
	"POST /external-sync/conflicts:batch-resolve",
	"GET /external-sync/events",
	"GET /external-sync/events/{id}",
	"POST /external-sync/events/{id}:replay",
	"GET /external-sync/health",
	"GET /enrich-config/",
	"PUT /enrich-config/",
	"GET /enrich-config/versions",
	"POST /enrich-config/versions/{version_id}:activate",
	"POST /enrich-config/preview",
	"POST /enrich-config/eval-suggestions:analyze",
	"POST /enrich-config/promote",
	"GET /guard-policies/",
	"POST /guard-policies/",
	"PUT /guard-policies/",
	"PATCH /guard-policies/{id}",
	"DELETE /guard-policies/{id}",
	"POST /guard-policies/effective",
	"GET /inbound/sources/",
	"POST /inbound/sources/",
	"POST /inbound/sources/test-connection",
	"POST /inbound/sources/slack/discover",
	"GET /inbound/sources/{id}",
	"DELETE /inbound/sources/{id}",
	"POST /inbound/sources/{id}/rotate-secret",
	"GET /inbound/sources/{id}/recent",
	"POST /inbound/sources/{id}/pause",
	"POST /inbound/sources/{id}/resume",
	"POST /inbound/sources/{id}/sync-now",
	"GET /tags/",
	"POST /tags/",
	"PATCH /tags/{id}",
	"DELETE /tags/{id}",
	"POST /feedback/{id}/tags",
	"DELETE /feedback/{id}/tags/{tag_id}",
	"POST /feedback/batch/tags",
	"POST /feedback/{id}/transition",
	"POST /feedback/transition/batch",
	"GET /feedback/{id}/audit",
	"GET /workflow/states",
	"POST /workflow/states",
	"PATCH /workflow/states/{id}",
	"DELETE /workflow/states/{id}",
	"GET /workflow/transitions",
	"PUT /workflow/transitions",
	"POST /workflow/seed",
}

func TestRouterInventory(t *testing.T) {
	t.Parallel()

	signer, err := session.NewSigner(strings.Repeat("k", 32))
	require.NoError(t, err)

	router := (&Router{
		signer:             signer,
		login:              &auth.Handler{},
		changePassword:     &auth.ChangePasswordHandler{},
		me:                 &me.MeHandler{},
		gdpr:               &consolegdpr.Handler{},
		apiKeys:            &apikey.APIKeysHandler{},
		notifyTargets:      &notifytarget.NotifyTargetsHandler{},
		externalSync:       &consoleexternalsync.Handler{},
		digestSubscription: &digestsubscription.Handler{},
		feedback:           &feedback.FeedbackHandler{},
		feedbackSearch:     &feedback.SearchHandler{},
		usage:              &usage.UsageHandler{},
		enrichConfig:       &enrichconfig.Handler{},
		guardPolicies:      &consoleguardpolicy.Handler{},
		inbound:            &consoleinbound.Handler{},
		tags:               &consoletag.Handler{},
		tagAssignments:     &consoletagassignment.Handler{},
		workflow:           &consoleworkflow.Handler{},
	}).Mount()

	got := make(map[string]bool)
	err = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	})
	require.NoError(t, err)

	expected := collectExpectedRoutes()
	for _, route := range expected {
		require.Truef(t, got[route], "missing route %s; got:\n%s", route, strings.Join(sortedKeys(got), "\n"))
		delete(got, route)
	}
	require.Emptyf(t, got, "unexpected routes:\n%s", strings.Join(sortedKeys(got), "\n"))
}

func collectExpectedRoutes() []string {
	var all []string
	all = append(all, expectedAuthRoutes...)
	all = append(all, expectedAPIKeyRoutes...)
	all = append(all, expectedAPIKeyRelatedRoutes...)
	all = append(all, expectedGDPRRoutes...)
	all = append(all, expectedOtherRoutes...)
	return all
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
