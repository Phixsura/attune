// SPDX-License-Identifier: Apache-2.0

package auditlog

import "strings"

var validActions = map[string]struct{}{
	"api_key.create":                  {},
	"api_key.revoke":                  {},
	"api_key.rotate":                  {},
	"digest_subscription.delete":      {},
	"digest_subscription.upsert":      {},
	"enrich_config.activate_version":  {},
	"enrich_config.promote_suggested": {},
	"enrich_config.update":            {},
	"enrichment_runtime.reset":        {},
	"enrichment_runtime.rollback":     {},
	"enrichment_runtime.update":       {},
	"feedback.batch_delete":           {},
	"feedback_job.cancel":             {},
	"gdpr.delete":                     {},
	"gdpr.delete.cancelled":           {},
	"gdpr.delete.requested":           {},
	"gdpr.export":                     {},
	"gdpr.export.revoked":             {},
	"guard_policy.create":             {},
	"guard_policy.delete":             {},
	"guard_policy.update":             {},
	"inbound_source.create":           {},
	"inbound_source.delete":           {},
	"inbound_source.pause":            {},
	"inbound_source.resume":           {},
	"inbound_source.rotate_secret":    {},
	"inbound_source.test_connection":  {},
	"llm_ability.delete":              {},
	"llm_ability.upsert":              {},
	"llm_channel.create":              {},
	"llm_channel.delete":              {},
	"llm_channel.test":                {},
	"llm_channel.update":              {},
	"llm_route.delete":                {},
	"llm_route.upsert":                {},
	"member.invite":                   {},
	"member.remove":                   {},
	"member.update_role":              {},
	"notify_target.create":            {},
	"notify_target.delete":            {},
	"notify_target.test":              {},
	"notify_target.update":            {},
	"outbox.retry":                    {},
	"tag.archive":                     {},
	"tag.create":                      {},
	"tag.update":                      {},
	"workflow_seed_defaults.run":      {},
	"workflow_state.archive":          {},
	"workflow_state.create":           {},
	"workflow_state.update":           {},
	"workflow_transition.replace":     {},

	// MCP tool calls - read operations
	"mcp.list_feedback":        {},
	"mcp.get_feedback":         {},
	"mcp.list_workflow_states": {},
	"mcp.get_workflow_state":   {},
	"mcp.list_tags":            {},

	// MCP tool calls - write operations
	"mcp.update_workflow_state": {},
	"mcp.add_tag":               {},
	"mcp.remove_tag":            {},
	"mcp.set_urgent":            {},

	// MCP tool calls - ingest operations
	"mcp.submit_feedback": {},

	// MCP tool calls - future expansion (keep for migration compat)
	"mcp.archive_tag":           {},
	"mcp.batch_update_status":   {},
	"mcp.batch_update_tags":     {},
	"mcp.create_tag":            {},
	"mcp.get_cluster":           {},
	"mcp.get_digest":            {},
	"mcp.get_enrichment_status": {},
	"mcp.get_usage_stats":       {},
	"mcp.link_issue":            {},
	"mcp.list_clusters":         {},
	"mcp.list_dimensions":       {},
	"mcp.mark_duplicate":        {},
	"mcp.reclassify":            {},
	"mcp.record_signal":         {},
	"mcp.retry_enrichment":      {},
	"mcp.search_feedback":       {},
	"mcp.trigger_digest":        {},
	"mcp.update_status":         {},
	"mcp.update_tags":           {},

	// MCP OAuth admin actions
	"mcp_client.create": {},
	"mcp_client.revoke": {},
}

func isKnownAction(action string) bool {
	_, ok := validActions[strings.TrimSpace(action)]
	return ok
}

// IsKnownAction reports whether action is in the audit allow-list. Exported
// so the console router's audit-inventory test can assert every route's
// declared audit action is actually accepted by the auditlog service —
// a fake recorder in handler tests would otherwise mask a missing entry
// (real bug found in #83 e2e: promote's action was unregistered).
func IsKnownAction(action string) bool {
	return isKnownAction(action)
}
