-- SPDX-License-Identifier: Apache-2.0
--
-- Customer Request decision intelligence: account profiles, revenue-aware
-- scoring inputs, and delivery-link sync state.

CREATE TABLE IF NOT EXISTS customer_request_accounts (
    tenant_id          TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    account_key        TEXT        NOT NULL,
    account_display    TEXT        NOT NULL DEFAULT '',
    revenue_cents      BIGINT      NOT NULL DEFAULT 0,
    revenue_currency   TEXT        NOT NULL DEFAULT 'USD',
    tier               TEXT        NOT NULL DEFAULT '',
    size_segment       TEXT        NOT NULL DEFAULT '',
    lifecycle_status   TEXT        NOT NULL DEFAULT '',
    crm_provider       TEXT        NOT NULL DEFAULT '',
    crm_external_id    TEXT        NOT NULL DEFAULT '',
    source             TEXT        NOT NULL DEFAULT 'manual',
    created_by         TEXT        NOT NULL,
    updated_by         TEXT        NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (tenant_id, account_key),
    CONSTRAINT chk_customer_request_accounts_key_length CHECK (length(account_key) BETWEEN 1 AND 512),
    CONSTRAINT chk_customer_request_accounts_display_length CHECK (length(account_display) <= 500),
    CONSTRAINT chk_customer_request_accounts_revenue_nonnegative CHECK (revenue_cents >= 0),
    CONSTRAINT chk_customer_request_accounts_currency CHECK (revenue_currency ~ '^[A-Z]{3}$'),
    CONSTRAINT chk_customer_request_accounts_tier_length CHECK (length(tier) <= 120),
    CONSTRAINT chk_customer_request_accounts_size_length CHECK (length(size_segment) <= 120),
    CONSTRAINT chk_customer_request_accounts_lifecycle_length CHECK (length(lifecycle_status) <= 120),
    CONSTRAINT chk_customer_request_accounts_crm_provider_length CHECK (length(crm_provider) <= 120),
    CONSTRAINT chk_customer_request_accounts_crm_external_length CHECK (length(crm_external_id) <= 512),
    CONSTRAINT chk_customer_request_accounts_source CHECK (source IN ('manual', 'feedback', 'crm', 'integration', 'api'))
);

CREATE INDEX IF NOT EXISTS idx_customer_request_accounts_tenant_revenue
    ON customer_request_accounts (tenant_id, revenue_currency, revenue_cents DESC, account_key);
CREATE INDEX IF NOT EXISTS idx_customer_request_accounts_tenant_tier
    ON customer_request_accounts (tenant_id, tier, revenue_cents DESC)
    WHERE tier <> '';
CREATE INDEX IF NOT EXISTS idx_customer_request_accounts_tenant_crm
    ON customer_request_accounts (tenant_id, crm_provider, crm_external_id)
    WHERE crm_provider <> '' AND crm_external_id <> '';

CREATE OR REPLACE FUNCTION update_customer_request_accounts_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_customer_request_accounts_updated_at ON customer_request_accounts;
CREATE TRIGGER trg_customer_request_accounts_updated_at
    BEFORE UPDATE ON customer_request_accounts
    FOR EACH ROW
    EXECUTE FUNCTION update_customer_request_accounts_updated_at();

ALTER TABLE customer_request_issue_links
    ADD COLUMN IF NOT EXISTS sync_state TEXT NOT NULL DEFAULT 'manual',
    ADD COLUMN IF NOT EXISTS external_status_category TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS external_assignee TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS external_updated_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS sync_error TEXT NOT NULL DEFAULT '';

