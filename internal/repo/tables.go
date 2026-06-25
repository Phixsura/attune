// SPDX-License-Identifier: Apache-2.0

// Package repo provides database repository types.
// This file defines table name constants for consistency and refactoring.
package repo

// Table names as constants for consistency across the codebase.
// Use these instead of hardcoded strings to make schema refactoring easier.
const (
	TableUserFeedback               = "user_feedback"
	TableNotifyOutbox               = "notify_outbox"
	TableNotifyTargets              = "notify_targets"
	TableTenants                    = "tenants"
	TableTenantFeedbackTags         = "tenant_feedback_tags"
	TableFeedbackTagAssignments     = "feedback_tag_assignments"
	TableAPIKeys                    = "api_keys"
	TableAdmins                     = "admins"
	TableGuardPolicies              = "guard_policies"
	TableWorkflowStates             = "workflow_states"
	TableWorkflowTransitions        = "workflow_transitions"
	TableDigestSubscriptions        = "digest_subscriptions"
	TableDigestRuns                 = "digest_runs"
	TableReplyDraftTasks            = "reply_draft_task"
	TableEmbeddingTasks             = "embedding_task"
	TableBatchJobs                  = "batch_jobs"
	TableGDPRRequests               = "gdpr_requests"
	TableGDPRExportJobs             = "gdpr_export_jobs"
	TableInboundSources             = "inbound_sources"
	TableLLMChannels                = "llm_channels"
	TableLLMChannelAbilities        = "llm_channel_abilities"
	TableLLMRoutes                  = "llm_routes"
	TableLLMUsageAudit              = "llm_usage_audit"
	TableAuditLog                   = "audit_log"
	TableFeedbackAudit              = "feedback_audit"
	TableMCPClients                 = "mcp_clients"
	TableMCPSessions                = "mcp_sessions"
	TableMCPRefreshGrants           = "mcp_refresh_grants"
	TableMCPToolOverrides           = "mcp_tool_overrides"
	TableSchemaMigrations           = "schema_migrations_feedback"
	TableIdempotencyKeys            = "idempotency_keys"
	TableTenantEnrichPrompts        = "tenant_enrich_prompts"
	TableTenantEnrichPromptVersions = "tenant_enrich_prompt_versions"
)
