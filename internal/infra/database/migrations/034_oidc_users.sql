-- OIDC users: external identity → local user mapping for SSO.
CREATE TABLE IF NOT EXISTS oidc_users (
    id            TEXT PRIMARY KEY DEFAULT gen_random_uuid()::TEXT,
    provider      TEXT NOT NULL DEFAULT 'oidc',
    external_id   TEXT NOT NULL,
    email         TEXT NOT NULL,
    display_name  TEXT,
    role          TEXT NOT NULL DEFAULT 'member',
    groups        JSONB NOT NULL DEFAULT '[]'::JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ
);

-- Unique constraint: one local user per (provider, external_id) pair.
CREATE UNIQUE INDEX IF NOT EXISTS idx_oidc_users_provider_external
    ON oidc_users (provider, external_id);

-- Fast lookup by email (for display/search).
CREATE INDEX IF NOT EXISTS idx_oidc_users_email
    ON oidc_users (email);

-- Role validation.
ALTER TABLE oidc_users ADD CONSTRAINT chk_oidc_role
    CHECK (role IN ('admin', 'member'));
