-- Per-tenant Lark OAuth installation state.
--
-- Populated by POST /fb/v1/console/install/callback when a tenant admin
-- installs the listen app from the Lark marketplace. Tokens are refreshed
-- by a background goroutine (see service/lark_token_refresher.go, Wave 2).
--
-- One row per tenant. Reinstalling is an UPSERT — overwrites tokens
-- and bumps updated_at.

CREATE TABLE IF NOT EXISTS tenant_lark_install (
    -- One-to-one with tenants. CASCADE so deleting a tenant cleans up.
    tenant_id                TEXT        PRIMARY KEY
                                          REFERENCES tenants(id) ON DELETE CASCADE,

    -- Lark's stable tenant identifier. Also mirrored on tenants.lark_tenant_key
    -- so install-callback can resolve tenant_id BEFORE this row exists.
    lark_tenant_key          TEXT        NOT NULL,

    -- The listen app's ID inside Lark's open platform. Multiple listen
    -- apps may eventually share this DB (dev/staging/prod), so keep it
    -- explicit per install.
    app_id                   TEXT        NOT NULL,

    -- OAuth tokens. access_token is short-lived (~2h), refresh_token longer.
    -- Stored plain-text — the listen DB is itself the trust boundary, so
    -- compromise of DB compromises everything anyway. Wave 3+: rotate to
    -- pgsodium / KMS column encryption when we add per-tenant data export.
    access_token             TEXT        NOT NULL,
    access_token_expires_at  TIMESTAMPTZ NOT NULL,
    refresh_token            TEXT        NOT NULL,
    refresh_token_expires_at TIMESTAMPTZ NOT NULL,

    -- Comma-separated Lark scope strings granted at install time.
    scopes                   TEXT        NOT NULL DEFAULT '',

    installed_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Callback dispatch: before tenant_id is known we look up by lark_tenant_key.
CREATE INDEX IF NOT EXISTS idx_tenant_lark_install_key
    ON tenant_lark_install (lark_tenant_key);
