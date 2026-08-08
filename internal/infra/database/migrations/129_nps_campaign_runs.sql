-- SPDX-License-Identifier: Apache-2.0
--
-- Relationship NPS campaigns (#236).

ALTER TABLE survey_campaigns
    DROP CONSTRAINT IF EXISTS chk_survey_campaigns_type,
    DROP CONSTRAINT IF EXISTS chk_survey_campaigns_trigger_event,
    DROP CONSTRAINT IF EXISTS chk_survey_campaigns_dedupe_policy;

ALTER TABLE survey_campaigns
    ADD CONSTRAINT chk_survey_campaigns_type
        CHECK (survey_type IN ('csat', 'ces', 'nps')),
    ADD CONSTRAINT chk_survey_campaigns_trigger_event
        CHECK (trigger_event IN ('workflow_transition', 'reply_sent', 'manual_link', 'request_resolved', 'scheduled_run')),
    ADD CONSTRAINT chk_survey_campaigns_dedupe_policy
        CHECK (dedupe_policy IN ('one_per_source', 'one_per_resolution', 'one_per_trigger', 'one_per_run')),
    ADD CONSTRAINT chk_survey_campaigns_nps_shape
        CHECK (
            survey_type <> 'nps' OR (
                trigger_event = 'scheduled_run'
                AND distribution_mode = 'contact_email'
                AND dedupe_policy = 'one_per_run'
                AND low_score_threshold = 6
                AND max_daily_invitations = 0
		AND min_days_between_contact BETWEEN 30 AND 365
                AND require_recent_customer_activity = FALSE
                AND recent_activity_days = 0
                AND suppress_auto_resolved = FALSE
            )
        );

CREATE UNIQUE INDEX IF NOT EXISTS uq_cohorts_tenant_id
    ON cohorts (tenant_id, id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_tenant_members_tenant_id
    ON tenant_members (tenant_id, id);

CREATE TABLE IF NOT EXISTS survey_nps_campaign_settings (
    campaign_id             UUID PRIMARY KEY,
    tenant_id               TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cohort_id               UUID NOT NULL,
    detractor_owner_member_id UUID NOT NULL,
    collection_days         INT NOT NULL,
    maximum_run_recipients  INT NOT NULL,
    sample_seed             TEXT NOT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_survey_nps_campaign_settings_campaign
        FOREIGN KEY (tenant_id, campaign_id)
        REFERENCES survey_campaigns(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT fk_survey_nps_campaign_settings_cohort
        FOREIGN KEY (tenant_id, cohort_id)
        REFERENCES cohorts(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_survey_nps_campaign_settings_detractor_owner
        FOREIGN KEY (tenant_id, detractor_owner_member_id)
        REFERENCES tenant_members(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_survey_nps_campaign_settings_collection_days
        CHECK (collection_days BETWEEN 7 AND 30),
    CONSTRAINT chk_survey_nps_campaign_settings_maximum_run_recipients
        CHECK (maximum_run_recipients BETWEEN 1 AND 100000),
    CONSTRAINT chk_survey_nps_campaign_settings_sample_seed_length
        CHECK (length(sample_seed) BETWEEN 16 AND 128)
);

CREATE INDEX IF NOT EXISTS idx_survey_nps_campaign_settings_cohort
    ON survey_nps_campaign_settings (tenant_id, cohort_id);

CREATE OR REPLACE FUNCTION update_survey_nps_campaign_settings_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_survey_nps_campaign_settings_updated_at
    BEFORE UPDATE ON survey_nps_campaign_settings
    FOR EACH ROW
    EXECUTE FUNCTION update_survey_nps_campaign_settings_updated_at();

CREATE TABLE IF NOT EXISTS survey_campaign_runs (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    campaign_id              UUID NOT NULL,
    sequence                 INT NOT NULL,
    client_request_key       UUID NOT NULL,
    request_fingerprint      TEXT NOT NULL,
    status                   TEXT NOT NULL DEFAULT 'scheduled',
    scheduled_at             TIMESTAMPTZ NOT NULL,
    opened_at                TIMESTAMPTZ,
    closes_at                TIMESTAMPTZ,
    definition_snapshot      JSONB NOT NULL DEFAULT '{}'::jsonb,
    evaluated_count          INT NOT NULL DEFAULT 0,
    eligible_count           INT NOT NULL DEFAULT 0,
    invitation_count         INT NOT NULL DEFAULT 0,
    redacted_response_count  INT NOT NULL DEFAULT 0,
    failure_reason           TEXT NOT NULL DEFAULT '',
    claimed_at               TIMESTAMPTZ,
    claimed_by               TEXT NOT NULL DEFAULT '',
    created_by               TEXT NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_survey_campaign_runs_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT uq_survey_campaign_runs_tenant_run_campaign UNIQUE (tenant_id, id, campaign_id),
    CONSTRAINT uq_survey_campaign_runs_request_key UNIQUE (tenant_id, campaign_id, client_request_key),
    CONSTRAINT uq_survey_campaign_runs_sequence UNIQUE (tenant_id, campaign_id, sequence),
    CONSTRAINT fk_survey_campaign_runs_campaign
        FOREIGN KEY (tenant_id, campaign_id)
        REFERENCES survey_campaigns(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT chk_survey_campaign_runs_status
        CHECK (status IN ('scheduled', 'evaluating', 'collecting', 'closed', 'failed')),
    CONSTRAINT chk_survey_campaign_runs_fingerprint_length
        CHECK (length(request_fingerprint) = 64),
    CONSTRAINT chk_survey_campaign_runs_definition_snapshot_object
        CHECK (jsonb_typeof(definition_snapshot) = 'object'),
    CONSTRAINT chk_survey_campaign_runs_counts
        CHECK (evaluated_count >= 0 AND eligible_count >= 0 AND invitation_count >= 0 AND redacted_response_count >= 0),
    CONSTRAINT chk_survey_campaign_runs_failure_reason_length
        CHECK (length(failure_reason) <= 240),
    CONSTRAINT chk_survey_campaign_runs_claimed_by_length
        CHECK (length(claimed_by) <= 256)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_survey_campaign_runs_one_nonterminal
    ON survey_campaign_runs (tenant_id, campaign_id)
    WHERE status IN ('scheduled', 'evaluating', 'collecting');
CREATE INDEX IF NOT EXISTS idx_survey_campaign_runs_due
    ON survey_campaign_runs (scheduled_at ASC, created_at ASC, id ASC)
    WHERE status = 'scheduled';

CREATE OR REPLACE FUNCTION update_survey_campaign_runs_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_survey_campaign_runs_updated_at
    BEFORE UPDATE ON survey_campaign_runs
    FOR EACH ROW
    EXECUTE FUNCTION update_survey_campaign_runs_updated_at();

ALTER TABLE survey_invitations
    ADD COLUMN IF NOT EXISTS run_id UUID;
ALTER TABLE survey_invitations
    ADD CONSTRAINT fk_survey_invitations_run
        FOREIGN KEY (tenant_id, run_id, campaign_id)
        REFERENCES survey_campaign_runs(tenant_id, id, campaign_id)
        ON DELETE CASCADE;
CREATE UNIQUE INDEX IF NOT EXISTS uq_survey_invitations_run_contact
    ON survey_invitations (tenant_id, run_id, contact_id)
    WHERE run_id IS NOT NULL AND contact_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_survey_invitations_run_created
    ON survey_invitations (tenant_id, run_id, created_at DESC, id DESC)
    WHERE run_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_survey_invitations_contact_cooldown
    ON survey_invitations (tenant_id, contact_id, created_at DESC)
    WHERE contact_id IS NOT NULL AND suppression_status = 'not_suppressed';

ALTER TABLE survey_responses
    ADD COLUMN IF NOT EXISTS survey_type TEXT,
    ADD COLUMN IF NOT EXISTS nps_bucket TEXT NOT NULL DEFAULT '';

UPDATE survey_responses sr
SET survey_type = sc.survey_type
FROM survey_campaigns sc
WHERE sc.tenant_id = sr.tenant_id
  AND sc.id = sr.campaign_id
  AND sr.survey_type IS NULL;

ALTER TABLE survey_responses
    ALTER COLUMN survey_type SET NOT NULL,
    DROP CONSTRAINT IF EXISTS chk_survey_responses_score;
ALTER TABLE survey_responses
    ADD CONSTRAINT chk_survey_responses_score_by_type
        CHECK (
            (survey_type = 'csat' AND score BETWEEN 1 AND 5)
            OR (survey_type = 'ces' AND score BETWEEN 1 AND 7)
            OR (survey_type = 'nps' AND score BETWEEN 0 AND 10)
        ),
    ADD CONSTRAINT chk_survey_responses_nps_bucket
        CHECK (
            (survey_type = 'nps' AND nps_bucket IN ('detractor', 'passive', 'promoter'))
            OR (survey_type IN ('csat', 'ces') AND nps_bucket = '')
        );
CREATE INDEX IF NOT EXISTS idx_survey_responses_nps_bucket
    ON survey_responses (tenant_id, campaign_id, nps_bucket, submitted_at DESC)
    WHERE survey_type = 'nps';

CREATE TABLE IF NOT EXISTS survey_response_feedback_links (
    response_id  UUID PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    feedback_id  BIGINT NOT NULL REFERENCES user_feedback(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_survey_response_feedback_links_tenant_response UNIQUE (tenant_id, response_id),
    CONSTRAINT uq_survey_response_feedback_links_tenant_feedback UNIQUE (tenant_id, feedback_id),
    CONSTRAINT fk_survey_response_feedback_links_response
        FOREIGN KEY (tenant_id, response_id)
        REFERENCES survey_responses(tenant_id, id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_survey_response_feedback_links_feedback
    ON survey_response_feedback_links (tenant_id, feedback_id);

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
        'survey.nps_run_cancel',
        'survey.nps_run_schedule',
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
