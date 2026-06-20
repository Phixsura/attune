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
	"GET /feedback/{id}",
	"POST /feedback/{id}/reply-draft/regenerate",
	"GET /usage",
	"GET /llm-usage",
	"GET /enrich-config/",
	"PUT /enrich-config/",
	"POST /enrich-config/preview",
	"GET /enrich-config/eval-suggestions",
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
	"GET /inbound/sources/{id}",
	"DELETE /inbound/sources/{id}",
	"POST /inbound/sources/{id}/rotate-secret",
	"POST /inbound/sources/{id}/pause",
	"POST /inbound/sources/{id}/resume",
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
		digestSubscription: &digestsubscription.Handler{},
		feedback:           &feedback.FeedbackHandler{},
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
