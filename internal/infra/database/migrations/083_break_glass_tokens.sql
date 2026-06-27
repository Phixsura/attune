-- Break-glass tokens for emergency SSO recovery (#158).
--
-- One-time, time-limited tokens that allow admin login when SSO is
-- unavailable or misconfigured. Designed per NIST SP 800-63C-4 replay
-- protection requirements.

CREATE TABLE IF NOT EXISTS break_glass_tokens (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    admin_email TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    issued_by TEXT NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    revoked_by TEXT,
    CONSTRAINT chk_breakglass_not_both CHECK (used_at IS NULL OR revoked_at IS NULL)
);

CREATE INDEX IF NOT EXISTS idx_breakglass_lookup
    ON break_glass_tokens (tenant_id, admin_email, expires_at)
    WHERE used_at IS NULL AND revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_breakglass_cleanup
    ON break_glass_tokens (expires_at)
    WHERE used_at IS NULL AND revoked_at IS NULL;

-- System settings key-value store for runtime configuration.
-- auth.mode is the first entry; future settings follow the same pattern.
CREATE TABLE IF NOT EXISTS system_settings (
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, key)
);

-- Seed default auth mode for existing tenants.
INSERT INTO system_settings (tenant_id, key, value, updated_by)
SELECT id, 'auth.mode', 'hybrid', 'migration'
FROM tenants
ON CONFLICT (tenant_id, key) DO NOTHING;
