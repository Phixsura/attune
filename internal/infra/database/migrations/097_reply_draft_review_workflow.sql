-- Reply draft review, revision, and controlled-send workflow (#164).

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS reply_send_hooks (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name              TEXT NOT NULL DEFAULT 'Default reply send hook',
    url_ciphertext    BYTEA NOT NULL,
    url_key_id        TEXT NOT NULL DEFAULT '',
    url_fingerprint   TEXT NOT NULL DEFAULT '',
    url_host          TEXT NOT NULL DEFAULT '',
    secret_ciphertext BYTEA,
    secret_key_id     TEXT NOT NULL DEFAULT '',
    enabled           BOOLEAN NOT NULL DEFAULT TRUE,
    created_by        TEXT NOT NULL DEFAULT '',
    updated_by        TEXT NOT NULL DEFAULT '',
    disabled_at       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (length(name) BETWEEN 1 AND 120)
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_reply_send_hooks_tenant_active
    ON reply_send_hooks (tenant_id)
    WHERE disabled_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_reply_send_hooks_tenant_enabled
    ON reply_send_hooks (tenant_id, enabled, updated_at DESC);

CREATE TABLE IF NOT EXISTS reply_drafts (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id                TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    feedback_id              BIGINT NOT NULL REFERENCES user_feedback(id) ON DELETE CASCADE,
    cycle_no                 INTEGER NOT NULL DEFAULT 1 CHECK (cycle_no >= 1),
    status                   TEXT NOT NULL DEFAULT 'suggested',
    active_revision_id       UUID,
    approved_revision_id     UUID,
    sent_revision_id         UUID,
    approved_hook_id         UUID REFERENCES reply_send_hooks(id) ON DELETE SET NULL,
    approved_hook_fingerprint TEXT NOT NULL DEFAULT '',
    sent_hook_id             UUID REFERENCES reply_send_hooks(id) ON DELETE SET NULL,
    source_fingerprint       TEXT NOT NULL DEFAULT '',
    source_meta              JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_blocker             TEXT NOT NULL DEFAULT '',
    external_delivery_status TEXT NOT NULL DEFAULT '',
    external_message_id      TEXT NOT NULL DEFAULT '',
    generated_at             TIMESTAMPTZ,
    generated_by             TEXT NOT NULL DEFAULT '',
    edited_at                TIMESTAMPTZ,
    edited_by                TEXT NOT NULL DEFAULT '',
    approved_at              TIMESTAMPTZ,
    approved_by              TEXT NOT NULL DEFAULT '',
    rejected_at              TIMESTAMPTZ,
    rejected_by              TEXT NOT NULL DEFAULT '',
    sent_at                  TIMESTAMPTZ,
    sent_by                  TEXT NOT NULL DEFAULT '',
    archived_at              TIMESTAMPTZ,
    revision                 BIGINT NOT NULL DEFAULT 1 CHECK (revision >= 1),
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, feedback_id, cycle_no),
    CHECK (status IN ('suggested', 'edited', 'approved', 'send_pending', 'send_failed', 'rejected', 'sent', 'stale')),
    CHECK (external_delivery_status IN ('', 'accepted', 'failed'))
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_reply_drafts_active_feedback
    ON reply_drafts (tenant_id, feedback_id)
    WHERE archived_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_reply_drafts_tenant_status
    ON reply_drafts (tenant_id, status, updated_at DESC)
    WHERE archived_at IS NULL;

CREATE TABLE IF NOT EXISTS reply_draft_revisions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    draft_id           UUID NOT NULL REFERENCES reply_drafts(id) ON DELETE CASCADE,
    tenant_id          TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    feedback_id        BIGINT NOT NULL REFERENCES user_feedback(id) ON DELETE CASCADE,
    cycle_no           INTEGER NOT NULL CHECK (cycle_no >= 1),
    revision_no        INTEGER NOT NULL CHECK (revision_no >= 1),
    origin             TEXT NOT NULL,
    content            TEXT NOT NULL,
    content_sha256     BYTEA NOT NULL,
    source_fingerprint TEXT NOT NULL DEFAULT '',
    metadata           JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by         TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (draft_id, revision_no),
    CHECK (origin IN ('ai', 'human', 'system')),
    CHECK (length(content) > 0)
);

CREATE INDEX IF NOT EXISTS idx_reply_draft_revisions_feedback
    ON reply_draft_revisions (tenant_id, feedback_id, cycle_no, revision_no DESC);

CREATE TABLE IF NOT EXISTS reply_draft_events (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    draft_id            UUID NOT NULL REFERENCES reply_drafts(id) ON DELETE CASCADE,
    tenant_id           TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    feedback_id         BIGINT NOT NULL REFERENCES user_feedback(id) ON DELETE CASCADE,
    revision_id         UUID REFERENCES reply_draft_revisions(id) ON DELETE SET NULL,
    hook_id             UUID REFERENCES reply_send_hooks(id) ON DELETE SET NULL,
    event_type          TEXT NOT NULL,
    actor_type          TEXT NOT NULL DEFAULT '',
    actor_id            TEXT NOT NULL DEFAULT '',
    blocker             TEXT NOT NULL DEFAULT '',
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (event_type IN (
        'generate',
        'generate_blocked',
        'edit',
        'approve',
        'reject',
        'stale',
        'send_request',
        'send_success',
        'send_failure'
    ))
);

CREATE INDEX IF NOT EXISTS idx_reply_draft_events_feedback
    ON reply_draft_events (tenant_id, feedback_id, created_at DESC);

CREATE TABLE IF NOT EXISTS reply_delivery_attempts (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    draft_id            UUID REFERENCES reply_drafts(id) ON DELETE CASCADE,
    feedback_id         BIGINT REFERENCES user_feedback(id) ON DELETE CASCADE,
    hook_id             UUID NOT NULL REFERENCES reply_send_hooks(id) ON DELETE RESTRICT,
    revision_id         UUID REFERENCES reply_draft_revisions(id) ON DELETE RESTRICT,
    event_type          TEXT NOT NULL DEFAULT 'reply.send',
    idempotency_key     TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'pending',
    http_status         INTEGER,
    attempts            INTEGER NOT NULL DEFAULT 1 CHECK (attempts >= 0),
    max_attempts        INTEGER NOT NULL DEFAULT 8 CHECK (max_attempts >= 1),
    next_retry_at       TIMESTAMPTZ,
    request_fingerprint TEXT NOT NULL DEFAULT '',
    external_message_id TEXT NOT NULL DEFAULT '',
    error               TEXT NOT NULL DEFAULT '',
    response_meta       JSONB NOT NULL DEFAULT '{}'::jsonb,
    requested_by_type   TEXT NOT NULL DEFAULT '',
    requested_by        TEXT NOT NULL DEFAULT '',
    requested_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, draft_id, idempotency_key),
    CHECK (length(idempotency_key) BETWEEN 8 AND 128),
    CHECK (event_type IN ('reply.send', 'reply.test')),
    CHECK (
        event_type = 'reply.test'
        OR (draft_id IS NOT NULL AND feedback_id IS NOT NULL AND revision_id IS NOT NULL)
    ),
    CHECK (status IN ('pending', 'accepted', 'failed', 'dead'))
);

