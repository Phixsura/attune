-- Migration 059: MCP audit actions
--
-- Extend audit_log action constraint with MCP tool calls and OAuth admin actions.
-- Keep the Go allow-list (internal/service/auditlog/actions.go) in lockstep.

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS chk_audit_action_value;
ALTER TABLE audit_log ADD CONSTRAINT chk_audit_action_value
    CHECK (action IN (
        -- Existing actions (from 057)
        'api_key.create',
        'api_key.revoke',
        'api_key.rotate',
        'digest_subscription.delete',
        'digest_subscription.upsert',
        'enrich_config.promote_suggested',
        'enrich_config.update',
        'enrichment_runtime.reset',
        'enrichment_runtime.rollback',
        'enrichment_runtime.update',
        'feedback.batch_delete',
        'feedback_job.cancel',
        'gdpr.delete',
        'gdpr.delete.cancelled',
        'gdpr.delete.requested',
        'gdpr.export',
        'gdpr.export.revoked',
        'guard_policy.create',
        'guard_policy.delete',
        'guard_policy.update',
        'inbound_source.create',
        'inbound_source.delete',
        'inbound_source.pause',
        'inbound_source.resume',
        'inbound_source.rotate_secret',
        'inbound_source.test_connection',
        'llm_ability.delete',
        'llm_ability.upsert',
        'llm_channel.create',
        'llm_channel.delete',
        'llm_channel.test',
        'llm_channel.update',
        'llm_route.delete',
        'llm_route.upsert',
        'member.invite',
        'member.remove',
        'member.update_role',
        'notify_target.create',
        'notify_target.delete',
        'notify_target.test',
        'notify_target.update',
        'outbox.retry',
        'tag.archive',
        'tag.create',
        'tag.update',
        'workflow_seed_defaults.run',
        'workflow_state.archive',
        'workflow_state.create',
        'workflow_state.update',
        'workflow_transition.replace',

        -- MCP tool calls - read operations
        'mcp.list_feedback',
        'mcp.get_feedback',
        'mcp.list_workflow_states',
        'mcp.get_workflow_state',
        'mcp.list_tags',

        -- MCP tool calls - write operations
        'mcp.update_workflow_state',
        'mcp.add_tag',
        'mcp.remove_tag',
        'mcp.set_urgent',

        -- MCP tool calls - ingest operations
        'mcp.submit_feedback',

        -- MCP tool calls - future expansion
        'mcp.search_feedback',
        'mcp.list_dimensions',
        'mcp.list_clusters',
        'mcp.get_cluster',
        'mcp.get_digest',
        'mcp.get_usage_stats',
        'mcp.update_status',
        'mcp.update_tags',
        'mcp.reclassify',
        'mcp.link_issue',
        'mcp.mark_duplicate',
        'mcp.batch_update_status',
        'mcp.batch_update_tags',
        'mcp.record_signal',
        'mcp.create_tag',
        'mcp.archive_tag',
        'mcp.trigger_digest',
        'mcp.get_enrichment_status',
        'mcp.retry_enrichment',

        -- MCP OAuth admin actions (new)
        'mcp_client.create',
        'mcp_client.revoke'
    ));
