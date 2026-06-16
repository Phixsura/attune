// ptrext:file-allow inventory fixtures use nil-safe handler pointers only for route enumeration.
package console

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/handlers/console/apikey"
	consoleauditlog "github.com/Phixsura/attune/internal/handlers/console/auditlog"
	"github.com/Phixsura/attune/internal/handlers/console/auth"
	"github.com/Phixsura/attune/internal/handlers/console/clusters"
	"github.com/Phixsura/attune/internal/handlers/console/digestsubscription"
	"github.com/Phixsura/attune/internal/handlers/console/enrichconfig"
	"github.com/Phixsura/attune/internal/handlers/console/feedback"
	"github.com/Phixsura/attune/internal/handlers/console/feedbackjob"
	consoleguardpolicy "github.com/Phixsura/attune/internal/handlers/console/guardpolicy"
	consoleinbound "github.com/Phixsura/attune/internal/handlers/console/inbound"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	consolellmconfig "github.com/Phixsura/attune/internal/handlers/console/llmconfig"
	"github.com/Phixsura/attune/internal/handlers/console/me"
	"github.com/Phixsura/attune/internal/handlers/console/member"
	"github.com/Phixsura/attune/internal/handlers/console/notifytarget"
	consoletag "github.com/Phixsura/attune/internal/handlers/console/tag"
	consoletagassignment "github.com/Phixsura/attune/internal/handlers/console/tagassignment"
	"github.com/Phixsura/attune/internal/handlers/console/usage"
	consoleworkflow "github.com/Phixsura/attune/internal/handlers/console/workflow"
)

