ALTER TABLE external_sync_runs
  ADD COLUMN IF NOT EXISTS input_metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE external_object_links
  ADD COLUMN IF NOT EXISTS normalized_payload JSONB NOT NULL DEFAULT '{}'::jsonb;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'chk_external_sync_runs_input_metadata_object'
  ) THEN
    ALTER TABLE external_sync_runs
      ADD CONSTRAINT chk_external_sync_runs_input_metadata_object
      CHECK (jsonb_typeof(input_metadata) = 'object');
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'chk_external_object_links_normalized_payload_object'
  ) THEN
    ALTER TABLE external_object_links
      ADD CONSTRAINT chk_external_object_links_normalized_payload_object
      CHECK (jsonb_typeof(normalized_payload) = 'object');
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS external_object_comments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  external_object_link_id UUID NOT NULL REFERENCES external_object_links(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  external_object_type TEXT NOT NULL DEFAULT 'issue',
  external_key TEXT NOT NULL,
  direction TEXT NOT NULL,
  origin TEXT NOT NULL,
  provider_comment_id TEXT NOT NULL DEFAULT '',
  local_comment_id TEXT NOT NULL DEFAULT '',
  author_display TEXT NOT NULL DEFAULT '',
  author_external_id TEXT NOT NULL DEFAULT '',
  body TEXT NOT NULL DEFAULT '',
  body_digest TEXT NOT NULL DEFAULT '',
  marker TEXT NOT NULL DEFAULT '',
  external_url TEXT NOT NULL DEFAULT '',
  external_version TEXT NOT NULL DEFAULT '',
  external_created_at TIMESTAMPTZ,
  external_updated_at TIMESTAMPTZ,
  last_synced_at TIMESTAMPTZ,
  sync_state TEXT NOT NULL DEFAULT 'pending',
  sync_error TEXT NOT NULL DEFAULT '',
  external_sync_event_id UUID REFERENCES external_sync_events(id) ON DELETE SET NULL,
  first_run_id UUID REFERENCES external_sync_runs(id) ON DELETE SET NULL,
  last_run_id UUID REFERENCES external_sync_runs(id) ON DELETE SET NULL,
  created_by TEXT NOT NULL DEFAULT '',
  updated_by TEXT NOT NULL DEFAULT '',
  body_truncated BOOLEAN NOT NULL DEFAULT FALSE,
  deleted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT chk_external_object_comments_provider_nonempty CHECK (provider <> ''),
  CONSTRAINT chk_external_object_comments_type_nonempty CHECK (external_object_type <> ''),
  CONSTRAINT chk_external_object_comments_key_nonempty CHECK (external_key <> ''),
  CONSTRAINT chk_external_object_comments_direction CHECK (direction IN ('pull', 'push')),
  CONSTRAINT chk_external_object_comments_origin CHECK (origin IN ('external', 'attune', 'system')),
  CONSTRAINT chk_external_object_comments_sync_state CHECK (sync_state IN ('pending', 'synced', 'conflict', 'failed', 'deleted')),
  CONSTRAINT chk_external_object_comments_body_size CHECK (char_length(body) <= 5000)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_external_object_comments_provider_comment_unique
  ON external_object_comments (tenant_id, external_object_link_id, provider_comment_id)
  WHERE provider_comment_id <> '' AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_external_object_comments_local_comment_unique
  ON external_object_comments (tenant_id, external_object_link_id, local_comment_id)
  WHERE local_comment_id <> '' AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_external_object_comments_marker_unique
  ON external_object_comments (tenant_id, external_object_link_id, marker)
  WHERE marker <> '' AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_external_object_comments_link_updated
  ON external_object_comments (tenant_id, external_object_link_id, external_updated_at DESC NULLS LAST, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_external_object_comments_sync_state
  ON external_object_comments (tenant_id, sync_state, updated_at DESC);

DROP TRIGGER IF EXISTS trg_external_object_comments_updated_at ON external_object_comments;
CREATE TRIGGER trg_external_object_comments_updated_at
  BEFORE UPDATE ON external_object_comments
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
    'customer_request.add_comment',
    'customer_request.delete_note',
    'customer_request.merge',
    'customer_request.create_github_issue',
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
