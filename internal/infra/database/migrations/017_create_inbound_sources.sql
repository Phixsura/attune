-- 017_create_inbound_sources.sql — channel-agnostic inbound source registry (#66 Plan T9).
--
-- One row per (tenant, channel, slug). UNIQUE constraint enforces the
-- N:1 model: a tenant may register many webhook sources and many email
-- mailboxes, but slug must be unique within (tenant, channel).
--
-- config is Tink AEAD-encrypted JSON; inbound.SecretStore is the legal decoder
-- interface.

CREATE TABLE IF NOT EXISTS inbound_sources (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel       TEXT NOT NULL,
    name          TEXT NOT NULL,
    slug          TEXT NOT NULL,
    config        BYTEA NOT NULL,
    enabled       BOOL NOT NULL DEFAULT TRUE,
    last_event_at TIMESTAMPTZ,
    last_uid      BIGINT NOT NULL DEFAULT 0,
    last_error    TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, channel, slug)
);

CREATE INDEX IF NOT EXISTS inbound_sources_channel_enabled
    ON inbound_sources (channel, enabled) WHERE enabled = TRUE;
CREATE INDEX IF NOT EXISTS inbound_sources_tenant
    ON inbound_sources (tenant_id, channel);