func TestMutatingRoutesHaveAuditCoverageDecision(t *testing.T) {
	t.Parallel()

	signer, err := session.NewSigner(strings.Repeat("k", 32))
	require.NoError(t, err)

	router := (&Router{
		signer:             signer,
		login:              &auth.Handler{},
		changePassword:     &auth.ChangePasswordHandler{},
		me:                 &me.MeHandler{},
		auditLog:           &consoleauditlog.Handler{},
		apiKeys:            &apikey.APIKeysHandler{},
		notifyTargets:      &notifytarget.NotifyTargetsHandler{},
		digestSubscription: &digestsubscription.Handler{},
		feedback:           &feedback.FeedbackHandler{},
		feedbackBatch:      &feedback.BatchHandler{},
		feedbackSearch:     &feedback.SearchHandler{},
		feedbackJob:        &feedbackjob.Handler{},
		usage:              &usage.UsageHandler{},
		enrichConfig:       &enrichconfig.Handler{},
		guardPolicies:      &consoleguardpolicy.Handler{},
		inbound:            &consoleinbound.Handler{},
		llmConfig:          &consolellmconfig.Handler{},
		clusters:           &clusters.ClustersHandler{},
		tags:               &consoletag.Handler{},
		tagAssignments:     &consoletagassignment.Handler{},
		workflow:           &consoleworkflow.Handler{},
		members:            &member.Handler{},
	}).Mount()

	got := map[string]bool{}
	err = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		switch method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			got[method+" "+route] = true
		}
		return nil
	})
	require.NoError(t, err)

	coverage := map[string]string{
		"POST /install/login":                              "exempt: login flow creates a session but is not a tenant-scoped unified audit event",
		"POST /logout":                                     "exempt: logout tears down a session only",
		"POST /me/change-password":                         "exempt: self-service auth flow outside the tenant-scoped unified audit stream",
		"POST /api-keys/":                                  "audited: api_key.create",
		"DELETE /api-keys/{id}":                            "audited: api_key.revoke",
		"POST /notify-targets/":                            "audited: notify_target.create",
		"PATCH /notify-targets/{id}":                       "audited: notify_target.update",
		"DELETE /notify-targets/{id}":                      "audited: notify_target.delete",
		"POST /notify-targets/{id}/test":                   "audited: notify_target.test",
		"PUT /digest-subscription":                         "audited: digest_subscription.upsert",
		"DELETE /digest-subscription":                      "audited: digest_subscription.delete",
		"POST /feedback/batch/tags":                        "exempt: operational feedback tagging flow, reviewed outside unified control-plane audit",
		"POST /feedback/transition/batch":                  "exempt: per-feedback workflow audit path, not unified control-plane audit",
		"POST /feedback/batch":                             "audited: feedback.batch_delete for delete payloads; route is payload-multiplexed and non-delete variants are operational",
		"POST /feedback/search":                            "exempt: read-only semantic search",
		"POST /feedback/{id}/reply-draft/regenerate":       "exempt: content regeneration, no control-plane state change",
		"POST /feedback/{id}/tags":                         "exempt: per-feedback tagging flow, not unified control-plane audit",
		"DELETE /feedback/{id}/tags/{tag_id}":              "exempt: per-feedback tagging flow, not unified control-plane audit",
		"POST /feedback/{id}/transition":                   "exempt: per-feedback workflow audit path, not unified control-plane audit",
		"PUT /enrich-config/":                              "audited: enrich_config.update",
		"POST /enrich-config/preview":                      "exempt: preview-only, does not persist config",
		"POST /guard-policies/":                            "audited: guard_policy.create",
		"PUT /guard-policies/":                             "audited: guard_policy.update",
		"PATCH /guard-policies/{id}":                       "audited: guard_policy.update",
		"DELETE /guard-policies/{id}":                      "audited: guard_policy.delete",
		"POST /guard-policies/effective":                   "exempt: effective-policy preview, no persisted mutation",
		"POST /inbound/sources/":                           "audited: inbound_source.create",
		"POST /inbound/sources/test-connection":            "audited: inbound_source.test_connection",
		"DELETE /inbound/sources/{id}":                     "audited: inbound_source.delete",
		"POST /inbound/sources/{id}/rotate-secret":         "audited: inbound_source.rotate_secret",
		"POST /inbound/sources/{id}/pause":                 "audited: inbound_source.pause",
		"POST /inbound/sources/{id}/resume":                "audited: inbound_source.resume",
		"POST /jobs/{job_id}/cancel":                       "audited: feedback_job.cancel",
		"POST /llm/channels":                               "audited: llm_channel.create",
		"PATCH /llm/channels/{id}":                         "audited: llm_channel.update",
		"DELETE /llm/channels/{id}":                        "audited: llm_channel.delete",
		"POST /llm/channels/{id}/test":                     "audited: llm_channel.test",
		"PUT /llm/channels/{channel_id}/abilities":         "audited: llm_ability.upsert",
		"POST /llm/channels/{channel_id}/abilities/delete": "audited: llm_ability.delete",
		"PUT /llm/routes":                                  "audited: llm_route.upsert",
		"POST /llm/routes/delete":                          "audited: llm_route.delete",
		"POST /tags/":                                      "audited: tag.create",
		"PATCH /tags/{id}":                                 "audited: tag.update",
		"DELETE /tags/{id}":                                "audited: tag.archive",
		"POST /workflow/states":                            "audited: workflow_state.create",
		"PATCH /workflow/states/{id}":                      "audited: workflow_state.update",
		"DELETE /workflow/states/{id}":                     "audited: workflow_state.archive",
		"PUT /workflow/transitions":                        "audited: workflow_transition.replace",
		"POST /workflow/seed":                              "audited: workflow_seed_defaults.run",
		"POST /members/":                                   "audited: member.invite",
		"PATCH /members/{id}":                              "audited: member.update_role",
		"DELETE /members/{id}":                             "audited: member.remove",
	}

	for route := range got {
		require.Containsf(t, coverage, route, "missing audit coverage decision for mutating route %s", route)
	}
	for route := range coverage {
		require.Truef(t, got[route], "stale audit coverage decision for missing route %s; got:\n%s", route, strings.Join(sortedKeys(got), "\n"))
	}
}
