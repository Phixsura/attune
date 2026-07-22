-- SPDX-License-Identifier: Apache-2.0

CREATE TABLE IF NOT EXISTS customer_request_delivery_artifacts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
  request_id UUID NOT NULL,
  provider TEXT NOT NULL,
  connection_id UUID REFERENCES external_connections(id) ON DELETE SET NULL,
  mapping_id UUID REFERENCES external_object_mappings(id) ON DELETE SET NULL,
  external_object_link_id UUID REFERENCES external_object_links(id) ON DELETE SET NULL,
  artifact_type TEXT NOT NULL,
  relationship TEXT NOT NULL DEFAULT 'references',
  external_key TEXT NOT NULL,
  external_url TEXT NOT NULL DEFAULT '',
  display_key TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT '',
  status_category TEXT NOT NULL DEFAULT '',
  state_reason TEXT NOT NULL DEFAULT '',
  assignee TEXT NOT NULL DEFAULT '',
  sync_state TEXT NOT NULL DEFAULT 'pending',
  sync_error TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'delivery_artifact',
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  external_updated_at TIMESTAMPTZ,
  first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen_at TIMESTAMPTZ,
  deleted_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT fk_customer_request_delivery_artifacts_request
    FOREIGN KEY (tenant_id, request_id)
    REFERENCES customer_requests(tenant_id, id)
    ON DELETE CASCADE,
  CONSTRAINT chk_customer_request_delivery_artifacts_provider_shape CHECK (provider ~ '^[a-z0-9_-]+$'),
  CONSTRAINT chk_customer_request_delivery_artifacts_type CHECK (
    artifact_type IN (
      'issue',
      'pull_request',
      'commit',
      'branch',
      'deployment',
      'release',
      'project_item',
      'sub_issue',
      'support_ticket'
    )
  ),
  CONSTRAINT chk_customer_request_delivery_artifacts_relationship CHECK (
    relationship IN (
      'tracked_by',
      'implements',
      'blocks',
      'duplicates',
      'references',
      'ships_in',
      'reported_from',
      'parent',
      'child'
    )
  ),
  CONSTRAINT chk_customer_request_delivery_artifacts_sync_state CHECK (
    sync_state IN ('manual', 'pending', 'synced', 'stale', 'failed', 'conflict', 'deleted')
  ),
  CONSTRAINT chk_customer_request_delivery_artifacts_external_key CHECK (length(btrim(external_key)) BETWEEN 1 AND 512),
  CONSTRAINT chk_customer_request_delivery_artifacts_external_url CHECK (length(external_url) <= 2048),
  CONSTRAINT chk_customer_request_delivery_artifacts_display_key CHECK (length(display_key) <= 512),
  CONSTRAINT chk_customer_request_delivery_artifacts_title CHECK (length(title) <= 500),
  CONSTRAINT chk_customer_request_delivery_artifacts_status CHECK (length(status) <= 120),
  CONSTRAINT chk_customer_request_delivery_artifacts_status_category CHECK (length(status_category) <= 120),
  CONSTRAINT chk_customer_request_delivery_artifacts_state_reason CHECK (length(state_reason) <= 240),
  CONSTRAINT chk_customer_request_delivery_artifacts_assignee CHECK (length(assignee) <= 500),
  CONSTRAINT chk_customer_request_delivery_artifacts_sync_error CHECK (length(sync_error) <= 2000),
  CONSTRAINT chk_customer_request_delivery_artifacts_source CHECK (length(btrim(source)) BETWEEN 1 AND 120),
  CONSTRAINT chk_customer_request_delivery_artifacts_payload_object CHECK (jsonb_typeof(payload) = 'object')
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_customer_request_delivery_artifacts_unique
  ON customer_request_delivery_artifacts (tenant_id, request_id, provider, artifact_type, external_key)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_customer_request_delivery_artifacts_request
  ON customer_request_delivery_artifacts (tenant_id, request_id, last_seen_at DESC NULLS LAST, updated_at DESC)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_customer_request_delivery_artifacts_external_link
  ON customer_request_delivery_artifacts (tenant_id, external_object_link_id)
  WHERE external_object_link_id IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_customer_request_delivery_artifacts_sync_health
  ON customer_request_delivery_artifacts (tenant_id, sync_state, last_seen_at DESC NULLS LAST)
  WHERE deleted_at IS NULL;

DROP TRIGGER IF EXISTS trg_customer_request_delivery_artifacts_updated_at ON customer_request_delivery_artifacts;
CREATE TRIGGER trg_customer_request_delivery_artifacts_updated_at
  BEFORE UPDATE ON customer_request_delivery_artifacts
  FOR EACH ROW
  EXECUTE FUNCTION update_external_sync_updated_at();
