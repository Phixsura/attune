-- SPDX-License-Identifier: Apache-2.0
--
-- Close-the-loop request notifications (#224).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS customer_notification_contacts (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    subject_key           TEXT NOT NULL DEFAULT '',
    subject_hash          TEXT NOT NULL DEFAULT '',
    display_name          TEXT NOT NULL DEFAULT '',
    organization          TEXT NOT NULL DEFAULT '',
    email_hash            TEXT NOT NULL,
    email_payload         BYTEA NOT NULL,
    email_verified_at     TIMESTAMPTZ,
    consent_state         TEXT NOT NULL DEFAULT 'unknown',
    consent_source        TEXT NOT NULL DEFAULT '',
    consent_text_version  TEXT NOT NULL DEFAULT '',
    legal_basis           TEXT NOT NULL DEFAULT '',
    consented_at          TIMESTAMPTZ,
    locale                TEXT NOT NULL DEFAULT '',
    timezone              TEXT NOT NULL DEFAULT '',
    bounced_at            TIMESTAMPTZ,
    complained_at         TIMESTAMPTZ,
    suppressed_at         TIMESTAMPTZ,
    suppression_reason    TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_customer_notification_contacts_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT uq_customer_notification_contacts_email_hash UNIQUE (tenant_id, email_hash),
    CONSTRAINT chk_customer_notification_contacts_consent_state
        CHECK (consent_state IN ('unknown', 'opted_in', 'opted_out', 'suppressed')),
    CONSTRAINT chk_customer_notification_contacts_email_hash_length
        CHECK (length(email_hash) BETWEEN 32 AND 128),
    CONSTRAINT chk_customer_notification_contacts_subject_key_length CHECK (length(subject_key) <= 512),
    CONSTRAINT chk_customer_notification_contacts_subject_hash_length CHECK (length(subject_hash) <= 128),
    CONSTRAINT chk_customer_notification_contacts_display_name_length CHECK (length(display_name) <= 200),
    CONSTRAINT chk_customer_notification_contacts_organization_length CHECK (length(organization) <= 200)
);

CREATE INDEX IF NOT EXISTS idx_customer_notification_contacts_subject
    ON customer_notification_contacts (tenant_id, subject_hash, subject_key)
    WHERE subject_hash <> '' OR subject_key <> '';
CREATE INDEX IF NOT EXISTS idx_customer_notification_contacts_suppression
    ON customer_notification_contacts (tenant_id, consent_state, suppressed_at, bounced_at, complained_at);

