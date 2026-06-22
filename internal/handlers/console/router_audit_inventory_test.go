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
	consoleenrichmentruntime "github.com/Phixsura/attune/internal/handlers/console/enrichmentruntime"
	"github.com/Phixsura/attune/internal/handlers/console/feedback"
	"github.com/Phixsura/attune/internal/handlers/console/feedbackjob"
	consolegdpr "github.com/Phixsura/attune/internal/handlers/console/gdpr"
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
	"github.com/Phixsura/attune/internal/service/auditlog"
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
		gdpr:               &consolegdpr.Handler{},
		apiKeys:            &apikey.APIKeysHandler{},
		notifyTargets:      &notifytarget.NotifyTargetsHandler{},
		digestSubscription: &digestsubscription.Handler{},
		feedback:           &feedback.FeedbackHandler{},
		feedbackBatch:      &feedback.BatchHandler{},
		feedbackSearch:     &feedback.SearchHandler{},
		feedbackJob:        &feedbackjob.Handler{},
		usage:              &usage.UsageHandler{},
		enrichConfig:       &enrichconfig.Handler{},
		enrichmentRuntime:  &consoleenrichmentruntime.Handler{},
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

	coverage := expectedMutatingRouteCoverage()

	for route := range got {
		require.Containsf(t, coverage, route, "missing audit coverage decision for mutating route %s", route)
	}
	for route := range coverage {
		require.Truef(t, got[route], "stale audit coverage decision for missing route %s; got:\n%s", route, strings.Join(sortedKeys(got), "\n"))
	}
}

// auditEmittedActions are audit actions this test has verified are emitted by
// real handler code (not just inventory labels) and must therefore exist in
// the auditlog allow-list. Handler unit tests use a fake recorder that accepts
// any action, so a route can claim to be audited while the real
// auditlog.Service silently rejects its action at runtime. The #83 e2e found
// enrich_config.promote_suggested unregistered; the same scan surfaced
// api_key.rotate (emitted by apikey/api_keys_advanced.go) with the same gap.
//
// NOTE: many other "audited: <action>" inventory decisions are label-only —
// the route is mounted but no handler code emits that action yet. Those are
// tracked separately; this test guards only the actions proven to be emitted.
var auditEmittedActions = []string{
	"enrich_config.update",
	"enrich_config.activate_version",
	"enrich_config.promote_suggested",
	"api_key.rotate",
	"api_key.create",
	"api_key.revoke",
	"retry_enrichment",
}

// TestAuditedRouteActionsAreRegistered asserts every emitted audit action is
// in the auditlog allow-list, so a runtime audit write is never silently
// dropped for an unregistered action.
func TestAuditedRouteActionsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, action := range auditEmittedActions {
		require.Truef(t, auditlog.IsKnownAction(action),
			"audit action %q is emitted by handler code but not in auditlog.validActions — "+
				"register it in internal/service/auditlog/actions.go or the runtime audit write is silently dropped",
			action)
	}
}

