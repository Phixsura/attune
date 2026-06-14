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

func TestRouterInventory(t *testing.T) {
	t.Parallel()

	signer, err := session.NewSigner(strings.Repeat("k", 32))
	require.NoError(t, err)

	router := (&Router{
		signer:             signer,
		login:              &auth.Handler{},
		changePassword:     &auth.ChangePasswordHandler{},
		me:                 &me.MeHandler{},
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

	expected := []string{
		"POST /install/login",
		"GET /me",
		"POST /logout",
		"POST /me/change-password",
		"GET /api-keys/",
		"POST /api-keys/",
		"DELETE /api-keys/{id}",
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

	for _, route := range expected {
		require.Truef(t, got[route], "missing route %s; got:\n%s", route, strings.Join(sortedKeys(got), "\n"))
		delete(got, route)
	}
	require.Emptyf(t, got, "unexpected routes:\n%s", strings.Join(sortedKeys(got), "\n"))
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
