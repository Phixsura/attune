-- SPDX-License-Identifier: Apache-2.0
--
-- Post-resolution CSAT and CES surveys (#236).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS survey_campaigns (
    id                               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                        TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                             TEXT NOT NULL,
    survey_type                      TEXT NOT NULL,
    status                           TEXT NOT NULL DEFAULT 'draft',
    trigger_event                    TEXT NOT NULL,
    distribution_mode                TEXT NOT NULL,
    dedupe_policy                    TEXT NOT NULL DEFAULT 'one_per_source',
    trigger_filter                   JSONB NOT NULL DEFAULT '{}'::jsonb,
    content                          JSONB NOT NULL DEFAULT '{}'::jsonb,
    locale                           TEXT NOT NULL DEFAULT 'en',
    content_version                  INT NOT NULL DEFAULT 1,
    sampling_percent                 NUMERIC(5, 2) NOT NULL DEFAULT 100,
    min_days_between_contact         INT NOT NULL DEFAULT 30,
    expires_after_days               INT NOT NULL DEFAULT 14,
    max_daily_invitations            INT NOT NULL DEFAULT 0,
    low_score_threshold              INT NOT NULL DEFAULT 3,
    require_recent_customer_activity BOOLEAN NOT NULL DEFAULT true,
    recent_activity_days             INT NOT NULL DEFAULT 30,
    suppress_auto_resolved           BOOLEAN NOT NULL DEFAULT true,
    created_by                       TEXT NOT NULL,
    updated_by                       TEXT NOT NULL,
    archived_at                      TIMESTAMPTZ,
    created_at                       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_survey_campaigns_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT chk_survey_campaigns_name_length CHECK (length(name) BETWEEN 1 AND 160),
    CONSTRAINT chk_survey_campaigns_type CHECK (survey_type IN ('csat', 'ces')),
    CONSTRAINT chk_survey_campaigns_status CHECK (status IN ('draft', 'active', 'archived')),
    CONSTRAINT chk_survey_campaigns_trigger_event CHECK (trigger_event IN ('workflow_transition', 'reply_sent', 'manual_link', 'request_resolved')),
    CONSTRAINT chk_survey_campaigns_distribution_mode CHECK (distribution_mode IN ('contact_email', 'source_link')),
    CONSTRAINT chk_survey_campaigns_dedupe_policy CHECK (dedupe_policy IN ('one_per_source', 'one_per_resolution', 'one_per_trigger')),
    CONSTRAINT chk_survey_campaigns_trigger_filter_object CHECK (jsonb_typeof(trigger_filter) = 'object'),
    CONSTRAINT chk_survey_campaigns_content_object CHECK (jsonb_typeof(content) = 'object'),
    CONSTRAINT chk_survey_campaigns_locale_length CHECK (length(locale) BETWEEN 2 AND 35),
    CONSTRAINT chk_survey_campaigns_content_version CHECK (content_version > 0),
    CONSTRAINT chk_survey_campaigns_sampling CHECK (sampling_percent >= 0 AND sampling_percent <= 100),
    CONSTRAINT chk_survey_campaigns_min_days CHECK (min_days_between_contact >= 0),
    CONSTRAINT chk_survey_campaigns_expiry CHECK (expires_after_days BETWEEN 1 AND 365),
    CONSTRAINT chk_survey_campaigns_max_daily CHECK (max_daily_invitations >= 0),
    CONSTRAINT chk_survey_campaigns_low_score_threshold CHECK (low_score_threshold BETWEEN 1 AND 10),
    CONSTRAINT chk_survey_campaigns_recent_activity_days CHECK (recent_activity_days BETWEEN 0 AND 3650),
    CONSTRAINT chk_survey_campaigns_archive_shape CHECK ((status = 'archived') = (archived_at IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_survey_campaigns_tenant_status
    ON survey_campaigns (tenant_id, status, updated_at DESC, id DESC);

CREATE OR REPLACE FUNCTION update_survey_campaigns_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_survey_campaigns_updated_at ON survey_campaigns;
CREATE TRIGGER trg_survey_campaigns_updated_at
    BEFORE UPDATE ON survey_campaigns
    FOR EACH ROW
    EXECUTE FUNCTION update_survey_campaigns_updated_at();

CREATE TABLE IF NOT EXISTS survey_invitations (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    campaign_id              UUID NOT NULL,
    campaign_content_version INT NOT NULL,
    campaign_snapshot        JSONB NOT NULL DEFAULT '{}'::jsonb,
    dedupe_key               TEXT NOT NULL DEFAULT '',
    source_type              TEXT NOT NULL DEFAULT '',
    source_id                TEXT NOT NULL DEFAULT '',
    request_id               UUID,
    contact_id               UUID,
    distribution_mode        TEXT NOT NULL,
    token_hash               TEXT NOT NULL,
    delivery_status          TEXT NOT NULL DEFAULT 'pending',
    response_status          TEXT NOT NULL DEFAULT 'not_started',
    suppression_status       TEXT NOT NULL DEFAULT 'not_suppressed',
    suppression_reason       TEXT NOT NULL DEFAULT '',
    recipient_snapshot       JSONB NOT NULL DEFAULT '{}'::jsonb,
    delivery_secret          BYTEA,
    provider                 TEXT NOT NULL DEFAULT '',
    provider_message_id      TEXT NOT NULL DEFAULT '',
    attempts                 INT NOT NULL DEFAULT 0,
    failure_kind             TEXT NOT NULL DEFAULT '',
    http_status              INT,
    last_error               TEXT NOT NULL DEFAULT '',
    claimed_at               TIMESTAMPTZ,
    claimed_by               TEXT NOT NULL DEFAULT '',
    next_retry_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at             TIMESTAMPTZ,
    opened_at                TIMESTAMPTZ,
    responded_at             TIMESTAMPTZ,
    expires_at               TIMESTAMPTZ,
    created_by               TEXT NOT NULL DEFAULT 'system',
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_survey_invitations_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT fk_survey_invitations_campaign
        FOREIGN KEY (tenant_id, campaign_id)
        REFERENCES survey_campaigns(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT fk_survey_invitations_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_survey_invitations_campaign_snapshot_object CHECK (jsonb_typeof(campaign_snapshot) = 'object'),
    CONSTRAINT chk_survey_invitations_recipient_snapshot_object CHECK (jsonb_typeof(recipient_snapshot) = 'object'),
    CONSTRAINT chk_survey_invitations_delivery_secret_length CHECK (delivery_secret IS NULL OR length(delivery_secret) <= 4096),
    CONSTRAINT chk_survey_invitations_content_version CHECK (campaign_content_version > 0),
    CONSTRAINT chk_survey_invitations_distribution_mode CHECK (distribution_mode IN ('contact_email', 'source_link')),
    CONSTRAINT chk_survey_invitations_delivery_status
        CHECK (delivery_status IN ('pending', 'accepted', 'delivered', 'rejected', 'bounced', 'complained', 'delayed', 'not_applicable')),
    CONSTRAINT chk_survey_invitations_response_status
        CHECK (response_status IN ('not_started', 'opened', 'started', 'completed', 'expired')),
    CONSTRAINT chk_survey_invitations_suppression_status
        CHECK (suppression_status IN ('not_suppressed', 'suppressed')),
    CONSTRAINT chk_survey_invitations_token_hash CHECK (token_hash ~ '^[a-f0-9]{64}$'),
    CONSTRAINT chk_survey_invitations_source_type_length CHECK (length(source_type) <= 120),
    CONSTRAINT chk_survey_invitations_source_id_length CHECK (length(source_id) <= 512),
    CONSTRAINT chk_survey_invitations_dedupe_key_length CHECK (length(dedupe_key) <= 512),
    CONSTRAINT chk_survey_invitations_provider_length CHECK (length(provider) <= 120),
    CONSTRAINT chk_survey_invitations_provider_message_length CHECK (length(provider_message_id) <= 512),
    CONSTRAINT chk_survey_invitations_attempts CHECK (attempts >= 0),
    CONSTRAINT chk_survey_invitations_failure_kind_length CHECK (length(failure_kind) <= 120),
    CONSTRAINT chk_survey_invitations_http_status CHECK (http_status IS NULL OR (http_status >= 100 AND http_status <= 599)),
    CONSTRAINT chk_survey_invitations_last_error_length CHECK (length(last_error) <= 1000),
    CONSTRAINT chk_survey_invitations_claimed_by_length CHECK (length(claimed_by) <= 256)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_survey_invitations_token_hash
    ON survey_invitations (token_hash);
CREATE UNIQUE INDEX IF NOT EXISTS uq_survey_invitations_dedupe
    ON survey_invitations (tenant_id, campaign_id, dedupe_key)
    WHERE dedupe_key <> '';
CREATE INDEX IF NOT EXISTS idx_survey_invitations_campaign_created
    ON survey_invitations (tenant_id, campaign_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_survey_invitations_response_status
    ON survey_invitations (tenant_id, response_status, created_at DESC)
    WHERE response_status <> 'completed';
CREATE INDEX IF NOT EXISTS idx_survey_invitations_delivery_queue
    ON survey_invitations (next_retry_at ASC, created_at ASC, id ASC)
    WHERE distribution_mode = 'contact_email'
      AND delivery_status IN ('pending', 'delayed');
CREATE INDEX IF NOT EXISTS idx_survey_invitations_expiry_queue
    ON survey_invitations (expires_at ASC, created_at ASC, id ASC)
    WHERE expires_at IS NOT NULL
      AND response_status NOT IN ('completed', 'expired');

CREATE OR REPLACE FUNCTION update_survey_invitations_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_survey_invitations_updated_at ON survey_invitations;
CREATE TRIGGER trg_survey_invitations_updated_at
    BEFORE UPDATE ON survey_invitations
    FOR EACH ROW
    EXECUTE FUNCTION update_survey_invitations_updated_at();

CREATE TABLE IF NOT EXISTS survey_responses (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    campaign_id      UUID NOT NULL,
    invitation_id    UUID NOT NULL,
    request_id       UUID,
    contact_id       UUID,
    source_type      TEXT NOT NULL DEFAULT '',
    source_id        TEXT NOT NULL DEFAULT '',
    score            INT NOT NULL,
    comment          TEXT NOT NULL DEFAULT '',
    locale           TEXT NOT NULL DEFAULT '',
    metadata         JSONB NOT NULL DEFAULT '{}'::jsonb,
    user_agent_hash  TEXT NOT NULL DEFAULT '',
    ip_hash          TEXT NOT NULL DEFAULT '',
    submitted_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_survey_responses_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT fk_survey_responses_campaign
        FOREIGN KEY (tenant_id, campaign_id)
        REFERENCES survey_campaigns(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT fk_survey_responses_invitation
        FOREIGN KEY (tenant_id, invitation_id)
        REFERENCES survey_invitations(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT fk_survey_responses_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_survey_responses_score CHECK (score BETWEEN 1 AND 10),
    CONSTRAINT chk_survey_responses_comment_length CHECK (length(comment) <= 5000),
    CONSTRAINT chk_survey_responses_metadata_object CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT chk_survey_responses_source_type_length CHECK (length(source_type) <= 120),
    CONSTRAINT chk_survey_responses_source_id_length CHECK (length(source_id) <= 512),
    CONSTRAINT chk_survey_responses_hash_lengths CHECK (length(user_agent_hash) <= 80 AND length(ip_hash) <= 80)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_survey_responses_invitation
    ON survey_responses (tenant_id, invitation_id);
CREATE INDEX IF NOT EXISTS idx_survey_responses_campaign_submitted
    ON survey_responses (tenant_id, campaign_id, submitted_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_survey_responses_request
    ON survey_responses (tenant_id, request_id, submitted_at DESC)
    WHERE request_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS survey_low_score_reviews (
    response_id        UUID PRIMARY KEY,
    tenant_id          TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    campaign_id        UUID NOT NULL,
    status             TEXT NOT NULL DEFAULT 'open',
    severity           TEXT NOT NULL DEFAULT 'medium',
    owner_member_id    UUID,
    root_cause         TEXT NOT NULL DEFAULT '',
    action_taken       TEXT NOT NULL DEFAULT '',
    customer_contacted BOOLEAN NOT NULL DEFAULT false,
    due_at             TIMESTAMPTZ,
    reviewed_at        TIMESTAMPTZ,
    claimed_at         TIMESTAMPTZ,
    claimed_by         TEXT NOT NULL DEFAULT '',
    updated_by         TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_survey_low_score_reviews_tenant_response UNIQUE (tenant_id, response_id),
    CONSTRAINT fk_survey_low_score_reviews_response
        FOREIGN KEY (tenant_id, response_id)
        REFERENCES survey_responses(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT fk_survey_low_score_reviews_campaign
        FOREIGN KEY (tenant_id, campaign_id)
        REFERENCES survey_campaigns(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_survey_low_score_reviews_status
        CHECK (status IN ('open', 'in_review', 'resolved', 'dismissed')),
    CONSTRAINT chk_survey_low_score_reviews_severity
        CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    CONSTRAINT chk_survey_low_score_reviews_root_cause_length CHECK (length(root_cause) <= 120),
    CONSTRAINT chk_survey_low_score_reviews_action_length CHECK (length(action_taken) <= 5000),
    CONSTRAINT chk_survey_low_score_reviews_claimed_by_length CHECK (length(claimed_by) <= 256),
    CONSTRAINT chk_survey_low_score_reviews_updated_by_length CHECK (length(updated_by) <= 256)
);

CREATE INDEX IF NOT EXISTS idx_survey_low_score_reviews_queue
    ON survey_low_score_reviews (tenant_id, campaign_id, status, due_at NULLS LAST, severity, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_survey_low_score_reviews_automation_queue
    ON survey_low_score_reviews (due_at ASC NULLS FIRST, updated_at ASC, response_id ASC)
    WHERE status IN ('open', 'in_review');

CREATE OR REPLACE FUNCTION update_survey_low_score_reviews_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_survey_low_score_reviews_updated_at ON survey_low_score_reviews;
CREATE TRIGGER trg_survey_low_score_reviews_updated_at
    BEFORE UPDATE ON survey_low_score_reviews
    FOR EACH ROW
    EXECUTE FUNCTION update_survey_low_score_reviews_updated_at();

CREATE TABLE IF NOT EXISTS survey_recovery_notifications (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    response_id         UUID NOT NULL,
    owner_member_id     UUID,
    channel             TEXT NOT NULL DEFAULT 'email',
    status              TEXT NOT NULL DEFAULT 'pending',
    reason              TEXT NOT NULL DEFAULT '',
    destination_hash    TEXT NOT NULL DEFAULT '',
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb,
    provider            TEXT NOT NULL DEFAULT '',
    provider_message_id TEXT NOT NULL DEFAULT '',
    attempts            INT NOT NULL DEFAULT 0,
    failure_kind        TEXT NOT NULL DEFAULT '',
    http_status         INT,
    last_error          TEXT NOT NULL DEFAULT '',
    claimed_at          TIMESTAMPTZ,
    claimed_by          TEXT NOT NULL DEFAULT '',
    next_retry_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_survey_recovery_notifications_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT fk_survey_recovery_notifications_response
        FOREIGN KEY (tenant_id, response_id)
        REFERENCES survey_responses(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT fk_survey_recovery_notifications_owner
        FOREIGN KEY (owner_member_id)
        REFERENCES tenant_members(id)
        ON DELETE SET NULL,
    CONSTRAINT chk_survey_recovery_notifications_channel
        CHECK (channel IN ('email')),
    CONSTRAINT chk_survey_recovery_notifications_status
        CHECK (status IN ('pending', 'delivered', 'failed', 'dead', 'suppressed')),
    CONSTRAINT chk_survey_recovery_notifications_reason_length CHECK (length(reason) <= 120),
    CONSTRAINT chk_survey_recovery_notifications_destination_hash_length CHECK (length(destination_hash) <= 80),
    CONSTRAINT chk_survey_recovery_notifications_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT chk_survey_recovery_notifications_provider_length CHECK (length(provider) <= 120),
    CONSTRAINT chk_survey_recovery_notifications_provider_message_length CHECK (length(provider_message_id) <= 512),
    CONSTRAINT chk_survey_recovery_notifications_attempts CHECK (attempts >= 0),
    CONSTRAINT chk_survey_recovery_notifications_failure_kind_length CHECK (length(failure_kind) <= 120),
    CONSTRAINT chk_survey_recovery_notifications_http_status CHECK (http_status IS NULL OR (http_status >= 100 AND http_status <= 599)),
    CONSTRAINT chk_survey_recovery_notifications_last_error_length CHECK (length(last_error) <= 1000),
    CONSTRAINT chk_survey_recovery_notifications_claimed_by_length CHECK (length(claimed_by) <= 256)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_survey_recovery_notifications_owner_reason
    ON survey_recovery_notifications (tenant_id, response_id, owner_member_id, channel, reason)
    WHERE owner_member_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_survey_recovery_notifications_queue
    ON survey_recovery_notifications (next_retry_at ASC, created_at ASC, id ASC)
    WHERE channel = 'email'
      AND status IN ('pending', 'failed');

CREATE OR REPLACE FUNCTION update_survey_recovery_notifications_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_survey_recovery_notifications_updated_at ON survey_recovery_notifications;
CREATE TRIGGER trg_survey_recovery_notifications_updated_at
    BEFORE UPDATE ON survey_recovery_notifications
    FOR EACH ROW
    EXECUTE FUNCTION update_survey_recovery_notifications_updated_at();

CREATE TABLE IF NOT EXISTS survey_provider_events (
    id                  BIGSERIAL PRIMARY KEY,
    tenant_id           TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    invitation_id       UUID,
    provider            TEXT NOT NULL,
    provider_event_type TEXT NOT NULL,
    provider_message_id TEXT NOT NULL DEFAULT '',
    provider_event_key  TEXT NOT NULL DEFAULT '',
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_survey_provider_events_invitation
        FOREIGN KEY (tenant_id, invitation_id)
        REFERENCES survey_invitations(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_survey_provider_events_provider_length CHECK (length(provider) BETWEEN 1 AND 120),
    CONSTRAINT chk_survey_provider_events_type
        CHECK (provider_event_type IN ('accepted', 'delivered', 'bounced', 'complained', 'rejected', 'temporarily_delayed', 'opened')),
    CONSTRAINT chk_survey_provider_events_message_length CHECK (length(provider_message_id) <= 512),
    CONSTRAINT chk_survey_provider_events_key_length CHECK (length(provider_event_key) <= 512),
    CONSTRAINT chk_survey_provider_events_payload_object CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_survey_provider_events_invitation
    ON survey_provider_events (tenant_id, invitation_id, created_at DESC)
    WHERE invitation_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_survey_provider_events_message
    ON survey_provider_events (tenant_id, provider, provider_message_id)
    WHERE provider_message_id <> '';
CREATE UNIQUE INDEX IF NOT EXISTS uq_survey_provider_events_key
    ON survey_provider_events (tenant_id, provider, provider_event_key)
    WHERE provider_event_key <> '';

ALTER TABLE gdpr_requests
    ADD COLUMN IF NOT EXISTS survey_invitation_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS survey_response_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS survey_low_score_review_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS survey_provider_event_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS survey_recovery_notification_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE gdpr_requests
    DROP CONSTRAINT IF EXISTS chk_gdpr_requests_survey_invitation_count,
    DROP CONSTRAINT IF EXISTS chk_gdpr_requests_survey_response_count,
    DROP CONSTRAINT IF EXISTS chk_gdpr_requests_survey_low_score_review_count,
    DROP CONSTRAINT IF EXISTS chk_gdpr_requests_survey_provider_event_count,
    DROP CONSTRAINT IF EXISTS chk_gdpr_requests_survey_recovery_notification_count;

ALTER TABLE gdpr_requests
    ADD CONSTRAINT chk_gdpr_requests_survey_invitation_count CHECK (survey_invitation_count >= 0),
    ADD CONSTRAINT chk_gdpr_requests_survey_response_count CHECK (survey_response_count >= 0),
    ADD CONSTRAINT chk_gdpr_requests_survey_low_score_review_count CHECK (survey_low_score_review_count >= 0),
    ADD CONSTRAINT chk_gdpr_requests_survey_provider_event_count CHECK (survey_provider_event_count >= 0),
    ADD CONSTRAINT chk_gdpr_requests_survey_recovery_notification_count CHECK (survey_recovery_notification_count >= 0);

ALTER TABLE gdpr_export_jobs
    ADD COLUMN IF NOT EXISTS survey_invitation_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS survey_response_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS survey_low_score_review_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS survey_provider_event_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS survey_recovery_notification_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE gdpr_export_jobs
    DROP CONSTRAINT IF EXISTS chk_gdpr_export_jobs_survey_invitation_count,
    DROP CONSTRAINT IF EXISTS chk_gdpr_export_jobs_survey_response_count,
    DROP CONSTRAINT IF EXISTS chk_gdpr_export_jobs_survey_low_score_review_count,
    DROP CONSTRAINT IF EXISTS chk_gdpr_export_jobs_survey_provider_event_count,
    DROP CONSTRAINT IF EXISTS chk_gdpr_export_jobs_survey_recovery_notification_count;

ALTER TABLE gdpr_export_jobs
    ADD CONSTRAINT chk_gdpr_export_jobs_survey_invitation_count CHECK (survey_invitation_count >= 0),
    ADD CONSTRAINT chk_gdpr_export_jobs_survey_response_count CHECK (survey_response_count >= 0),
    ADD CONSTRAINT chk_gdpr_export_jobs_survey_low_score_review_count CHECK (survey_low_score_review_count >= 0),
    ADD CONSTRAINT chk_gdpr_export_jobs_survey_provider_event_count CHECK (survey_provider_event_count >= 0),
    ADD CONSTRAINT chk_gdpr_export_jobs_survey_recovery_notification_count CHECK (survey_recovery_notification_count >= 0);

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
