-- Migration 004: tenant_notify_targets — per-tenant outbound webhook config.
--
-- Wave 1.2: env array is upserted into this table at startup (dogfood mode).
-- Wave 2: console UI does CRUD directly; env is deprecated.
--
-- Idempotent: re-runs ignored via IF NOT EXISTS.

CREATE TABLE IF NOT EXISTS tenant_notify_targets (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    destination_type TEXT        NOT NULL
        CHECK (destination_type IN ('raw-webhook', 'lark-bot', 'slack-bot', 'email')),
    audience         TEXT        NOT NULL DEFAULT 'pool'
        CHECK (audience IN ('pool', 'radar', 'all')),
    url              TEXT,
    secret           TEXT,
    timeout_seconds  SMALLINT    NOT NULL DEFAULT 10,
    disabled         BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, destination_type, audience)
);

-- Hot path index: list active destinations for one tenant on every push.
CREATE INDEX IF NOT EXISTS idx_tenant_notify_active
    ON tenant_notify_targets (tenant_id)
    WHERE disabled = FALSE;
