-- 117: Cohort sync tables (#233).
--
-- cohort_sources: one per provider connection (Amplitude project, Mixpanel project).
-- cohorts: one per synced cohort.
-- cohort_memberships: one row per user per cohort.
-- cohort_sync_runs: sync execution log.

CREATE TABLE IF NOT EXISTS cohort_sources (
  id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id                  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  provider                   TEXT NOT NULL,
  name                       TEXT NOT NULL,
  auth_type                  TEXT NOT NULL DEFAULT 'api_key',
  credential_key_id          TEXT NOT NULL DEFAULT '',
  credential_ciphertext      BYTEA NOT NULL DEFAULT '',
  base_url                   TEXT NOT NULL DEFAULT '',
  provider_config            JSONB NOT NULL DEFAULT '{}'::jsonb,
  webhook_secret_key_id      TEXT NOT NULL DEFAULT '',
  webhook_secret_ciphertext  BYTEA NOT NULL DEFAULT '',
  enabled                    BOOLEAN NOT NULL DEFAULT TRUE,
  status                     TEXT NOT NULL DEFAULT 'active',
  last_sync_at               TIMESTAMPTZ,
  last_error                 TEXT NOT NULL DEFAULT '',
  created_by                 TEXT NOT NULL DEFAULT '',
  updated_by                 TEXT NOT NULL DEFAULT '',
  created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT chk_cohort_sources_provider CHECK (provider IN ('amplitude', 'mixpanel')),
  CONSTRAINT chk_cohort_sources_status CHECK (status IN ('active', 'disabled', 'error')),
  CONSTRAINT chk_cohort_sources_name_nonempty CHECK (name <> ''),
  CONSTRAINT chk_cohort_sources_config_object CHECK (jsonb_typeof(provider_config) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_cohort_sources_tenant
  ON cohort_sources (tenant_id);

CREATE TABLE IF NOT EXISTS cohorts (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id           TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  cohort_source_id    UUID NOT NULL REFERENCES cohort_sources(id) ON DELETE CASCADE,
  external_cohort_id  TEXT NOT NULL,
  name                TEXT NOT NULL,
  description         TEXT NOT NULL DEFAULT '',
  stale_ttl_days      INT NOT NULL DEFAULT 30,
  member_count        INT NOT NULL DEFAULT 0,
  enabled             BOOLEAN NOT NULL DEFAULT TRUE,
  last_synced_at      TIMESTAMPTZ,
  last_error          TEXT NOT NULL DEFAULT '',
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT chk_cohorts_name_nonempty CHECK (name <> ''),
  CONSTRAINT chk_cohorts_external_id_nonempty CHECK (external_cohort_id <> ''),
  CONSTRAINT chk_cohorts_stale_ttl CHECK (stale_ttl_days BETWEEN 1 AND 365),
  CONSTRAINT uq_cohorts_source_external UNIQUE (tenant_id, cohort_source_id, external_cohort_id)
);

CREATE INDEX IF NOT EXISTS idx_cohorts_tenant
  ON cohorts (tenant_id);

CREATE TABLE IF NOT EXISTS cohort_memberships (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id          TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  cohort_id          UUID NOT NULL REFERENCES cohorts(id) ON DELETE CASCADE,
  external_user_id   TEXT NOT NULL,
  email              TEXT NOT NULL DEFAULT '',
  display_name       TEXT NOT NULL DEFAULT '',
  user_properties    JSONB NOT NULL DEFAULT '{}'::jsonb,
  joined_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  left_at            TIMESTAMPTZ,
  expires_at         TIMESTAMPTZ,
  last_seen_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT chk_cohort_memberships_ext_id_nonempty CHECK (external_user_id <> ''),
  CONSTRAINT chk_cohort_memberships_properties_object CHECK (jsonb_typeof(user_properties) = 'object'),
  CONSTRAINT uq_cohort_memberships_user UNIQUE (tenant_id, cohort_id, external_user_id)
);

-- Active members for a cohort (filter queries).
CREATE INDEX IF NOT EXISTS idx_cohort_memberships_active
  ON cohort_memberships (tenant_id, cohort_id)
  WHERE left_at IS NULL;

-- All cohorts a user belongs to (feedback JOIN path).
CREATE INDEX IF NOT EXISTS idx_cohort_memberships_by_user
  ON cohort_memberships (tenant_id, external_user_id)
  WHERE left_at IS NULL;

-- Expired membership cleanup.
CREATE INDEX IF NOT EXISTS idx_cohort_memberships_expired
  ON cohort_memberships (expires_at)
  WHERE expires_at IS NOT NULL AND left_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS cohort_sync_runs (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  cohort_id       UUID NOT NULL REFERENCES cohorts(id) ON DELETE CASCADE,
  trigger         TEXT NOT NULL DEFAULT 'webhook',
  status          TEXT NOT NULL DEFAULT 'running',
  members_added   INT NOT NULL DEFAULT 0,
  members_removed INT NOT NULL DEFAULT 0,
  members_total   INT NOT NULL DEFAULT 0,
  error_message   TEXT NOT NULL DEFAULT '',
  started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at     TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT chk_cohort_sync_runs_trigger CHECK (trigger IN ('webhook', 'manual', 'system')),
  CONSTRAINT chk_cohort_sync_runs_status CHECK (status IN ('running', 'succeeded', 'failed', 'skipped'))
);

CREATE INDEX IF NOT EXISTS idx_cohort_sync_runs_cohort
  ON cohort_sync_runs (tenant_id, cohort_id, created_at DESC);
