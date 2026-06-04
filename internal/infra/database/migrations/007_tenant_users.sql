-- Members of a tenant.
--
-- Populated when a Lark user lands on the OAuth callback after they
-- (or their admin) install the listen app. Each row = one Lark user
-- inside one tenant.
--
-- Stage B uses this only to:
--   * answer GET /fb/v1/console/me without re-hitting Lark API per request
--   * record last_seen_at for "active users" billing signal
--   * prepare for Wave 3 RBAC (role column already shipped)
--
-- Audit log table (tenant_audit_log) is intentionally NOT created here;
-- it joins this table once audit is built.

CREATE TABLE IF NOT EXISTS tenant_users (
    -- Surrogate PK so future audit_log can reference users without
    -- leaking Lark's open_id into external systems.
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),

    tenant_id     TEXT         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- Lark's user identifier scoped to the listen app. Stable across logins.
    -- (Lark also has union_id across the same ISV's apps; Stage B doesn't
    -- need it.)
    open_id       TEXT         NOT NULL,

    name          TEXT         NOT NULL DEFAULT '',
    avatar_url    TEXT         NOT NULL DEFAULT '',

    -- Stage B is single-role in practice; column shipped for Wave 3 RBAC.
    -- First user of a tenant (the installer) gets 'admin'; later joiners
    -- default 'member' until an admin promotes them.
    role          TEXT         NOT NULL DEFAULT 'member'
                                CHECK (role IN ('admin', 'member')),

    is_active     BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    last_seen_at  TIMESTAMPTZ,

    -- One Lark user per tenant; reinstall is idempotent UPSERT.
    UNIQUE (tenant_id, open_id)
);

CREATE INDEX IF NOT EXISTS idx_tenant_users_tenant
    ON tenant_users (tenant_id) WHERE is_active = TRUE;
