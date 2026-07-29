-- 120: Cohort sync webhook event log (#233).
--
-- Records each incoming webhook delivery for dedup, replay, and debugging.
-- Mirrors the external_sync_events pattern from migration 105.

CREATE TABLE IF NOT EXISTS cohort_sync_events (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id           TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  cohort_source_id    UUID NOT NULL REFERENCES cohort_sources(id) ON DELETE CASCADE,
  provider            TEXT NOT NULL,
  event_type          TEXT NOT NULL DEFAULT '',
  dedupe_key          TEXT NOT NULL,
  status              TEXT NOT NULL DEFAULT 'received',
  payload_digest      TEXT NOT NULL DEFAULT '',
  members_count       INT NOT NULL DEFAULT 0,
  failure_reason      TEXT NOT NULL DEFAULT '',
  run_id              UUID REFERENCES cohort_sync_runs(id) ON DELETE SET NULL,
  received_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT chk_cohort_sync_events_provider CHECK (provider <> ''),
  CONSTRAINT chk_cohort_sync_events_dedupe CHECK (dedupe_key <> ''),
  CONSTRAINT chk_cohort_sync_events_status CHECK (status IN ('received', 'processed', 'duplicate', 'failed'))
);

-- Dedup: at most one event per dedupe_key per source.
CREATE UNIQUE INDEX IF NOT EXISTS uq_cohort_sync_events_dedupe
  ON cohort_sync_events (tenant_id, cohort_source_id, dedupe_key);

CREATE INDEX IF NOT EXISTS idx_cohort_sync_events_source
  ON cohort_sync_events (tenant_id, cohort_source_id, created_at DESC);
