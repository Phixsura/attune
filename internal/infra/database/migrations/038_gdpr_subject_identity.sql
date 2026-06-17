-- GDPR subject identity and audit actions (#43).
--
-- Adds tenant-scoped canonical subject columns to user_feedback so GDPR
-- export/delete can match one end-user exactly, without relying on the legacy
-- transport-shaped user_id. Also extends audit_log's allowed action set with
-- gdpr.export / gdpr.delete.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS subject_key TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS subject_display TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS subject_hash TEXT NOT NULL DEFAULT '';

WITH normalized AS (
    SELECT id,
           tenant_id,
           CASE
               WHEN user_id LIKE 'ext_%:%' THEN split_part(user_id, ':', 2)
               WHEN user_id LIKE 'ext_%' THEN user_id
               ELSE user_id
           END AS computed_subject
    FROM user_feedback
)
UPDATE user_feedback uf
SET subject_key = normalized.computed_subject,
    subject_display = normalized.computed_subject,
    subject_hash = CASE
        WHEN normalized.computed_subject <> ''
            THEN encode(
                digest(
                    convert_to(normalized.tenant_id, 'UTF8')
                    || decode('00', 'hex')
                    || convert_to(normalized.computed_subject, 'UTF8'),
                    'sha256'
                ),
                'hex'
            )
        ELSE ''
    END
FROM normalized
WHERE uf.id = normalized.id
  AND uf.subject_key = '';

CREATE INDEX IF NOT EXISTS idx_user_feedback_tenant_subject_key
    ON user_feedback (tenant_id, subject_key, created_at DESC)
    WHERE subject_key <> '';

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS chk_audit_action_value;
ALTER TABLE audit_log ADD CONSTRAINT chk_audit_action_value
    CHECK (action IN (
        'api_key.create',
        'api_key.revoke',
        'digest_subscription.delete',
        'digest_subscription.upsert',
        'enrich_config.update',
        'feedback.batch_delete',
        'feedback_job.cancel',
        'gdpr.delete',
        'gdpr.export',
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