CREATE TABLE IF NOT EXISTS customer_notification_webhook_targets (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                        TEXT NOT NULL,
    url_payload                 BYTEA NOT NULL,
    url_host                    TEXT NOT NULL DEFAULT '',
    secret_payload              BYTEA,
    signature_version           TEXT NOT NULL DEFAULT 'v1-content-sha256',
    event_mask                  JSONB NOT NULL DEFAULT '{}'::jsonb,
    include_recipient_identity  BOOLEAN NOT NULL DEFAULT false,
    status                      TEXT NOT NULL DEFAULT 'active',
    verified_at                 TIMESTAMPTZ,
    last_tested_at              TIMESTAMPTZ,
    created_by                  TEXT NOT NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_customer_notification_webhook_targets_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT chk_customer_notification_webhook_targets_status
        CHECK (status IN ('active', 'disabled', 'suppressed')),
    CONSTRAINT chk_customer_notification_webhook_targets_name_length CHECK (length(name) BETWEEN 1 AND 120),
    CONSTRAINT chk_customer_notification_webhook_targets_event_mask_object CHECK (jsonb_typeof(event_mask) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_customer_notification_webhook_targets_active
    ON customer_notification_webhook_targets (tenant_id, status, updated_at DESC)
    WHERE status = 'active';

CREATE TABLE IF NOT EXISTS customer_request_subscriptions (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    request_id            UUID,
    account_key           TEXT NOT NULL DEFAULT '',
    contact_id            UUID NOT NULL,
    scope                 TEXT NOT NULL,
    source                TEXT NOT NULL,
    event_mask            JSONB NOT NULL DEFAULT '{}'::jsonb,
    status                TEXT NOT NULL DEFAULT 'active',
    unsubscribed_at       TIMESTAMPTZ,
    created_by            TEXT NOT NULL DEFAULT 'system',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_customer_request_subscriptions_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT fk_customer_request_subscriptions_contact
        FOREIGN KEY (tenant_id, contact_id)
        REFERENCES customer_notification_contacts(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT fk_customer_request_subscriptions_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_customer_request_subscriptions_scope
        CHECK (scope IN ('request', 'tenant_updates', 'changelog', 'account')),
    CONSTRAINT chk_customer_request_subscriptions_source
        CHECK (source IN (
            'submitter',
            'voter',
            'commenter',
            'follower',
            'linked_feedback_submitter',
            'account_follower',
            'manual'
        )),
    CONSTRAINT chk_customer_request_subscriptions_status
        CHECK (status IN ('active', 'unsubscribed', 'suppressed')),
    CONSTRAINT chk_customer_request_subscriptions_shape
        CHECK (
            (scope = 'request' AND request_id IS NOT NULL AND account_key = '') OR
            (scope IN ('tenant_updates', 'changelog') AND request_id IS NULL AND account_key = '') OR
            (scope = 'account' AND request_id IS NULL AND account_key <> '')
        ),
    CONSTRAINT chk_customer_request_subscriptions_event_mask_object CHECK (jsonb_typeof(event_mask) = 'object')
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_request_subscriptions_request
    ON customer_request_subscriptions (tenant_id, request_id, contact_id, source)
    WHERE request_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_request_subscriptions_tenant_scope
    ON customer_request_subscriptions (tenant_id, scope, contact_id, source)
    WHERE request_id IS NULL AND account_key = '';
CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_request_subscriptions_account
    ON customer_request_subscriptions (tenant_id, account_key, contact_id, source)
    WHERE request_id IS NULL AND account_key <> '';
CREATE INDEX IF NOT EXISTS idx_customer_request_subscriptions_active_request
    ON customer_request_subscriptions (tenant_id, request_id, status, contact_id)
    WHERE request_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS public_update_threads (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    surface               TEXT NOT NULL,
    slug                  TEXT NOT NULL DEFAULT '',
    state                 TEXT NOT NULL DEFAULT 'draft',
    public_url            TEXT NOT NULL DEFAULT '',
    created_by            TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_public_update_threads_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT chk_public_update_threads_surface CHECK (surface IN ('request_update', 'changelog_post')),
    CONSTRAINT chk_public_update_threads_state CHECK (state IN ('draft', 'published', 'archived')),
    CONSTRAINT chk_public_update_threads_slug_length CHECK (length(slug) <= 160),
    CONSTRAINT chk_public_update_threads_url_length CHECK (length(public_url) <= 2048)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_public_update_threads_slug
    ON public_update_threads (tenant_id, slug)
    WHERE slug <> '';

CREATE TABLE IF NOT EXISTS public_update_posts (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    thread_id             UUID NOT NULL,
    kind                  TEXT NOT NULL,
    state                 TEXT NOT NULL DEFAULT 'draft',
    title                 TEXT NOT NULL,
    body                  TEXT NOT NULL,
    locale                TEXT NOT NULL DEFAULT '',
    segment_filter        JSONB NOT NULL DEFAULT '{}'::jsonb,
    visibility            TEXT NOT NULL DEFAULT 'public',
    notify_subscribers    BOOLEAN NOT NULL DEFAULT false,
    content_version       INT NOT NULL DEFAULT 1,
    content_hash          TEXT NOT NULL DEFAULT '',
    supersedes_post_id    UUID,
    published_by          TEXT NOT NULL DEFAULT '',
    published_at          TIMESTAMPTZ,
    created_by            TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_public_update_posts_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT uq_public_update_posts_thread_version UNIQUE (tenant_id, thread_id, content_version),
    CONSTRAINT fk_public_update_posts_thread
        FOREIGN KEY (tenant_id, thread_id)
        REFERENCES public_update_threads(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_public_update_posts_supersedes
        FOREIGN KEY (tenant_id, supersedes_post_id)
        REFERENCES public_update_posts(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_public_update_posts_kind
        CHECK (kind IN ('status_change', 'shipped', 'moderator_response', 'changelog_post')),
    CONSTRAINT chk_public_update_posts_state CHECK (state IN ('draft', 'published', 'archived')),
    CONSTRAINT chk_public_update_posts_visibility CHECK (visibility = 'public'),
    CONSTRAINT chk_public_update_posts_version CHECK (content_version > 0),
    CONSTRAINT chk_public_update_posts_title_length CHECK (length(title) BETWEEN 1 AND 200),
    CONSTRAINT chk_public_update_posts_body_length CHECK (length(body) BETWEEN 1 AND 20000),
    CONSTRAINT chk_public_update_posts_segment_filter_object CHECK (jsonb_typeof(segment_filter) = 'object'),
    CONSTRAINT chk_public_update_posts_published_fields
        CHECK (
            (state = 'published' AND published_at IS NOT NULL AND published_by <> '') OR
            (state <> 'published')
        )
);

CREATE INDEX IF NOT EXISTS idx_public_update_posts_tenant_thread
    ON public_update_posts (tenant_id, thread_id, content_version DESC);
CREATE INDEX IF NOT EXISTS idx_public_update_posts_published
    ON public_update_posts (tenant_id, published_at DESC, id DESC)
    WHERE state = 'published';

CREATE TABLE IF NOT EXISTS public_update_request_links (
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    update_id             UUID NOT NULL,
    request_id            UUID NOT NULL,
    role                  TEXT NOT NULL DEFAULT 'primary',
    old_status            TEXT NOT NULL DEFAULT '',
    new_status            TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, update_id, request_id),
    CONSTRAINT fk_public_update_request_links_update
        FOREIGN KEY (tenant_id, update_id)
        REFERENCES public_update_posts(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_public_update_request_links_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_public_update_request_links_role CHECK (role IN ('primary', 'related'))
);

CREATE INDEX IF NOT EXISTS idx_public_update_request_links_request
    ON public_update_request_links (tenant_id, request_id, created_at DESC);

CREATE TABLE IF NOT EXISTS request_direct_followups (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    request_id            UUID,
    feedback_id           BIGINT,
    subscription_id       UUID,
    contact_id            UUID NOT NULL,
    kind                  TEXT NOT NULL,
    body                  TEXT NOT NULL,
    state                 TEXT NOT NULL DEFAULT 'draft',
    created_by            TEXT NOT NULL,
    sent_at               TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_request_direct_followups_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT fk_request_direct_followups_contact
        FOREIGN KEY (tenant_id, contact_id)
        REFERENCES customer_notification_contacts(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_request_direct_followups_subscription
        FOREIGN KEY (tenant_id, subscription_id)
        REFERENCES customer_request_subscriptions(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_request_direct_followups_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_request_direct_followups_feedback
        FOREIGN KEY (tenant_id, feedback_id)
        REFERENCES user_feedback(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_request_direct_followups_kind CHECK (kind IN ('need_info', 'moderator_response')),
    CONSTRAINT chk_request_direct_followups_state CHECK (state IN ('draft', 'sent', 'archived')),
    CONSTRAINT chk_request_direct_followups_anchor
        CHECK (
            (request_id IS NOT NULL OR feedback_id IS NOT NULL) AND
            (feedback_id IS NOT NULL OR subscription_id IS NOT NULL)
        ),
    CONSTRAINT chk_request_direct_followups_body_length CHECK (length(body) BETWEEN 1 AND 20000)
);

CREATE TABLE IF NOT EXISTS customer_request_notification_events (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    primary_request_id    UUID,
    update_id             UUID,
    direct_followup_id    UUID,
    event_type            TEXT NOT NULL,
    audience_scope        TEXT NOT NULL,
    dedupe_key            TEXT NOT NULL,
    old_status            TEXT NOT NULL DEFAULT '',
    new_status            TEXT NOT NULL DEFAULT '',
    actor_type            TEXT NOT NULL DEFAULT 'system',
    actor_id              TEXT NOT NULL DEFAULT 'system',
    status                TEXT NOT NULL DEFAULT 'pending',
    attempts              SMALLINT NOT NULL DEFAULT 0,
    next_retry_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_at            TIMESTAMPTZ,
    claimed_by            TEXT NOT NULL DEFAULT '',
    resolved_at           TIMESTAMPTZ,
    last_error            TEXT NOT NULL DEFAULT '',
    recipient_snapshot    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_customer_request_notification_events_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT uq_customer_request_notification_events_dedupe UNIQUE (tenant_id, dedupe_key),
    CONSTRAINT fk_customer_request_notification_events_request
        FOREIGN KEY (tenant_id, primary_request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_customer_request_notification_events_update
        FOREIGN KEY (tenant_id, update_id)
        REFERENCES public_update_posts(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_customer_request_notification_events_followup
        FOREIGN KEY (tenant_id, direct_followup_id)
        REFERENCES request_direct_followups(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_customer_request_notification_events_type
        CHECK (event_type IN (
            'request.status_changed',
            'request.shipped',
            'request.need_info_direct',
            'request.moderator_response',
            'changelog.post_published'
        )),
    CONSTRAINT chk_customer_request_notification_events_scope
        CHECK (audience_scope IN ('public_broadcast', 'direct_followup')),
    CONSTRAINT chk_customer_request_notification_events_status
        CHECK (status IN ('pending', 'resolving', 'resolved', 'failed', 'dead')),
    CONSTRAINT chk_customer_request_notification_events_attempts CHECK (attempts >= 0),
    CONSTRAINT chk_customer_request_notification_events_snapshot_object CHECK (jsonb_typeof(recipient_snapshot) = 'object'),
    CONSTRAINT chk_customer_request_notification_events_source
        CHECK (
            (audience_scope = 'public_broadcast' AND update_id IS NOT NULL AND direct_followup_id IS NULL) OR
            (audience_scope = 'direct_followup' AND direct_followup_id IS NOT NULL AND update_id IS NULL)
        ),
    CONSTRAINT chk_customer_request_notification_events_type_scope
        CHECK (
            (event_type = 'request.need_info_direct' AND audience_scope = 'direct_followup') OR
            (event_type <> 'request.need_info_direct')
        )
);

CREATE INDEX IF NOT EXISTS idx_customer_request_notification_events_claim
    ON customer_request_notification_events (status, next_retry_at, created_at, id)
    WHERE status IN ('pending', 'failed');
CREATE INDEX IF NOT EXISTS idx_customer_request_notification_events_request
    ON customer_request_notification_events (tenant_id, primary_request_id, created_at DESC)
    WHERE primary_request_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS customer_request_notification_deliveries (
    id                    BIGSERIAL PRIMARY KEY,
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    event_id              UUID NOT NULL,
    subscription_id       UUID,
    contact_id            UUID,
    webhook_target_id     UUID,
    channel               TEXT NOT NULL,
    destination_hash      TEXT NOT NULL,
    payload               JSONB NOT NULL,
    sensitive_payload     BYTEA,
    status                TEXT NOT NULL DEFAULT 'pending',
    attempts              SMALLINT NOT NULL DEFAULT 0,
    failure_kind          TEXT NOT NULL DEFAULT '',
    http_status           INT,
    last_error            TEXT NOT NULL DEFAULT '',
    dead_reason           TEXT NOT NULL DEFAULT '',
    next_retry_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    trace_id              TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at          TIMESTAMPTZ,
    claimed_at            TIMESTAMPTZ,
    claimed_by            TEXT NOT NULL DEFAULT '',
    last_manual_retry_at  TIMESTAMPTZ,
    retried_by            TEXT NOT NULL DEFAULT '',
    manual_retry_count    INT NOT NULL DEFAULT 0,
    CONSTRAINT fk_customer_request_notification_deliveries_event
        FOREIGN KEY (tenant_id, event_id)
        REFERENCES customer_request_notification_events(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_customer_request_notification_deliveries_subscription
        FOREIGN KEY (tenant_id, subscription_id)
        REFERENCES customer_request_subscriptions(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_customer_request_notification_deliveries_contact
        FOREIGN KEY (tenant_id, contact_id)
        REFERENCES customer_notification_contacts(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT fk_customer_request_notification_deliveries_webhook_target
        FOREIGN KEY (tenant_id, webhook_target_id)
        REFERENCES customer_notification_webhook_targets(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_customer_request_notification_deliveries_channel CHECK (channel IN ('email', 'webhook')),
    CONSTRAINT chk_customer_request_notification_deliveries_status
        CHECK (status IN ('pending', 'delivered', 'failed', 'dead', 'suppressed')),
    CONSTRAINT chk_customer_request_notification_deliveries_attempts CHECK (attempts >= 0),
    CONSTRAINT chk_customer_request_notification_deliveries_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT chk_customer_request_notification_deliveries_destination
        CHECK (
            (channel = 'email' AND contact_id IS NOT NULL AND webhook_target_id IS NULL) OR
            (channel = 'webhook' AND webhook_target_id IS NOT NULL AND contact_id IS NULL)
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_request_notification_deliveries_contact
    ON customer_request_notification_deliveries (event_id, contact_id, channel)
    WHERE contact_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_customer_request_notification_deliveries_webhook
    ON customer_request_notification_deliveries (event_id, webhook_target_id, channel)
    WHERE webhook_target_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_customer_request_notification_deliveries_claim
    ON customer_request_notification_deliveries (status, next_retry_at, created_at, id)
    WHERE status IN ('pending', 'failed');
CREATE INDEX IF NOT EXISTS idx_customer_request_notification_deliveries_tenant_status
    ON customer_request_notification_deliveries (tenant_id, status, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS customer_request_unsubscribe_tokens (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    contact_id            UUID NOT NULL,
    request_id            UUID,
    scope                 TEXT NOT NULL,
    purpose               TEXT NOT NULL DEFAULT 'unsubscribe',
    token_version         TEXT NOT NULL DEFAULT 'v1',
    token_hash            TEXT NOT NULL UNIQUE,
    used_by_user_agent    TEXT NOT NULL DEFAULT '',
    expires_at            TIMESTAMPTZ,
    used_at               TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_customer_request_unsubscribe_tokens_contact
        FOREIGN KEY (tenant_id, contact_id)
        REFERENCES customer_notification_contacts(tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT fk_customer_request_unsubscribe_tokens_request
        FOREIGN KEY (tenant_id, request_id)
        REFERENCES customer_requests(tenant_id, id)
        ON DELETE RESTRICT,
    CONSTRAINT chk_customer_request_unsubscribe_tokens_scope
        CHECK (scope IN ('request', 'tenant', 'changelog')),
    CONSTRAINT chk_customer_request_unsubscribe_tokens_purpose
        CHECK (purpose IN ('unsubscribe', 'preferences')),
    CONSTRAINT chk_customer_request_unsubscribe_tokens_shape
        CHECK (
            (scope = 'request' AND request_id IS NOT NULL) OR
            (scope IN ('tenant', 'changelog') AND request_id IS NULL)
        )
);

CREATE INDEX IF NOT EXISTS idx_customer_request_unsubscribe_tokens_contact
    ON customer_request_unsubscribe_tokens (tenant_id, contact_id, scope, request_id);

CREATE TABLE IF NOT EXISTS customer_notification_settings (
    tenant_id                         TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    email_enabled                     BOOLEAN NOT NULL DEFAULT false,
    webhook_enabled                   BOOLEAN NOT NULL DEFAULT false,
    enabled_event_types               JSONB NOT NULL DEFAULT '{}'::jsonb,
    status_policy                     JSONB NOT NULL DEFAULT '{}'::jsonb,
    default_consent_mode              TEXT NOT NULL DEFAULT 'disabled',
    require_public_update_for_status  BOOLEAN NOT NULL DEFAULT true,
    max_recipients_without_confirm    INT NOT NULL DEFAULT 100,
    tenant_hourly_send_limit          INT NOT NULL DEFAULT 1000,
    contact_daily_send_limit          INT NOT NULL DEFAULT 10,
    updated_by                        TEXT NOT NULL DEFAULT '',
    created_at                        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_customer_notification_settings_consent_mode
        CHECK (default_consent_mode IN ('explicit_opt_in', 'existing_app_consent', 'disabled')),
    CONSTRAINT chk_customer_notification_settings_enabled_event_types_object CHECK (jsonb_typeof(enabled_event_types) = 'object'),
    CONSTRAINT chk_customer_notification_settings_status_policy_object CHECK (jsonb_typeof(status_policy) = 'object'),
    CONSTRAINT chk_customer_notification_settings_limits
        CHECK (
            max_recipients_without_confirm >= 0 AND
            tenant_hourly_send_limit >= 0 AND
            contact_daily_send_limit >= 0
        )
);

CREATE TABLE IF NOT EXISTS customer_notification_email_senders (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    from_name             TEXT NOT NULL,
    from_email_hash       TEXT NOT NULL,
    from_email_payload    BYTEA NOT NULL,
    reply_to_hash         TEXT NOT NULL DEFAULT '',
    reply_to_payload      BYTEA,
    domain                TEXT NOT NULL,
    dkim_status           TEXT NOT NULL DEFAULT 'pending',
    spf_status            TEXT NOT NULL DEFAULT 'pending',
    dmarc_status          TEXT NOT NULL DEFAULT 'pending',
    provider              TEXT NOT NULL DEFAULT '',
    provider_config       BYTEA,
    status                TEXT NOT NULL DEFAULT 'pending',
    verified_at           TIMESTAMPTZ,
    created_by            TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_customer_notification_email_senders_tenant_id UNIQUE (tenant_id, id),
    CONSTRAINT uq_customer_notification_email_senders_from UNIQUE (tenant_id, from_email_hash),
    CONSTRAINT chk_customer_notification_email_senders_dkim CHECK (dkim_status IN ('pending', 'verified', 'failed')),
    CONSTRAINT chk_customer_notification_email_senders_spf CHECK (spf_status IN ('pending', 'verified', 'failed')),
    CONSTRAINT chk_customer_notification_email_senders_dmarc CHECK (dmarc_status IN ('pending', 'verified', 'failed')),
    CONSTRAINT chk_customer_notification_email_senders_status CHECK (status IN ('pending', 'active', 'disabled', 'failed')),
    CONSTRAINT chk_customer_notification_email_senders_name_length CHECK (length(from_name) BETWEEN 1 AND 120),
    CONSTRAINT chk_customer_notification_email_senders_domain_length CHECK (length(domain) BETWEEN 1 AND 255)
);

CREATE INDEX IF NOT EXISTS idx_customer_notification_email_senders_active
    ON customer_notification_email_senders (tenant_id, status, verified_at DESC)
    WHERE status = 'active';

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
        'customer_request.add_comment',
        'customer_request.delete_note',
        'customer_request.merge',
        'customer_request.link_issue',
        'customer_request.unlink_issue',
        'customer_request.record_issue_sync',
        'customer_request.update_scoring_settings',
        'public_policy.update',
        'portal_submission.create',
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
        'request_notification.settings_update',
        'request_notification.sender_verify',
        'request_notification.webhook_target_create',
        'request_notification.webhook_target_update',
        'request_notification.webhook_target_delete',
        'request_notification.webhook_target_test',
        'request_notification.subscribe',
        'request_notification.unsubscribe',
        'request_notification.suppress_contact',
        'request_notification.bounce',
        'request_notification.complaint',
        'request_notification.event_create',
        'request_notification.delivery_retry',
        'request_notification.delivery_dead',
        'request_notification.public_update_publish',
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
