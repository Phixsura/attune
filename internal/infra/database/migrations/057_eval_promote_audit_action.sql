-- Migration 057: allow the eval-suggestion promote + api-key rotate audit
-- actions in the audit log.
--
-- Two unregistered actions were emitted by handler code but rejected by the
-- chk_audit_action_value whitelist (last set in 052), so their audit writes
-- failed silently (the handlers log the error but still return 200):
--   * enrich_config.promote_suggested — promoting an eval-suggested taxonomy
--     value (#83); found via the full-stack e2e.
--   * api_key.rotate — emitted by apikey/api_keys_advanced.go since key
--     rotation shipped, but never whitelisted; surfaced by the same audit
--     cross-check. A security-relevant gap: key rotations went unaudited.
--
-- Keep the Go allow-list (internal/service/auditlog/actions.go) in lockstep.

ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS chk_audit_action_value;
ALTER TABLE audit_log ADD CONSTRAINT chk_audit_action_value
    CHECK (action IN (
        'api_key.create',
        'api_key.revoke',
        'api_key.rotate',
        'digest_subscription.delete',
        'digest_subscription.upsert',
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
        'tag.archive',
        'tag.create',
        'tag.update',
        'workflow_seed_defaults.run',
        'workflow_state.archive',
        'workflow_state.create',
        'workflow_state.update',
        'workflow_transition.replace'
    ));
