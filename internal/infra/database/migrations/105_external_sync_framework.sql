-- SPDX-License-Identifier: Apache-2.0
--
-- Generic tenant-scoped external sync framework (#214).

CREATE TABLE IF NOT EXISTS external_connections (
    id                    UUID        PRIMARY KEY,
    tenant_id             TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider              TEXT        NOT NULL,
    name                  TEXT        NOT NULL,
    enabled               BOOLEAN     NOT NULL DEFAULT TRUE,
    status                TEXT        NOT NULL DEFAULT 'active',
    auth_type             TEXT        NOT NULL,
    base_url              TEXT        NOT NULL DEFAULT '',
    provider_config       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    scopes                TEXT[]      NOT NULL DEFAULT '{}',
    credential_key_id     TEXT        NOT NULL REFERENCES secret_key_registry(key_id),
    credential_ciphertext BYTEA       NOT NULL,
    webhook_secret_key_id TEXT        REFERENCES secret_key_registry(key_id),
    webhook_secret_ciphertext BYTEA,
    webhook_secret_set_at TIMESTAMPTZ,
    last_tested_at        TIMESTAMPTZ,
    last_test_status      TEXT        NOT NULL DEFAULT 'untested',
    last_error            TEXT        NOT NULL DEFAULT '',
    created_by            TEXT        NOT NULL,
    updated_by            TEXT        NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at            TIMESTAMPTZ,

    CONSTRAINT chk_external_connections_provider_shape CHECK (provider ~ '^[a-z0-9_-]+$'),
    CONSTRAINT chk_external_connections_name_not_empty CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    CONSTRAINT chk_external_connections_status CHECK (status IN ('active', 'disabled', 'draining', 'quarantined', 'deleted')),
    CONSTRAINT chk_external_connections_auth_type CHECK (auth_type IN ('api_key', 'token', 'oauth', 'basic')),
    CONSTRAINT chk_external_connections_last_test_status CHECK (last_test_status IN ('untested', 'ok', 'failed')),
    CONSTRAINT chk_external_connections_provider_config_object CHECK (jsonb_typeof(provider_config) = 'object'),
    CONSTRAINT chk_external_connections_last_error_size CHECK (length(last_error) <= 2000),
    CONSTRAINT chk_external_connections_deleted_status CHECK ((deleted_at IS NULL) OR status = 'deleted'),
    CONSTRAINT chk_external_connections_webhook_secret_pair CHECK (
        (webhook_secret_key_id IS NULL AND webhook_secret_ciphertext IS NULL AND webhook_secret_set_at IS NULL)
        OR
        (webhook_secret_key_id IS NOT NULL AND webhook_secret_ciphertext IS NOT NULL AND webhook_secret_set_at IS NOT NULL)
    )
);

ALTER TABLE external_connections
    ADD COLUMN IF NOT EXISTS webhook_secret_key_id TEXT REFERENCES secret_key_registry(key_id),
    ADD COLUMN IF NOT EXISTS webhook_secret_ciphertext BYTEA,
    ADD COLUMN IF NOT EXISTS webhook_secret_set_at TIMESTAMPTZ;

ALTER TABLE external_connections DROP CONSTRAINT IF EXISTS chk_external_connections_webhook_secret_pair;
ALTER TABLE external_connections ADD CONSTRAINT chk_external_connections_webhook_secret_pair CHECK (
    (webhook_secret_key_id IS NULL AND webhook_secret_ciphertext IS NULL AND webhook_secret_set_at IS NULL)
    OR
    (webhook_secret_key_id IS NOT NULL AND webhook_secret_ciphertext IS NOT NULL AND webhook_secret_set_at IS NOT NULL)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_external_connections_active_name
    ON external_connections (tenant_id, provider, lower(name))
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_external_connections_tenant_enabled
    ON external_connections (tenant_id, provider, enabled, status)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS external_object_mappings (
    id                   UUID        PRIMARY KEY,
    tenant_id            TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    connection_id        UUID        NOT NULL REFERENCES external_connections(id) ON DELETE CASCADE,
    local_object_type    TEXT        NOT NULL,
    external_object_type TEXT        NOT NULL,
    direction            TEXT        NOT NULL,
    field_mapping        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    status_mapping       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    conflict_policy      TEXT        NOT NULL DEFAULT 'manual',
    tombstone_policy     TEXT        NOT NULL DEFAULT 'mark_stale',
    enabled              BOOLEAN     NOT NULL DEFAULT TRUE,
    mapping_version      INT         NOT NULL DEFAULT 1,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_external_object_mappings_local_type CHECK (local_object_type IN ('customer_request')),
    CONSTRAINT chk_external_object_mappings_external_type CHECK (external_object_type IN ('issue')),
    CONSTRAINT chk_external_object_mappings_direction CHECK (direction IN ('pull', 'push', 'bidirectional')),
    CONSTRAINT chk_external_object_mappings_conflict_policy CHECK (conflict_policy IN ('manual', 'local_wins', 'external_wins', 'latest_update_wins')),
    CONSTRAINT chk_external_object_mappings_tombstone_policy CHECK (tombstone_policy IN ('ignore', 'mark_stale', 'unlink', 'archive_local')),
    CONSTRAINT chk_external_object_mappings_field_mapping_object CHECK (jsonb_typeof(field_mapping) = 'object'),
    CONSTRAINT chk_external_object_mappings_status_mapping_object CHECK (jsonb_typeof(status_mapping) = 'object'),
    CONSTRAINT chk_external_object_mappings_version CHECK (mapping_version >= 1)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_external_object_mappings_active_unique
    ON external_object_mappings (tenant_id, connection_id, local_object_type, external_object_type)
    WHERE enabled;
CREATE INDEX IF NOT EXISTS idx_external_object_mappings_tenant_connection
    ON external_object_mappings (tenant_id, connection_id, enabled);
CREATE INDEX IF NOT EXISTS idx_external_object_mappings_object_type
    ON external_object_mappings (tenant_id, local_object_type, external_object_type);

CREATE TABLE IF NOT EXISTS external_object_links (
    id                   UUID        PRIMARY KEY,
    tenant_id            TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mapping_id           UUID        NOT NULL REFERENCES external_object_mappings(id) ON DELETE CASCADE,
    local_object_type    TEXT        NOT NULL,
    local_object_id      TEXT        NOT NULL,
    external_object_type TEXT        NOT NULL,
    external_key         TEXT        NOT NULL,
    external_url         TEXT        NOT NULL DEFAULT '',
    external_version     TEXT        NOT NULL DEFAULT '',
    external_updated_at  TIMESTAMPTZ,
    local_updated_at     TIMESTAMPTZ,
    sync_state           TEXT        NOT NULL DEFAULT 'pending',
    sync_error           TEXT        NOT NULL DEFAULT '',
    last_synced_at       TIMESTAMPTZ,
    external_deleted_at  TIMESTAMPTZ,
    local_deleted_at     TIMESTAMPTZ,
    tombstone_reason     TEXT        NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_external_object_links_local_type CHECK (local_object_type IN ('customer_request')),
    CONSTRAINT chk_external_object_links_external_type CHECK (external_object_type IN ('issue')),
    CONSTRAINT chk_external_object_links_sync_state CHECK (sync_state IN ('pending', 'synced', 'stale', 'failed', 'conflict', 'deleted')),
    CONSTRAINT chk_external_object_links_external_key_not_empty CHECK (length(btrim(external_key)) BETWEEN 1 AND 512),
    CONSTRAINT chk_external_object_links_sync_error_size CHECK (length(sync_error) <= 2000),
    CONSTRAINT chk_external_object_links_tombstone_reason_size CHECK (length(tombstone_reason) <= 512)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_external_object_links_local_unique
    ON external_object_links (tenant_id, mapping_id, local_object_type, local_object_id)
    WHERE local_deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_external_object_links_external_unique
    ON external_object_links (tenant_id, mapping_id, external_object_type, external_key)
    WHERE external_deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_external_object_links_sync_health
    ON external_object_links (tenant_id, sync_state, last_synced_at);

ALTER TABLE customer_request_issue_links
    ADD COLUMN IF NOT EXISTS external_object_link_id UUID REFERENCES external_object_links(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_customer_request_issue_links_external_object_link
    ON customer_request_issue_links (tenant_id, external_object_link_id)
    WHERE external_object_link_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS external_sync_cursors (
    tenant_id              TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mapping_id             UUID        NOT NULL REFERENCES external_object_mappings(id) ON DELETE CASCADE,
    stream_key             TEXT        NOT NULL,
    cursor                 JSONB       NOT NULL DEFAULT '{}'::jsonb,
    high_watermark         TIMESTAMPTZ,
    last_successful_run_id UUID,
    reset_requested_at     TIMESTAMPTZ,
    reset_requested_by     TEXT        NOT NULL DEFAULT '',
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (tenant_id, mapping_id, stream_key),
    CONSTRAINT chk_external_sync_cursors_stream_key CHECK (length(btrim(stream_key)) BETWEEN 1 AND 200),
    CONSTRAINT chk_external_sync_cursors_cursor_object CHECK (jsonb_typeof(cursor) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_external_sync_cursors_reset
    ON external_sync_cursors (tenant_id, reset_requested_at)
    WHERE reset_requested_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS external_sync_runs (
    id                UUID        PRIMARY KEY,
    tenant_id         TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    connection_id     UUID        NOT NULL REFERENCES external_connections(id) ON DELETE CASCADE,
    mapping_id        UUID        REFERENCES external_object_mappings(id) ON DELETE SET NULL,
    direction         TEXT        NOT NULL,
    trigger           TEXT        NOT NULL,
    status            TEXT        NOT NULL DEFAULT 'queued',
    claimed_at        TIMESTAMPTZ,
    claimed_by        TEXT,
    attempts          INT         NOT NULL DEFAULT 0,
    next_retry_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at        TIMESTAMPTZ,
    finished_at       TIMESTAMPTZ,
    cursor_before     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    cursor_after      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    records_seen      INT         NOT NULL DEFAULT 0,
    records_changed   INT         NOT NULL DEFAULT 0,
    records_failed    INT         NOT NULL DEFAULT 0,
    conflicts_created INT         NOT NULL DEFAULT 0,
    error_kind        TEXT        NOT NULL DEFAULT '',
    error_message     TEXT        NOT NULL DEFAULT '',
    actor_id          TEXT        NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_external_sync_runs_direction CHECK (direction IN ('pull', 'push', 'bidirectional')),
    CONSTRAINT chk_external_sync_runs_trigger CHECK (trigger IN ('manual', 'schedule', 'retry', 'system', 'webhook', 'backfill')),
    CONSTRAINT chk_external_sync_runs_status CHECK (status IN ('queued', 'running', 'succeeded', 'partial', 'failed', 'cancelled', 'dead')),
    CONSTRAINT chk_external_sync_runs_attempts CHECK (attempts >= 0),
    CONSTRAINT chk_external_sync_runs_counts CHECK (records_seen >= 0 AND records_changed >= 0 AND records_failed >= 0 AND conflicts_created >= 0),
    CONSTRAINT chk_external_sync_runs_cursor_before_object CHECK (jsonb_typeof(cursor_before) = 'object'),
    CONSTRAINT chk_external_sync_runs_cursor_after_object CHECK (jsonb_typeof(cursor_after) = 'object'),
    CONSTRAINT chk_external_sync_runs_error_message_size CHECK (length(error_message) <= 2000)
);

CREATE INDEX IF NOT EXISTS idx_external_sync_runs_claim
    ON external_sync_runs (status, next_retry_at, claimed_at)
    WHERE status IN ('queued', 'failed');
CREATE INDEX IF NOT EXISTS idx_external_sync_runs_tenant_created
    ON external_sync_runs (tenant_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_external_sync_runs_connection_created
    ON external_sync_runs (tenant_id, connection_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_external_sync_runs_mapping_created
    ON external_sync_runs (tenant_id, mapping_id, created_at DESC, id DESC)
    WHERE mapping_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_external_sync_runs_health
    ON external_sync_runs (tenant_id, status, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS external_sync_attempts (
    id                  BIGSERIAL   PRIMARY KEY,
    run_id              UUID        NOT NULL REFERENCES external_sync_runs(id) ON DELETE CASCADE,
    attempt_number      INT         NOT NULL,
    started_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at         TIMESTAMPTZ,
    result              TEXT        NOT NULL DEFAULT 'running',
    http_status         INT         NOT NULL DEFAULT 0,
    provider_request_id TEXT        NOT NULL DEFAULT '',
    retry_after         TIMESTAMPTZ,
    error_kind          TEXT        NOT NULL DEFAULT '',
    error_message       TEXT        NOT NULL DEFAULT '',

    CONSTRAINT chk_external_sync_attempts_number CHECK (attempt_number >= 1),
    CONSTRAINT chk_external_sync_attempts_result CHECK (result IN ('running', 'succeeded', 'failed')),
    CONSTRAINT chk_external_sync_attempts_http_status CHECK (http_status BETWEEN 0 AND 599),
    CONSTRAINT chk_external_sync_attempts_error_message_size CHECK (length(error_message) <= 2000),
    UNIQUE (run_id, attempt_number)
);

CREATE TABLE IF NOT EXISTS external_sync_record_failures (
    id                 UUID        PRIMARY KEY,
    tenant_id          TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    run_id             UUID        NOT NULL REFERENCES external_sync_runs(id) ON DELETE CASCADE,
    mapping_id         UUID        NOT NULL REFERENCES external_object_mappings(id) ON DELETE CASCADE,
    operation          TEXT        NOT NULL,
    local_object_id    TEXT        NOT NULL DEFAULT '',
    external_key       TEXT        NOT NULL DEFAULT '',
    failure_kind       TEXT        NOT NULL,
    message            TEXT        NOT NULL DEFAULT '',
    payload_digest     TEXT        NOT NULL DEFAULT '',
    retry_mode         TEXT        NOT NULL,
    normalized_payload JSONB       NOT NULL DEFAULT '{}'::jsonb,
    retryable          BOOLEAN     NOT NULL DEFAULT TRUE,
    resolved_at        TIMESTAMPTZ,
    resolved_by        TEXT        NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_external_sync_record_failures_operation CHECK (operation IN ('pull', 'push', 'link', 'tombstone')),
    CONSTRAINT chk_external_sync_record_failures_retry_mode CHECK (retry_mode IN ('refetch', 'replay')),
    CONSTRAINT chk_external_sync_record_failures_payload_object CHECK (jsonb_typeof(normalized_payload) = 'object'),
    CONSTRAINT chk_external_sync_record_failures_message_size CHECK (length(message) <= 2000)
);

CREATE INDEX IF NOT EXISTS idx_external_sync_record_failures_retry
    ON external_sync_record_failures (tenant_id, retryable, resolved_at)
    WHERE resolved_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_external_sync_record_failures_run
    ON external_sync_record_failures (run_id, created_at);

CREATE TABLE IF NOT EXISTS external_sync_conflicts (
    id                UUID        PRIMARY KEY,
    tenant_id         TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    mapping_id        UUID        NOT NULL REFERENCES external_object_mappings(id) ON DELETE CASCADE,
    local_object_id   TEXT        NOT NULL DEFAULT '',
    external_key      TEXT        NOT NULL DEFAULT '',
    conflict_kind     TEXT        NOT NULL,
    status            TEXT        NOT NULL DEFAULT 'open',
    local_snapshot    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    external_snapshot JSONB       NOT NULL DEFAULT '{}'::jsonb,
    resolution        TEXT        NOT NULL DEFAULT '',
    resolved_at       TIMESTAMPTZ,
    resolved_by       TEXT        NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_external_sync_conflicts_status CHECK (status IN ('open', 'resolved', 'ignored')),
    CONSTRAINT chk_external_sync_conflicts_resolution CHECK (resolution IN ('', 'local_wins', 'external_wins', 'manual_merge', 'ignored')),
    CONSTRAINT chk_external_sync_conflicts_local_snapshot_object CHECK (jsonb_typeof(local_snapshot) = 'object'),
    CONSTRAINT chk_external_sync_conflicts_external_snapshot_object CHECK (jsonb_typeof(external_snapshot) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_external_sync_conflicts_open
    ON external_sync_conflicts (tenant_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS external_sync_events (
    id                 UUID        PRIMARY KEY,
    tenant_id          TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    connection_id      UUID        NOT NULL REFERENCES external_connections(id) ON DELETE CASCADE,
    mapping_id         UUID        REFERENCES external_object_mappings(id) ON DELETE SET NULL,
    provider           TEXT        NOT NULL,
    event_type         TEXT        NOT NULL,
    external_event_id  TEXT        NOT NULL DEFAULT '',
    dedupe_key         TEXT        NOT NULL,
    signature_status   TEXT        NOT NULL,
    status             TEXT        NOT NULL DEFAULT 'received',
    payload_digest     TEXT        NOT NULL,
    normalized_payload JSONB       NOT NULL DEFAULT '{}'::jsonb,
    received_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    replayed_at        TIMESTAMPTZ,
    replayed_by        TEXT        NOT NULL DEFAULT '',
    run_id             UUID        REFERENCES external_sync_runs(id) ON DELETE SET NULL,
    failure_reason     TEXT        NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_external_sync_events_provider_shape CHECK (provider ~ '^[a-z0-9_-]+$'),
    CONSTRAINT chk_external_sync_events_event_type CHECK (length(btrim(event_type)) BETWEEN 1 AND 200),
    CONSTRAINT chk_external_sync_events_external_event_id_size CHECK (length(external_event_id) <= 512),
    CONSTRAINT chk_external_sync_events_dedupe_key CHECK (length(btrim(dedupe_key)) BETWEEN 1 AND 512),
    CONSTRAINT chk_external_sync_events_signature_status CHECK (signature_status IN ('verified', 'failed', 'not_required')),
    CONSTRAINT chk_external_sync_events_status CHECK (status IN ('received', 'replayed', 'ignored', 'failed')),
    CONSTRAINT chk_external_sync_events_payload_digest CHECK (payload_digest ~ '^[a-f0-9]{64}$'),
    CONSTRAINT chk_external_sync_events_payload_object CHECK (jsonb_typeof(normalized_payload) = 'object'),
    CONSTRAINT chk_external_sync_events_failure_reason_size CHECK (length(failure_reason) <= 2000),
    CONSTRAINT uq_external_sync_events_dedupe UNIQUE (tenant_id, connection_id, dedupe_key)
);

CREATE INDEX IF NOT EXISTS idx_external_sync_events_tenant_created
    ON external_sync_events (tenant_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_external_sync_events_connection_created
    ON external_sync_events (tenant_id, connection_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_external_sync_events_status
    ON external_sync_events (tenant_id, status, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_external_sync_events_run
    ON external_sync_events (tenant_id, run_id)
    WHERE run_id IS NOT NULL;

CREATE OR REPLACE FUNCTION update_external_sync_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_external_connections_updated_at ON external_connections;
CREATE TRIGGER trg_external_connections_updated_at
    BEFORE UPDATE ON external_connections
    FOR EACH ROW
    EXECUTE FUNCTION update_external_sync_updated_at();

DROP TRIGGER IF EXISTS trg_external_object_mappings_updated_at ON external_object_mappings;
CREATE TRIGGER trg_external_object_mappings_updated_at
    BEFORE UPDATE ON external_object_mappings
    FOR EACH ROW
    EXECUTE FUNCTION update_external_sync_updated_at();

DROP TRIGGER IF EXISTS trg_external_object_links_updated_at ON external_object_links;
CREATE TRIGGER trg_external_object_links_updated_at
    BEFORE UPDATE ON external_object_links
    FOR EACH ROW
    EXECUTE FUNCTION update_external_sync_updated_at();

DROP TRIGGER IF EXISTS trg_external_sync_runs_updated_at ON external_sync_runs;
CREATE TRIGGER trg_external_sync_runs_updated_at
    BEFORE UPDATE ON external_sync_runs
    FOR EACH ROW
    EXECUTE FUNCTION update_external_sync_updated_at();

DROP TRIGGER IF EXISTS trg_external_sync_conflicts_updated_at ON external_sync_conflicts;
CREATE TRIGGER trg_external_sync_conflicts_updated_at
    BEFORE UPDATE ON external_sync_conflicts
    FOR EACH ROW
    EXECUTE FUNCTION update_external_sync_updated_at();

DROP TRIGGER IF EXISTS trg_external_sync_events_updated_at ON external_sync_events;
CREATE TRIGGER trg_external_sync_events_updated_at
    BEFORE UPDATE ON external_sync_events
    FOR EACH ROW
    EXECUTE FUNCTION update_external_sync_updated_at();

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