CREATE INDEX IF NOT EXISTS idx_reply_delivery_attempts_feedback
    ON reply_delivery_attempts (tenant_id, feedback_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_reply_delivery_attempts_tenant_recent
    ON reply_delivery_attempts (tenant_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS ux_reply_delivery_attempts_test_idempotency
    ON reply_delivery_attempts (tenant_id, idempotency_key)
    WHERE event_type = 'reply.test';

CREATE INDEX IF NOT EXISTS idx_reply_delivery_attempts_retry
    ON reply_delivery_attempts (tenant_id, status, next_retry_at)
    WHERE status IN ('failed', 'dead');

CREATE INDEX IF NOT EXISTS idx_reply_delivery_attempts_due_retry
    ON reply_delivery_attempts (status, next_retry_at, created_at)
    WHERE event_type = 'reply.send' AND status = 'failed' AND next_retry_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_reply_delivery_attempts_pending_stale
    ON reply_delivery_attempts (updated_at)
    WHERE status = 'pending';

WITH seeded_drafts AS (
    INSERT INTO reply_drafts (
        tenant_id,
        feedback_id,
        cycle_no,
        status,
        source_fingerprint,
        source_meta,
        generated_at,
        generated_by,
        created_at,
        updated_at
    )
    SELECT
        f.tenant_id,
        f.id,
        1,
        'suggested',
        encode(digest(
            concat_ws('|',
                COALESCE(f.content, ''),
                COALESCE(f.source, ''),
                COALESCE(f.user_id, ''),
                COALESCE(f.source_meta::text, ''),
                COALESCE(f.enriched_title, ''),
                COALESCE(f.enriched_rationale, ''),
                COALESCE(f.enriched_attrs::text, ''),
                COALESCE(f.language, ''),
                COALESCE(f.enrichment_status, '')
            ),
            'sha256'
        ), 'hex'),
        jsonb_build_object(
            'source', 'legacy_user_feedback',
            'feedback_source', f.source,
            'language', COALESCE(f.language, ''),
            'enrichment_status', f.enrichment_status
        ),
        f.reply_draft_generated_at,
        'legacy-backfill',
        COALESCE(f.reply_draft_generated_at, NOW()),
        COALESCE(f.reply_draft_generated_at, NOW())
    FROM user_feedback f
    WHERE COALESCE(f.reply_draft, '') <> ''
    ON CONFLICT (tenant_id, feedback_id, cycle_no) DO NOTHING
    RETURNING id
),
seeded_revisions AS (
    INSERT INTO reply_draft_revisions (
        draft_id,
        tenant_id,
        feedback_id,
        cycle_no,
        revision_no,
        origin,
        content,
        content_sha256,
        source_fingerprint,
        metadata,
        created_by,
        created_at
    )
    SELECT
        d.id,
        d.tenant_id,
        d.feedback_id,
        d.cycle_no,
        1,
        'ai',
        f.reply_draft,
        digest(f.reply_draft, 'sha256'),
        d.source_fingerprint,
        d.source_meta,
        'legacy-backfill',
        COALESCE(f.reply_draft_generated_at, NOW())
    FROM reply_drafts d
    JOIN user_feedback f
      ON f.tenant_id = d.tenant_id
     AND f.id = d.feedback_id
    WHERE COALESCE(f.reply_draft, '') <> ''
      AND d.cycle_no = 1
      AND d.archived_at IS NULL
    ON CONFLICT (draft_id, revision_no) DO NOTHING
    RETURNING id
)
UPDATE reply_drafts d
SET active_revision_id = r.id,
    revision = GREATEST(d.revision, 1),
    updated_at = GREATEST(d.updated_at, r.created_at)
FROM reply_draft_revisions r
WHERE r.draft_id = d.id
  AND r.revision_no = 1
  AND d.active_revision_id IS NULL;

INSERT INTO reply_draft_events (
    draft_id,
    tenant_id,
    feedback_id,
    revision_id,
    event_type,
    actor_type,
    actor_id,
    metadata,
    created_at
)
SELECT
    d.id,
    d.tenant_id,
    d.feedback_id,
    d.active_revision_id,
    'generate',
    'system',
    'legacy-backfill',
    jsonb_build_object('source', 'user_feedback.reply_draft'),
    COALESCE(d.generated_at, d.created_at)
FROM reply_drafts d
WHERE d.generated_by = 'legacy-backfill'
  AND d.active_revision_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1
      FROM reply_draft_events e
      WHERE e.draft_id = d.id
        AND e.event_type = 'generate'
        AND e.actor_id = 'legacy-backfill'
  );

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
