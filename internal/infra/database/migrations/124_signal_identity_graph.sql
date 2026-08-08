-- SPDX-License-Identifier: Apache-2.0
--
-- Durable customer signal identity graph foundation.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS signal_subjects (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    display_name             TEXT NOT NULL DEFAULT '',
    primary_identity_kind    TEXT NOT NULL DEFAULT '',
    primary_identity_value   TEXT NOT NULL DEFAULT '',
    status                   TEXT NOT NULL DEFAULT 'active',
    created_by               TEXT NOT NULL,
    updated_by               TEXT NOT NULL,
    merged_into_subject_id   UUID,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at              TIMESTAMPTZ,
    CONSTRAINT uq_signal_subjects_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT fk_signal_subjects_merged_same_tenant
        FOREIGN KEY (tenant_id, merged_into_subject_id)
        REFERENCES signal_subjects(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_signal_subjects_status CHECK (status IN ('active', 'merged', 'archived')),
    CONSTRAINT chk_signal_subjects_display_length CHECK (length(display_name) <= 500),
    CONSTRAINT chk_signal_subjects_primary_kind_length CHECK (length(primary_identity_kind) <= 80),
    CONSTRAINT chk_signal_subjects_primary_value_length CHECK (length(primary_identity_value) <= 512),
    CONSTRAINT chk_signal_subjects_actor_lengths CHECK (length(created_by) <= 256 AND length(updated_by) <= 256),
    CONSTRAINT chk_signal_subjects_merge_shape CHECK ((status = 'merged') = (merged_into_subject_id IS NOT NULL)),
    CONSTRAINT chk_signal_subjects_not_self_merged CHECK (merged_into_subject_id IS NULL OR merged_into_subject_id <> id),
    CONSTRAINT chk_signal_subjects_archive_shape CHECK ((status = 'archived') = (archived_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_signal_subjects_tenant_status
    ON signal_subjects (tenant_id, status, updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_signal_subjects_merged_into
    ON signal_subjects (tenant_id, merged_into_subject_id)
    WHERE merged_into_subject_id IS NOT NULL;

CREATE OR REPLACE FUNCTION update_signal_subjects_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_signal_subjects_updated_at ON signal_subjects;
CREATE TRIGGER trg_signal_subjects_updated_at
    BEFORE UPDATE ON signal_subjects
    FOR EACH ROW
    EXECUTE FUNCTION update_signal_subjects_updated_at();

CREATE TABLE IF NOT EXISTS signal_subject_identities (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id              TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    subject_id             UUID NOT NULL,
    kind                   TEXT NOT NULL,
    value                  TEXT NOT NULL,
    value_normalized       TEXT NOT NULL,
    source                 TEXT NOT NULL DEFAULT 'review',
    confidence             TEXT NOT NULL DEFAULT 'reviewed',
    first_feedback_id      BIGINT,
    latest_feedback_id     BIGINT,
    evidence_count         INT NOT NULL DEFAULT 0,
    created_by             TEXT NOT NULL,
    updated_by             TEXT NOT NULL,
    revoked_at             TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_signal_subject_identities_subject
        FOREIGN KEY (tenant_id, subject_id)
        REFERENCES signal_subjects(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_signal_subject_identities_kind
        CHECK (kind IN ('email', 'external_id', 'source_contact_id', 'crm_id', 'support_id')),
    CONSTRAINT chk_signal_subject_identities_value_lengths
        CHECK (length(value) BETWEEN 1 AND 512 AND length(value_normalized) BETWEEN 1 AND 512),
    CONSTRAINT chk_signal_subject_identities_source_length CHECK (length(source) <= 120),
    CONSTRAINT chk_signal_subject_identities_confidence
        CHECK (confidence IN ('observed', 'reviewed', 'conflict')),
    CONSTRAINT chk_signal_subject_identities_evidence CHECK (evidence_count >= 0),
    CONSTRAINT chk_signal_subject_identities_actor_lengths CHECK (length(created_by) <= 256 AND length(updated_by) <= 256)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_signal_subject_identities_active_key
    ON signal_subject_identities (tenant_id, kind, value_normalized)
    WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_signal_subject_identities_subject_key
    ON signal_subject_identities (tenant_id, subject_id, kind, value_normalized)
    WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_signal_subject_identities_subject
    ON signal_subject_identities (tenant_id, subject_id, updated_at DESC, id DESC)
    WHERE revoked_at IS NULL;

CREATE OR REPLACE FUNCTION update_signal_subject_identities_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_signal_subject_identities_updated_at ON signal_subject_identities;
CREATE TRIGGER trg_signal_subject_identities_updated_at
    BEFORE UPDATE ON signal_subject_identities
    FOR EACH ROW
    EXECUTE FUNCTION update_signal_subject_identities_updated_at();

CREATE TABLE IF NOT EXISTS signal_subject_merge_events (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    subject_id                 UUID NOT NULL,
    action                     TEXT NOT NULL DEFAULT 'review_merge',
    identity_kind              TEXT NOT NULL,
    identity_value             TEXT NOT NULL,
    identity_value_normalized  TEXT NOT NULL,
    feedback_ids               BIGINT[] NOT NULL DEFAULT '{}'::bigint[],
    evidence_count             INT NOT NULL,
    note                       TEXT NOT NULL DEFAULT '',
    created_by                 TEXT NOT NULL,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_signal_subject_merge_events_subject
        FOREIGN KEY (tenant_id, subject_id)
        REFERENCES signal_subjects(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_signal_subject_merge_events_action CHECK (action IN ('review_merge', 'split')),
    CONSTRAINT chk_signal_subject_merge_events_identity_kind
        CHECK (identity_kind IN ('email', 'external_id', 'source_contact_id', 'crm_id', 'support_id')),
    CONSTRAINT chk_signal_subject_merge_events_identity_value_lengths
        CHECK (length(identity_value) BETWEEN 1 AND 512 AND length(identity_value_normalized) BETWEEN 1 AND 512),
    CONSTRAINT chk_signal_subject_merge_events_feedback_ids
        CHECK (
            (action = 'review_merge' AND cardinality(feedback_ids) BETWEEN 2 AND 50)
            OR (action = 'split' AND cardinality(feedback_ids) BETWEEN 0 AND 50)
        ),
    CONSTRAINT chk_signal_subject_merge_events_evidence_count CHECK (evidence_count >= 0),
    CONSTRAINT chk_signal_subject_merge_events_note_length CHECK (length(note) <= 1000),
    CONSTRAINT chk_signal_subject_merge_events_actor_length CHECK (length(created_by) <= 256)
);

CREATE INDEX IF NOT EXISTS idx_signal_subject_merge_events_subject
    ON signal_subject_merge_events (tenant_id, subject_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_signal_subject_merge_events_identity
    ON signal_subject_merge_events (tenant_id, identity_kind, identity_value_normalized, created_at DESC);

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
        'breakglass.approve',
        'breakglass.expire',
        'breakglass.issue',
        'breakglass.recovery_codes.generate',
        'breakglass.recovery_codes.revoke',
        'breakglass.recovery_codes.use',
        'breakglass.revoke',
        'breakglass.unlock_ip',
        'breakglass.use',
        'classification_review.record',
        'cohort.sync',
        'cohort.update',
        'cohort_source.create',
        'cohort_source.delete',
        'cohort_source.update',
        'customer_request.add_comment',
        'customer_request.add_note',
        'customer_request.add_vote',
        'customer_request.create',
        'customer_request.create_github_issue',
        'customer_request.delete_note',
        'customer_request.link_customer',
        'customer_request.link_feedback',
        'customer_request.link_issue',
        'customer_request.merge',
        'customer_request.promote_feedback',
        'customer_request.record_issue_sync',
        'customer_request.remove_vote',
        'customer_request.unlink_customer',
        'customer_request.unlink_feedback',
        'customer_request.unlink_issue',
        'customer_request.update',
        'customer_request.update_scoring_settings',
        'digest_subscription.delete',
        'digest_subscription.upsert',
        'enrich_config.activate_version',
        'enrich_config.promote_suggested',
        'enrich_config.update',
        'enrichment_runtime.reset',
        'enrichment_runtime.rollback',
        'enrichment_runtime.update',
        'external_connection.create',
        'external_connection.delete',
        'external_connection.qualify',
        'external_connection.resume',
        'external_connection.test',
        'external_connection.update',
        'external_provider_installation.create',
        'external_provider_installation.delete',
        'external_provider_installation.qualify',
        'external_provider_installation.resources_select',
        'external_sync_conflict.resolve',
        'external_sync_cursor.reset',
        'external_sync_event.replay',
        'external_sync_failure.retry',
        'external_sync_mapping.update',
        'external_sync_run.backfill',
        'external_sync_run.request',
        'external_sync_run.retry',
        'feedback.batch_delete',
        'feedback_assignment.policy_restore',
        'feedback_assignment.policy_update',
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
        'inbound_source.sync_now',
        'inbound_source.test_connection',
        'inbound_source.update',
        'llm_ability.delete',
        'llm_ability.upsert',
        'llm_channel.create',
        'llm_channel.delete',
        'llm_channel.test',
        'llm_channel.update',
        'llm_route.delete',
        'llm_route.upsert',
        'mcp.add_tag',
        'mcp.archive_tag',
        'mcp.batch_update_status',
        'mcp.batch_update_tags',
        'mcp.create_tag',
        'mcp.get_cluster',
        'mcp.get_digest',
        'mcp.get_enrichment_status',
        'mcp.get_feedback',
        'mcp.get_usage_stats',
        'mcp.get_workflow_state',
        'mcp.link_issue',
        'mcp.list_clusters',
        'mcp.list_dimensions',
        'mcp.list_feedback',
        'mcp.list_tags',
        'mcp.list_workflow_states',
        'mcp.mark_duplicate',
        'mcp.reclassify',
        'mcp.record_signal',
        'mcp.remove_tag',
        'mcp.retry_enrichment',
        'mcp.search_feedback',
        'mcp.set_urgent',
        'mcp.submit_feedback',
        'mcp.trigger_digest',
        'mcp.update_status',
        'mcp.update_tags',
        'mcp.update_workflow_state',
        'mcp_client.create',
        'mcp_client.revoke',
        'mcp_client.tool_policy_update',
        'mcp_client.update',
        'mcp_refresh_grant.revoke',
        'mcp_session.revoke',
        'mcp_tool.authorize_denied',
        'mcp_tool.rate_limited',
        'member.invite',
        'member.remove',
        'member.update_role',
        'moderation.approve',
        'moderation.hide',
        'moderation.mark_spam',
        'moderation.reject',
        'moderation.restore',
        'notify_target.create',
        'notify_target.delete',
        'notify_target.test',
        'notify_target.update',
        'outbox.retry',
        'portal_submission.create',
        'public_policy.update',
        'public_request_profile.upsert',
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
        'request_notification.bounce',
        'request_notification.complaint',
        'request_notification.delivery_dead',
        'request_notification.delivery_retry',
        'request_notification.event_create',
        'request_notification.public_update_publish',
        'request_notification.sender_verify',
        'request_notification.settings_update',
        'request_notification.subscribe',
        'request_notification.suppress_contact',
        'request_notification.unsubscribe',
        'request_notification.webhook_target_create',
        'request_notification.webhook_target_delete',
        'request_notification.webhook_target_test',
        'request_notification.webhook_target_update',
        'retry_enrichment',
        'service_account.create',
        'service_account.delete',
        'service_account.update',
        'signal_subject.merge',
        'signal_subject.split',
        'survey.campaign_archive',
        'survey.campaign_create',
        'survey.campaign_update',
        'survey.hosted_link_create',
        'survey.invitation_delivery_retry',
        'survey.low_score_review_assign',
        'survey.low_score_review_batch_update',
        'survey.low_score_review_escalate',
        'survey.low_score_review_update',
        'survey.provider_event_record',
        'survey.test_email_send',
        'tag.archive',
        'tag.create',
        'tag.update',
        'workflow_seed_defaults.run',
        'workflow_state.archive',
        'workflow_state.create',
        'workflow_state.update',
        'workflow_transition.replace'
    ));
