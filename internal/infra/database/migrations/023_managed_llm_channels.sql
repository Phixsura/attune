-- Managed LLM channel and route state (#23).
--
-- Process config now carries only the shared secret keyset. Provider endpoint
-- config, API keys, abilities, and routing are runtime state managed through
-- Console/API/CLI.

CREATE TABLE IF NOT EXISTS secret_key_registry (
    key_id              TEXT PRIMARY KEY,
    primary_key         BOOLEAN NOT NULL DEFAULT FALSE,
    status              TEXT NOT NULL DEFAULT 'ENABLED',
    type_url            TEXT NOT NULL DEFAULT '',
    output_prefix_type  TEXT NOT NULL DEFAULT '',
    key_material_type   TEXT NOT NULL DEFAULT '',
    fingerprint_sha256  TEXT NOT NULL DEFAULT '',
    fingerprint_version INTEGER NOT NULL DEFAULT 1,
    first_seen_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    retired_at          TIMESTAMPTZ,
    CHECK (status IN ('ENABLED', 'DISABLED', 'DESTROYED', 'UNKNOWN'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_secret_key_registry_primary
    ON secret_key_registry (primary_key)
    WHERE primary_key;

CREATE TABLE IF NOT EXISTS llm_channels (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                  TEXT NOT NULL,
    protocol              TEXT NOT NULL,
    base_url              TEXT NOT NULL DEFAULT '',
    auth_mode             TEXT NOT NULL DEFAULT 'bearer',
    credential_key_id     TEXT NOT NULL DEFAULT '',
    credential_ciphertext BYTEA,
    status                TEXT NOT NULL DEFAULT 'enabled',
    priority              INTEGER NOT NULL DEFAULT 0,
    weight                INTEGER NOT NULL DEFAULT 1 CHECK (weight >= 0),
    timeout_seconds       INTEGER NOT NULL DEFAULT 60 CHECK (timeout_seconds > 0),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_tested_at        TIMESTAMPTZ,
    last_test_status      TEXT NOT NULL DEFAULT '',
    last_error            TEXT NOT NULL DEFAULT '',
    UNIQUE (name),
    CHECK (protocol IN ('openai-compat', 'openai-responses', 'anthropic', 'gemini')),
    CHECK (auth_mode IN ('bearer', 'none')),
    CHECK (status IN ('enabled', 'disabled', 'draining'))
);

CREATE INDEX IF NOT EXISTS idx_llm_channels_status_priority
    ON llm_channels (status, priority DESC, weight DESC, name ASC);

CREATE TABLE IF NOT EXISTS llm_channel_abilities (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id      UUID NOT NULL REFERENCES llm_channels(id) ON DELETE CASCADE,
    logical_model   TEXT NOT NULL,
    provider_model  TEXT NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    priority        INTEGER NOT NULL DEFAULT 0,
    weight          INTEGER NOT NULL DEFAULT 1 CHECK (weight >= 0),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (channel_id, logical_model)
);

CREATE INDEX IF NOT EXISTS idx_llm_abilities_model_enabled
    ON llm_channel_abilities (logical_model, enabled, priority DESC, weight DESC);

CREATE TABLE IF NOT EXISTS llm_routes (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      TEXT NOT NULL DEFAULT '',
    purpose        TEXT NOT NULL,
    logical_model  TEXT NOT NULL,
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, purpose)
);

CREATE INDEX IF NOT EXISTS idx_llm_routes_lookup
    ON llm_routes (tenant_id, purpose, enabled);

ALTER TABLE llm_audit
    ADD COLUMN IF NOT EXISTS channel_id UUID,
    ADD COLUMN IF NOT EXISTS provider_model_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS llm_protocol TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_llm_audit_channel_created
    ON llm_audit (channel_id, created_at DESC)
    WHERE channel_id IS NOT NULL;
