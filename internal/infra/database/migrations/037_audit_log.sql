CREATE TABLE IF NOT EXISTS audit_log (
    id               BIGSERIAL PRIMARY KEY,
    tenant_id        TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    actor_type       TEXT NOT NULL,
    actor_id         TEXT NOT NULL,
    actor_email      TEXT NOT NULL DEFAULT '',
    actor_ip         TEXT NOT NULL DEFAULT '',
    actor_user_agent TEXT NOT NULL DEFAULT '',
    action           TEXT NOT NULL,
    target_type      TEXT NOT NULL,
    target_id        TEXT NOT NULL DEFAULT '',
    summary          TEXT NOT NULL DEFAULT '',
    before_json      JSONB,
    after_json       JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE audit_log ADD CONSTRAINT chk_audit_actor_type
    CHECK (length(actor_type) BETWEEN 1 AND 64);
ALTER TABLE audit_log ADD CONSTRAINT chk_audit_actor_id
    CHECK (length(actor_id) BETWEEN 1 AND 128);
ALTER TABLE audit_log ADD CONSTRAINT chk_audit_action
    CHECK (length(action) BETWEEN 1 AND 128);
ALTER TABLE audit_log ADD CONSTRAINT chk_audit_action_value
    CHECK (action IN (
        'api_key.create',
        'api_key.revoke',
        'digest_subscription.delete',
        'digest_subscription.upsert',
        'enrich_config.update',
        'feedback.batch_delete',
        'feedback_job.cancel',
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
        'tag.archive',
        'tag.create',
        'tag.update',
        'workflow_seed_defaults.run',
        'workflow_state.archive',
        'workflow_state.create',
        'workflow_state.update',
        'workflow_transition.replace'
    ));
ALTER TABLE audit_log ADD CONSTRAINT chk_audit_target_type
    CHECK (length(target_type) BETWEEN 1 AND 64);
ALTER TABLE audit_log ADD CONSTRAINT chk_audit_target_id
    CHECK (length(target_id) <= 256);
ALTER TABLE audit_log ADD CONSTRAINT chk_audit_summary
    CHECK (length(summary) <= 512);

CREATE INDEX IF NOT EXISTS idx_audit_tenant_created
    ON audit_log (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_tenant_action_created
    ON audit_log (tenant_id, action, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_tenant_actor_created
    ON audit_log (tenant_id, actor_type, actor_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_audit_tenant_target_created
    ON audit_log (tenant_id, target_type, target_id, created_at DESC);

CREATE OR REPLACE FUNCTION prevent_audit_log_mutation() RETURNS trigger AS $$
DECLARE
    prune_before timestamptz;
BEGIN
    IF TG_OP = 'TRUNCATE' THEN
        RAISE EXCEPTION 'audit_log is append-only';
    END IF;

    IF TG_OP = 'DELETE' THEN
        prune_before := NULLIF(current_setting('attune.audit_prune_before', true), '')::timestamptz;
        IF current_setting('attune.audit_prune', true) = 'on'
           AND prune_before IS NOT NULL
           AND OLD.created_at < prune_before THEN
            RETURN OLD;
        END IF;
        RAISE EXCEPTION 'audit_log rows may only be deleted by retention prune before cutoff';
    END IF;

    RAISE EXCEPTION 'audit_log is append-only';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS audit_log_no_mutation ON audit_log;
CREATE TRIGGER audit_log_no_mutation
    BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW
    EXECUTE FUNCTION prevent_audit_log_mutation();

DROP TRIGGER IF EXISTS audit_log_no_truncate ON audit_log;
CREATE TRIGGER audit_log_no_truncate
    BEFORE TRUNCATE ON audit_log
    FOR EACH STATEMENT
    EXECUTE FUNCTION prevent_audit_log_mutation();
