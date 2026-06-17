// SPDX-License-Identifier: Apache-2.0

package auditlog

import "strings"

var validActions = map[string]struct{}{
	"api_key.create":                 {},
	"api_key.revoke":                 {},
	"digest_subscription.delete":     {},
	"digest_subscription.upsert":     {},
	"enrich_config.update":           {},
	"feedback.batch_delete":          {},
	"feedback_job.cancel":            {},
	"gdpr.delete":                    {},
	"gdpr.delete.cancelled":          {},
	"gdpr.delete.requested":          {},
	"gdpr.export":                    {},
	"gdpr.export.revoked":            {},
	"guard_policy.create":            {},
	"guard_policy.delete":            {},
	"guard_policy.update":            {},
	"inbound_source.create":          {},
	"inbound_source.delete":          {},
	"inbound_source.pause":           {},
	"inbound_source.resume":          {},
	"inbound_source.rotate_secret":   {},
	"inbound_source.test_connection": {},
	"llm_ability.delete":             {},
	"llm_ability.upsert":             {},
	"llm_channel.create":             {},
	"llm_channel.delete":             {},
	"llm_channel.test":               {},
	"llm_channel.update":             {},
	"llm_route.delete":               {},
	"llm_route.upsert":               {},
	"member.invite":                  {},
	"member.remove":                  {},
	"member.update_role":             {},
	"notify_target.create":           {},
	"notify_target.delete":           {},
	"notify_target.test":             {},
	"notify_target.update":           {},
	"tag.archive":                    {},
	"tag.create":                     {},
	"tag.update":                     {},
	"workflow_seed_defaults.run":     {},
	"workflow_state.archive":         {},
	"workflow_state.create":          {},
	"workflow_state.update":          {},
	"workflow_transition.replace":    {},
}

func isKnownAction(action string) bool {
	_, ok := validActions[strings.TrimSpace(action)]
	return ok
}