func expectedMutatingRouteCoverage() map[string]string {
	return map[string]string{
		"POST /install/login":                                "exempt: login flow creates a session but is not a tenant-scoped unified audit event",
		"POST /logout":                                       "exempt: logout tears down a session only",
		"POST /me/change-password":                           "exempt: self-service auth flow outside the tenant-scoped unified audit stream",
		"POST /api-keys/":                                    "audited: api_key.create",
		"DELETE /api-keys/{id}":                              "audited: api_key.revoke",
		"POST /api-keys/event-subscriptions":                 "audited: api_key.event_subscription.create",
		"POST /api-keys/{id}/rotate":                         "audited: api_key.rotate",
		"PATCH /api-keys/{id}/environment":                   "audited: api_key.environment.update",
		"PUT /api-keys/policy":                               "audited: api_key.policy.update",
		"POST /api-keys/approvals":                           "audited: api_key.approval.create",
		"POST /api-keys/approvals/{id}/review":               "audited: api_key.approval.review",
		"PUT /api-keys/{id}/tags":                            "audited: api_key.tags.update",
		"PUT /api-keys/{id}/budget":                          "audited: api_key.budget.update",
		"POST /api-keys/{id}/temp-token":                     "audited: api_key.temp_token.create",
		"POST /api-keys/{id}/project":                        "audited: api_key.project.bind",
		"POST /service-accounts/":                            "audited: service_account.create",
		"POST /projects/":                                    "audited: project.create",
		"POST /oauth2/clients/":                              "audited: oauth2_client.create",
		"POST /secret-managers/":                             "audited: secret_manager.create",
		"POST /managed-identities/":                          "audited: managed_identity.create",
		"POST /siem-integrations/":                           "audited: siem_integration.create",
		"POST /ai-agents/":                                   "audited: ai_agent.create",
		"POST /api-keys/{id}/rotation-schedule":              "audited: api_key.rotation_schedule.create",
		"POST /api-keys/{id}/signing-keys":                   "audited: api_key.signing_key.create",
		"POST /gdpr/step-up/verify":                          "exempt: recent-auth refreshes the session but does not mutate tenant data directly",
		"POST /gdpr/export":                                  "audited: gdpr.export",
		"POST /gdpr/exports/{job_id}/revoke":                 "audited: gdpr.export.revoked",
		"POST /gdpr/delete":                                  "audited: gdpr.delete",
		"POST /gdpr/requests/{request_id}/cancel":            "audited: gdpr.delete.cancelled",
		"POST /notify-targets/":                              "audited: notify_target.create",
		"PATCH /notify-targets/{id}":                         "audited: notify_target.update",
		"DELETE /notify-targets/{id}":                        "audited: notify_target.delete",
		"POST /notify-targets/{id}/test":                     "audited: notify_target.test",
		"PUT /digest-subscription":                           "audited: digest_subscription.upsert",
		"DELETE /digest-subscription":                        "audited: digest_subscription.delete",
		"POST /feedback/batch/tags":                          "exempt: operational feedback tagging flow, reviewed outside unified control-plane audit",
		"POST /feedback/transition/batch":                    "exempt: per-feedback workflow audit path, not unified control-plane audit",
		"POST /feedback/batch":                               "audited: feedback.batch_delete for delete payloads; route is payload-multiplexed and non-delete variants are operational",
		"POST /feedback/search":                              "exempt: read-only semantic search",
		"POST /feedback/{id}/reply-draft/regenerate":         "exempt: content regeneration, no control-plane state change",
		"POST /feedback/{id}/tags":                           "exempt: per-feedback tagging flow, not unified control-plane audit",
		"DELETE /feedback/{id}/tags/{tag_id}":                "exempt: per-feedback tagging flow, not unified control-plane audit",
		"POST /feedback/{id}/transition":                     "exempt: per-feedback workflow audit path, not unified control-plane audit",
		"POST /feedback/{id}/retry-enrichment":               "audited: retry_enrichment",
		"PUT /enrich-config/":                                "audited: enrich_config.update",
		"POST /enrich-config/versions/{version_id}:activate": "audited: enrich_config.activate_version",
		"POST /enrich-config/eval-suggestions:analyze":       "exempt: explicit LLM eval analysis, no persisted config mutation",
		"POST /enrich-config/preview":                        "exempt: preview-only, does not persist config",
		"POST /enrich-config/promote":                        "audited: enrich_config.promote_suggested",
		"PUT /enrichment-runtime/":                           "audited: enrichment_runtime.update",
		"POST /enrichment-runtime/reset":                     "audited: enrichment_runtime.reset",
		"POST /enrichment-runtime/rollback":                  "audited: enrichment_runtime.rollback",
		"POST /enrichment-runtime:reset":                     "audited: enrichment_runtime.reset (legacy compatibility route)",
		"POST /enrichment-runtime:rollback":                  "audited: enrichment_runtime.rollback (legacy compatibility route)",
		"POST /guard-policies/":                              "audited: guard_policy.create",
		"PUT /guard-policies/":                               "audited: guard_policy.update",
		"PATCH /guard-policies/{id}":                         "audited: guard_policy.update",
		"DELETE /guard-policies/{id}":                        "audited: guard_policy.delete",
		"POST /guard-policies/effective":                     "exempt: effective-policy preview, no persisted mutation",
		"POST /inbound/sources/":                             "audited: inbound_source.create",
		"POST /inbound/sources/test-connection":              "audited: inbound_source.test_connection",
		"DELETE /inbound/sources/{id}":                       "audited: inbound_source.delete",
		"POST /inbound/sources/{id}/rotate-secret":           "audited: inbound_source.rotate_secret",
		"POST /inbound/sources/{id}/pause":                   "audited: inbound_source.pause",
		"POST /inbound/sources/{id}/resume":                  "audited: inbound_source.resume",
		"POST /jobs/{job_id}/cancel":                         "audited: feedback_job.cancel",
		"POST /llm/channels":                                 "audited: llm_channel.create",
		"PATCH /llm/channels/{id}":                           "audited: llm_channel.update",
		"DELETE /llm/channels/{id}":                          "audited: llm_channel.delete",
		"POST /llm/channels/{id}/test":                       "audited: llm_channel.test",
		"PUT /llm/channels/{channel_id}/abilities":           "audited: llm_ability.upsert",
		"POST /llm/channels/{channel_id}/abilities/delete":   "audited: llm_ability.delete",
		"PUT /llm/routes":                                    "audited: llm_route.upsert",
		"POST /llm/routes/delete":                            "audited: llm_route.delete",
		"POST /tags/":                                        "audited: tag.create",
		"PATCH /tags/{id}":                                   "audited: tag.update",
		"DELETE /tags/{id}":                                  "audited: tag.archive",
		"POST /workflow/states":                              "audited: workflow_state.create",
		"PATCH /workflow/states/{id}":                        "audited: workflow_state.update",
		"DELETE /workflow/states/{id}":                       "audited: workflow_state.archive",
		"PUT /workflow/transitions":                          "audited: workflow_transition.replace",
		"POST /workflow/seed":                                "audited: workflow_seed_defaults.run",
		"POST /members/":                                     "audited: member.invite",
		"PATCH /members/{id}":                                "audited: member.update_role",
		"DELETE /members/{id}":                               "audited: member.remove",
	}
}
