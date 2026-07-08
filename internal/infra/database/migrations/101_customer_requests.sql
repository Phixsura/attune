-- SPDX-License-Identifier: Apache-2.0
--
-- Customer Requests (#212): tenant-scoped product request objects linked to
-- raw feedback evidence and external delivery references.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Composite foreign keys from request evidence need the tenant dimension on
-- existing rows. The primary key remains the stable bigint id.
CREATE UNIQUE INDEX IF NOT EXISTS uq_user_feedback_tenant_id
    ON user_feedback (tenant_id, id);

CREATE TABLE IF NOT EXISTS customer_request_counters (
    tenant_id   TEXT   PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    next_number BIGINT NOT NULL DEFAULT 1
        CONSTRAINT chk_customer_request_counters_next_positive CHECK (next_number > 0)
);

CREATE TABLE IF NOT EXISTS customer_requests (
    id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id              TEXT        NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    display_number         BIGINT      NOT NULL,
    display_id             TEXT        NOT NULL,
    title                  TEXT        NOT NULL,
    description            TEXT        NOT NULL DEFAULT '',
    status                 TEXT        NOT NULL DEFAULT 'open',
    priority               TEXT        NOT NULL DEFAULT 'none',
    owner_member_id        UUID        REFERENCES tenant_members(id) ON DELETE SET NULL,
    created_by             TEXT        NOT NULL,
    updated_by             TEXT        NOT NULL,
    merged_into_request_id UUID,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at            TIMESTAMPTZ,

    CONSTRAINT uq_customer_requests_tenant_id_id UNIQUE (tenant_id, id),
    CONSTRAINT uq_customer_requests_tenant_display_id UNIQUE (tenant_id, display_id),
    CONSTRAINT uq_customer_requests_tenant_display_number UNIQUE (tenant_id, display_number),
    CONSTRAINT fk_customer_requests_merged_same_tenant
        FOREIGN KEY (tenant_id, merged_into_request_id)
        REFERENCES customer_requests(tenant_id, id),
    CONSTRAINT chk_customer_requests_title_length CHECK (length(title) BETWEEN 1 AND 200),
    CONSTRAINT chk_customer_requests_description_length CHECK (length(description) <= 10000),
    CONSTRAINT chk_customer_requests_status CHECK (status IN ('open', 'planned', 'in_progress', 'shipped', 'cancelled')),
    CONSTRAINT chk_customer_requests_priority CHECK (priority IN ('none', 'low', 'medium', 'high', 'urgent')),
    CONSTRAINT chk_customer_requests_not_self_merged CHECK (merged_into_request_id IS NULL OR merged_into_request_id <> id)
);

CREATE INDEX IF NOT EXISTS idx_customer_requests_tenant_updated
    ON customer_requests (tenant_id, updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_customer_requests_tenant_status
    ON customer_requests (tenant_id, status, updated_at DESC)
    WHERE archived_at IS NULL AND merged_into_request_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_customer_requests_tenant_priority
    ON customer_requests (tenant_id, priority, updated_at DESC)
    WHERE archived_at IS NULL AND merged_into_request_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_customer_requests_owner
    ON customer_requests (tenant_id, owner_member_id, updated_at DESC)
    WHERE owner_member_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_customer_requests_merged_into
    ON customer_requests (tenant_id, merged_into_request_id)
    WHERE merged_into_request_id IS NOT NULL;

CREATE OR REPLACE FUNCTION update_customer_requests_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_customer_requests_updated_at ON customer_requests;
CREATE TRIGGER trg_customer_requests_updated_at
    BEFORE UPDATE ON customer_requests
    FOR EACH ROW
    EXECUTE FUNCTION update_customer_requests_updated_at();

CREATE TABLE IF NOT EXISTS customer_request_feedback_links (
    tenant_id   TEXT        NOT NULL,
    request_id  UUID        NOT NULL,
    feedback_id BIGINT      NOT NULL,
    importance  TEXT        NOT NULL DEFAULT 'normal',
    note        TEXT        NOT NULL DEFAULT '',
    created_by  TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (request_id, feedback_id),
    CONSTRAINT uq_customer_request_feedback_links_tenant UNIQUE (tenant_id, request_id, feedback_id),
    CONSTRAINT fk_customer_request_feedback_links_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT fk_customer_request_feedback_links_feedback
        FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES user_feedback(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_customer_request_feedback_importance CHECK (importance IN ('normal', 'important', 'critical')),
    CONSTRAINT chk_customer_request_feedback_note_length CHECK (length(note) <= 5000)
);

CREATE INDEX IF NOT EXISTS idx_customer_request_feedback_links_tenant_request
    ON customer_request_feedback_links (tenant_id, request_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_customer_request_feedback_links_tenant_feedback
    ON customer_request_feedback_links (tenant_id, feedback_id, created_at DESC);

CREATE TABLE IF NOT EXISTS customer_request_customer_links (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT        NOT NULL,
    request_id      UUID        NOT NULL,
    subject_key     TEXT        NOT NULL DEFAULT '',
    subject_hash    TEXT        NOT NULL DEFAULT '',
    subject_display TEXT        NOT NULL DEFAULT '',
    account_key     TEXT        NOT NULL DEFAULT '',
    account_display TEXT        NOT NULL DEFAULT '',
    note            TEXT        NOT NULL DEFAULT '',
    created_by      TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_customer_request_customer_links_identity
        UNIQUE (tenant_id, request_id, subject_hash, subject_key, account_key),
    CONSTRAINT fk_customer_request_customer_links_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_customer_request_customer_identity
        CHECK (subject_key <> '' OR subject_hash <> '' OR account_key <> ''),
    CONSTRAINT chk_customer_request_customer_subject_key_length CHECK (length(subject_key) <= 512),
    CONSTRAINT chk_customer_request_customer_subject_hash_length CHECK (length(subject_hash) <= 128),
    CONSTRAINT chk_customer_request_customer_subject_display_length CHECK (length(subject_display) <= 500),
    CONSTRAINT chk_customer_request_customer_account_key_length CHECK (length(account_key) <= 512),
    CONSTRAINT chk_customer_request_customer_account_display_length CHECK (length(account_display) <= 500),
    CONSTRAINT chk_customer_request_customer_note_length CHECK (length(note) <= 5000)
);

CREATE INDEX IF NOT EXISTS idx_customer_request_customer_links_tenant_request
    ON customer_request_customer_links (tenant_id, request_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_customer_request_customer_links_identity
    ON customer_request_customer_links (tenant_id, subject_hash, subject_key, account_key);

CREATE TABLE IF NOT EXISTS customer_request_votes (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT        NOT NULL,
    request_id      UUID        NOT NULL,
    subject_key     TEXT        NOT NULL DEFAULT '',
    subject_hash    TEXT        NOT NULL DEFAULT '',
    subject_display TEXT        NOT NULL DEFAULT '',
    account_key     TEXT        NOT NULL DEFAULT '',
    account_display TEXT        NOT NULL DEFAULT '',
    weight          INT         NOT NULL DEFAULT 1,
    note            TEXT        NOT NULL DEFAULT '',
    created_by      TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_customer_request_votes_identity
        UNIQUE (tenant_id, request_id, subject_hash, subject_key, account_key),
    CONSTRAINT fk_customer_request_votes_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_customer_request_votes_identity
        CHECK (subject_key <> '' OR subject_hash <> '' OR account_key <> ''),
    CONSTRAINT chk_customer_request_votes_weight CHECK (weight BETWEEN 1 AND 100),
    CONSTRAINT chk_customer_request_votes_subject_key_length CHECK (length(subject_key) <= 512),
    CONSTRAINT chk_customer_request_votes_subject_hash_length CHECK (length(subject_hash) <= 128),
    CONSTRAINT chk_customer_request_votes_subject_display_length CHECK (length(subject_display) <= 500),
    CONSTRAINT chk_customer_request_votes_account_key_length CHECK (length(account_key) <= 512),
    CONSTRAINT chk_customer_request_votes_account_display_length CHECK (length(account_display) <= 500),
    CONSTRAINT chk_customer_request_votes_note_length CHECK (length(note) <= 5000)
);

CREATE INDEX IF NOT EXISTS idx_customer_request_votes_tenant_request
    ON customer_request_votes (tenant_id, request_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_customer_request_votes_identity
    ON customer_request_votes (tenant_id, subject_hash, subject_key, account_key);

CREATE TABLE IF NOT EXISTS customer_request_issue_links (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      TEXT        NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    request_id     UUID        NOT NULL,
    provider       TEXT        NOT NULL,
    external_key   TEXT        NOT NULL,
    external_url   TEXT        NOT NULL,
    title          TEXT        NOT NULL DEFAULT '',
    status         TEXT        NOT NULL DEFAULT '',
    created_by     TEXT        NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_synced_at TIMESTAMPTZ,

    CONSTRAINT uq_customer_request_issue_links_ref UNIQUE (tenant_id, request_id, provider, external_key),
    CONSTRAINT fk_customer_request_issue_links_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_customer_request_issue_provider CHECK (provider IN ('github', 'jira', 'linear', 'other')),
    CONSTRAINT chk_customer_request_issue_external_key_length CHECK (length(external_key) BETWEEN 1 AND 512),
    CONSTRAINT chk_customer_request_issue_external_url_length CHECK (length(external_url) BETWEEN 1 AND 2048),
    CONSTRAINT chk_customer_request_issue_title_length CHECK (length(title) <= 500),
    CONSTRAINT chk_customer_request_issue_status_length CHECK (length(status) <= 120)
);

CREATE INDEX IF NOT EXISTS idx_customer_request_issue_links_tenant_request
    ON customer_request_issue_links (tenant_id, request_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_customer_request_issue_links_tenant_provider
    ON customer_request_issue_links (tenant_id, provider, external_key);

CREATE OR REPLACE FUNCTION update_customer_request_issue_links_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_customer_request_issue_links_updated_at ON customer_request_issue_links;
CREATE TRIGGER trg_customer_request_issue_links_updated_at
    BEFORE UPDATE ON customer_request_issue_links
    FOR EACH ROW
    EXECUTE FUNCTION update_customer_request_issue_links_updated_at();

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
        'mcp.reclassify',
        'mcp.record_signal',
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