ALTER TABLE customer_request_issue_links
    DROP CONSTRAINT IF EXISTS chk_customer_request_issue_sync_state,
    ADD CONSTRAINT chk_customer_request_issue_sync_state
        CHECK (sync_state IN ('manual', 'pending', 'synced', 'stale', 'failed')),
    DROP CONSTRAINT IF EXISTS chk_customer_request_issue_status_category_length,
    ADD CONSTRAINT chk_customer_request_issue_status_category_length CHECK (length(external_status_category) <= 120),
    DROP CONSTRAINT IF EXISTS chk_customer_request_issue_assignee_length,
    ADD CONSTRAINT chk_customer_request_issue_assignee_length CHECK (length(external_assignee) <= 500),
    DROP CONSTRAINT IF EXISTS chk_customer_request_issue_sync_error_length,
    ADD CONSTRAINT chk_customer_request_issue_sync_error_length CHECK (length(sync_error) <= 2000);

CREATE INDEX IF NOT EXISTS idx_customer_request_issue_links_sync_state
    ON customer_request_issue_links (tenant_id, sync_state, last_synced_at DESC)
    WHERE sync_state <> 'manual';

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS chk_audit_action_value;
ALTER TABLE audit_log ADD CONSTRAINT chk_audit_action_value
    CHECK (action IN (
        'api_key.create',
        'api_key.revoke',
        'api_key.rotate',
        'audit_evidence.create',
        'audit_evidence.download',
        'audit_evidence.expire',
        'auth.mode_change',
        'breakglass.issue',
        'breakglass.use',
        'breakglass.revoke',
        'breakglass.expire',
        'breakglass.unlock_ip',
        'breakglass.approve',
        'breakglass.recovery_codes.generate',
        'breakglass.recovery_codes.use',
        'breakglass.recovery_codes.revoke',
        'service_account.create',
        'service_account.delete',
        'service_account.update',
        'customer_request.create',
        'customer_request.update',
        'customer_request.promote_feedback',
        'customer_request.link_feedback',
        'customer_request.unlink_feedback',
        'customer_request.link_customer',
        'customer_request.unlink_customer',
        'customer_request.add_vote',
        'customer_request.remove_vote',
        'customer_request.merge',
        'customer_request.link_issue',
        'customer_request.unlink_issue',
        'customer_request.record_issue_sync',
        'digest_subscription.delete',
        'digest_subscription.upsert',
        'enrich_config.activate_version',
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
        'reply_draft.approve',
        'reply_draft.edit',
        'reply_draft.generate',
        'reply_draft.generate_blocked',
        'reply_draft.reject',
        'reply_draft.send.failure',
        'reply_draft.send.request',
        'reply_draft.send.success',
        'reply_draft.stale',
        'reply_send_hook.disable',
        'reply_send_hook.redeliver',
        'reply_send_hook.test',
        'reply_send_hook.upsert',
        'retry_enrichment',
        'tag.archive',
        'tag.create',
        'tag.update',
        'workflow_seed_defaults.run',
        'workflow_state.archive',
        'workflow_state.create',
        'workflow_state.update',
        'workflow_transition.replace',
        'mcp.list_feedback',
        'mcp.get_feedback',
        'mcp.list_workflow_states',
        'mcp.get_workflow_state',
        'mcp.list_tags',
        'mcp.update_workflow_state',
        'mcp.add_tag',
        'mcp.remove_tag',
        'mcp.set_urgent',
        'mcp.submit_feedback',
        'mcp.archive_tag',
        'mcp.batch_update_status',
        'mcp.batch_update_tags',
        'mcp.create_tag',
        'mcp.get_cluster',
        'mcp.get_digest',
        'mcp.get_enrichment_status',
        'mcp.get_usage_stats',
        'mcp.link_issue',
        'mcp.list_clusters',
        'mcp.list_dimensions',
        'mcp.mark_duplicate',
        'mcp.record_signal',
        'mcp.reclassify',
        'mcp.retry_enrichment',
        'mcp.search_feedback',
        'mcp.trigger_digest',
        'mcp.update_status',
        'mcp.update_tags',
        'mcp_client.create',
        'mcp_client.revoke',
        'mcp_client.update',
        'mcp_client.tool_policy_update',
        'mcp_refresh_grant.revoke',
        'mcp_session.revoke',
        'mcp_tool.authorize_denied',
        'mcp_tool.rate_limited'
    ));
