-- SPDX-License-Identifier: Apache-2.0
--
-- Public visibility and moderation policy (#215): tenant-scoped publication
-- rules, cross-surface moderation state, and public request projection.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS public_visibility_policies (
    tenant_id                 TEXT        PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    portal_access_mode        TEXT        NOT NULL DEFAULT 'disabled',
    search_indexing_enabled   BOOLEAN     NOT NULL DEFAULT false,
    requests_enabled          BOOLEAN     NOT NULL DEFAULT false,
    comments_enabled          BOOLEAN     NOT NULL DEFAULT false,
    roadmap_enabled           BOOLEAN     NOT NULL DEFAULT false,
    changelog_enabled         BOOLEAN     NOT NULL DEFAULT false,
    submission_write_mode     TEXT        NOT NULL DEFAULT 'disabled',
    comment_write_mode        TEXT        NOT NULL DEFAULT 'disabled',
    vote_write_mode           TEXT        NOT NULL DEFAULT 'disabled',
    default_request_state     TEXT        NOT NULL DEFAULT 'pending',
    default_comment_state     TEXT        NOT NULL DEFAULT 'pending',
    submitter_identity_mode   TEXT        NOT NULL DEFAULT 'anonymous',
    show_vote_count           BOOLEAN     NOT NULL DEFAULT true,
    show_comment_count        BOOLEAN     NOT NULL DEFAULT true,
    show_submitter_display    BOOLEAN     NOT NULL DEFAULT false,
    hide_public_timestamps    BOOLEAN     NOT NULL DEFAULT false,
    updated_by                TEXT        NOT NULL,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_public_visibility_policy_access_mode
        CHECK (portal_access_mode IN ('disabled', 'public', 'authenticated', 'invite_only')),
    CONSTRAINT chk_public_visibility_policy_runtime_access_mode
        CHECK (portal_access_mode IN ('disabled', 'public')),
    CONSTRAINT chk_public_visibility_policy_submission_write_mode
        CHECK (submission_write_mode IN ('disabled', 'anonymous', 'identified')),
    CONSTRAINT chk_public_visibility_policy_comment_write_mode
        CHECK (comment_write_mode IN ('disabled', 'anonymous', 'identified')),
    CONSTRAINT chk_public_visibility_policy_vote_write_mode
        CHECK (vote_write_mode IN ('disabled', 'anonymous', 'identified')),
    CONSTRAINT chk_public_visibility_policy_default_request_state
        CHECK (default_request_state IN ('pending', 'approved')),
    CONSTRAINT chk_public_visibility_policy_default_comment_state
        CHECK (default_comment_state IN ('pending', 'approved')),
    CONSTRAINT chk_public_visibility_policy_identity_mode
        CHECK (submitter_identity_mode IN ('anonymous', 'display_name', 'organization')),
    CONSTRAINT chk_public_visibility_policy_updated_by_length CHECK (length(updated_by) BETWEEN 1 AND 256)
);

CREATE OR REPLACE FUNCTION update_public_visibility_policies_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_public_visibility_policies_updated_at ON public_visibility_policies;
CREATE TRIGGER trg_public_visibility_policies_updated_at
    BEFORE UPDATE ON public_visibility_policies
    FOR EACH ROW
    EXECUTE FUNCTION update_public_visibility_policies_updated_at();

CREATE TABLE IF NOT EXISTS public_request_profiles (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            TEXT        NOT NULL,
    request_id           UUID        NOT NULL,
    public_slug          TEXT        NOT NULL,
    public_title         TEXT        NOT NULL,
    public_summary       TEXT        NOT NULL DEFAULT '',
    public_state         TEXT        NOT NULL DEFAULT '',
    roadmap_column       TEXT        NOT NULL DEFAULT '',
    included_in_portal   BOOLEAN     NOT NULL DEFAULT false,
    included_in_roadmap  BOOLEAN     NOT NULL DEFAULT false,
    published_at         TIMESTAMPTZ,
    updated_by           TEXT        NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_public_request_profiles_tenant_request UNIQUE (tenant_id, request_id),
    CONSTRAINT uq_public_request_profiles_tenant_slug UNIQUE (tenant_id, public_slug),
    CONSTRAINT fk_public_request_profiles_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_public_request_profiles_slug_length CHECK (length(public_slug) BETWEEN 1 AND 160),
    CONSTRAINT chk_public_request_profiles_title_length CHECK (length(public_title) BETWEEN 1 AND 200),
    CONSTRAINT chk_public_request_profiles_summary_length CHECK (length(public_summary) <= 2000),
    CONSTRAINT chk_public_request_profiles_state_length CHECK (length(public_state) <= 80),
    CONSTRAINT chk_public_request_profiles_column_length CHECK (length(roadmap_column) <= 80),
    CONSTRAINT chk_public_request_profiles_updated_by_length CHECK (length(updated_by) BETWEEN 1 AND 256)
);

CREATE INDEX IF NOT EXISTS idx_public_request_profiles_portal
    ON public_request_profiles (tenant_id, included_in_portal, updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_public_request_profiles_roadmap
    ON public_request_profiles (tenant_id, included_in_roadmap, roadmap_column, updated_at DESC)
    WHERE included_in_roadmap;

CREATE OR REPLACE FUNCTION update_public_request_profiles_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_public_request_profiles_updated_at ON public_request_profiles;
CREATE TRIGGER trg_public_request_profiles_updated_at
    BEFORE UPDATE ON public_request_profiles
    FOR EACH ROW
    EXECUTE FUNCTION update_public_request_profiles_updated_at();

CREATE TABLE IF NOT EXISTS public_moderation_subjects (
    id                       UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    surface                  TEXT        NOT NULL,
    subject_id               TEXT        NOT NULL,
    state                    TEXT        NOT NULL DEFAULT 'pending',
    reason_code              TEXT        NOT NULL DEFAULT '',
    reason_note              TEXT        NOT NULL DEFAULT '',
    submitted_by_display     TEXT        NOT NULL DEFAULT '',
    submitted_by_fingerprint TEXT        NOT NULL DEFAULT '',
    reviewed_by              TEXT        NOT NULL DEFAULT '',
    reviewed_at              TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_public_moderation_subjects_subject UNIQUE (tenant_id, surface, subject_id),
    CONSTRAINT chk_public_moderation_subjects_surface
        CHECK (surface IN ('request', 'request_comment', 'roadmap_item', 'changelog_post', 'portal_submission')),
    CONSTRAINT chk_public_moderation_subjects_state
        CHECK (state IN ('pending', 'approved', 'rejected', 'hidden', 'spam')),
    CONSTRAINT chk_public_moderation_subjects_reason_code_length CHECK (length(reason_code) <= 80),
    CONSTRAINT chk_public_moderation_subjects_reason_code_format
        CHECK (reason_code = '' OR reason_code ~ '^[a-z0-9][a-z0-9_.-]{0,79}$'),
    CONSTRAINT chk_public_moderation_subjects_reason_note_length CHECK (length(reason_note) <= 1000),
    CONSTRAINT chk_public_moderation_subjects_submitter_display_length CHECK (length(submitted_by_display) <= 200),
    CONSTRAINT chk_public_moderation_subjects_fingerprint_length CHECK (length(submitted_by_fingerprint) <= 128),
    CONSTRAINT chk_public_moderation_subjects_reviewed_by_length CHECK (length(reviewed_by) <= 256)
);

CREATE INDEX IF NOT EXISTS idx_public_moderation_subjects_queue
    ON public_moderation_subjects (tenant_id, state, surface, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_public_moderation_subjects_subject
    ON public_moderation_subjects (tenant_id, surface, subject_id);

CREATE OR REPLACE FUNCTION update_public_moderation_subjects_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_public_moderation_subjects_updated_at ON public_moderation_subjects;
CREATE TRIGGER trg_public_moderation_subjects_updated_at
    BEFORE UPDATE ON public_moderation_subjects
    FOR EACH ROW
    EXECUTE FUNCTION update_public_moderation_subjects_updated_at();

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
        'customer_request.add_note',
        'customer_request.delete_note',
        'customer_request.merge',
        'customer_request.link_issue',
        'customer_request.unlink_issue',
        'customer_request.record_issue_sync',
        'customer_request.update_scoring_settings',
        'public_policy.update',
        'public_request_profile.upsert',
        'moderation.approve',
        'moderation.reject',
        'moderation.hide',
        'moderation.mark_spam',
        'moderation.restore',
        'digest_subscription.delete',
        'digest_subscription.upsert',
        'enrich_config.activate_version',
        'enrich_config.promote_suggested',
        'enrich_config.update',
        'enrichment_runtime.reset',
        'enrichment_runtime.rollback',
        'enrichment_runtime.update',
        'external_connection.create',
        'external_connection.update',
        'external_connection.delete',
        'external_connection.qualify',
        'external_connection.resume',
        'external_connection.test',
        'external_sync_mapping.update',
        'external_sync_cursor.reset',
        'external_sync_run.request',
        'external_sync_run.backfill',
        'external_sync_run.retry',
        'external_sync_failure.retry',
        'external_sync_conflict.resolve',
        'external_sync_event.replay',
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
