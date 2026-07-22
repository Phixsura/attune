-- SPDX-License-Identifier: Apache-2.0

CREATE TABLE IF NOT EXISTS external_provider_installations (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  display_name TEXT NOT NULL,
  installation_kind TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  external_installation_id TEXT NOT NULL DEFAULT '',
  account_login TEXT NOT NULL DEFAULT '',
  account_id TEXT NOT NULL DEFAULT '',
  account_url TEXT NOT NULL DEFAULT '',
  base_url TEXT NOT NULL DEFAULT '',
  permissions JSONB NOT NULL DEFAULT '{}'::jsonb,
  capability_profile JSONB NOT NULL DEFAULT '{}'::jsonb,
  resource_selection TEXT NOT NULL DEFAULT 'selected',
  qualification_status TEXT NOT NULL DEFAULT 'untested',
  last_qualified_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT '',
  created_by TEXT NOT NULL,
  updated_by TEXT NOT NULL,
  deleted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT chk_external_provider_installations_provider_shape CHECK (provider ~ '^[a-z0-9_-]+$'),
  CONSTRAINT chk_external_provider_installations_display_name CHECK (length(btrim(display_name)) BETWEEN 1 AND 200),
  CONSTRAINT chk_external_provider_installations_kind CHECK (installation_kind IN ('github_app', 'oauth_app', 'token', 'manual')),
  CONSTRAINT chk_external_provider_installations_status CHECK (status IN ('pending', 'active', 'limited', 'drifted', 'suspended', 'deleted')),
  CONSTRAINT chk_external_provider_installations_resource_selection CHECK (resource_selection IN ('all', 'selected', 'none')),
  CONSTRAINT chk_external_provider_installations_qualification_status CHECK (qualification_status IN ('untested', 'ok', 'warning', 'failed')),
  CONSTRAINT chk_external_provider_installations_permissions_object CHECK (jsonb_typeof(permissions) = 'object'),
  CONSTRAINT chk_external_provider_installations_capability_profile_object CHECK (jsonb_typeof(capability_profile) = 'object'),
  CONSTRAINT chk_external_provider_installations_last_error_size CHECK (length(last_error) <= 2000),
  CONSTRAINT chk_external_provider_installations_deleted_status CHECK ((deleted_at IS NULL) OR status = 'deleted')
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_external_provider_installations_active_name
  ON external_provider_installations (tenant_id, provider, lower(display_name))
  WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_external_provider_installations_external_id
  ON external_provider_installations (tenant_id, provider, installation_kind, external_installation_id)
  WHERE deleted_at IS NULL AND external_installation_id <> '';

CREATE INDEX IF NOT EXISTS idx_external_provider_installations_tenant_status
  ON external_provider_installations (tenant_id, provider, status, qualification_status)
  WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS external_provider_installation_resources (
  id UUID PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  installation_id UUID NOT NULL REFERENCES external_provider_installations(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  resource_type TEXT NOT NULL,
  external_resource_id TEXT NOT NULL DEFAULT '',
  resource_key TEXT NOT NULL,
  display_name TEXT NOT NULL,
  html_url TEXT NOT NULL DEFAULT '',
  selected BOOLEAN NOT NULL DEFAULT TRUE,
  status TEXT NOT NULL DEFAULT 'active',
  permissions JSONB NOT NULL DEFAULT '{}'::jsonb,
  last_seen_at TIMESTAMPTZ,
  deleted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT chk_external_provider_installation_resources_provider_shape CHECK (provider ~ '^[a-z0-9_-]+$'),
  CONSTRAINT chk_external_provider_installation_resources_type CHECK (resource_type IN ('repository', 'project', 'workspace', 'organization')),
  CONSTRAINT chk_external_provider_installation_resources_key CHECK (length(btrim(resource_key)) BETWEEN 1 AND 512),
  CONSTRAINT chk_external_provider_installation_resources_display_name CHECK (length(btrim(display_name)) BETWEEN 1 AND 300),
  CONSTRAINT chk_external_provider_installation_resources_status CHECK (status IN ('active', 'removed', 'unknown')),
  CONSTRAINT chk_external_provider_installation_resources_permissions_object CHECK (jsonb_typeof(permissions) = 'object'),
  CONSTRAINT chk_external_provider_installation_resources_deleted_status CHECK ((deleted_at IS NULL) OR status = 'removed')
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_external_provider_installation_resources_key
  ON external_provider_installation_resources (tenant_id, installation_id, resource_type, resource_key)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_external_provider_installation_resources_selected
  ON external_provider_installation_resources (tenant_id, installation_id, selected, status)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_external_provider_installation_resources_provider_resource
  ON external_provider_installation_resources (tenant_id, provider, resource_type, resource_key)
  WHERE deleted_at IS NULL;

ALTER TABLE external_connections
  ADD COLUMN IF NOT EXISTS provider_installation_id UUID REFERENCES external_provider_installations(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_external_connections_provider_installation
  ON external_connections (tenant_id, provider_installation_id)
  WHERE provider_installation_id IS NOT NULL AND deleted_at IS NULL;

DROP TRIGGER IF EXISTS trg_external_provider_installations_updated_at ON external_provider_installations;
CREATE TRIGGER trg_external_provider_installations_updated_at
  BEFORE UPDATE ON external_provider_installations
  FOR EACH ROW
  EXECUTE FUNCTION update_external_sync_updated_at();

DROP TRIGGER IF EXISTS trg_external_provider_installation_resources_updated_at ON external_provider_installation_resources;
CREATE TRIGGER trg_external_provider_installation_resources_updated_at
  BEFORE UPDATE ON external_provider_installation_resources
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
    'external_provider_installation.create',
    'external_provider_installation.delete',
    'external_provider_installation.qualify',
    'external_provider_installation.resources_select',
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
