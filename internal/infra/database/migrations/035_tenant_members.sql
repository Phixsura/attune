-- Unified tenant membership table for RBAC (#38).
--
-- Associates users from any authentication source (admin, oidc_user, tenant_user)
-- with a tenant and role. Replaces per-table role columns as the single source
-- of truth for authorization.

CREATE TABLE IF NOT EXISTS tenant_members (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     TEXT         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    -- User reference (supports multiple authentication sources)
    member_type   TEXT         NOT NULL DEFAULT 'oidc_user'
                  CHECK (member_type IN ('admin', 'oidc_user', 'tenant_user')),
    user_id       TEXT         NOT NULL,

    -- Three-level role hierarchy: admin > member > viewer
    role          TEXT         NOT NULL DEFAULT 'member'
                  CHECK (role IN ('admin', 'member', 'viewer')),

    -- Role source distinguishes IdP-assigned vs manually-assigned roles.
    -- 'idp' = set by OIDC group mapping, may be updated on login
    -- 'manual' = explicitly set by an admin, preserved across logins
    -- 'bootstrap' = set during migration or first admin creation
    role_source   TEXT         NOT NULL DEFAULT 'idp'
                  CHECK (role_source IN ('idp', 'manual', 'bootstrap')),

    -- Audit trail for invitations
    invited_by    UUID         REFERENCES tenant_members(id) ON DELETE SET NULL,
    invited_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    accepted_at   TIMESTAMPTZ, -- NULL = pending invitation

    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    -- One membership per user per tenant
    UNIQUE (tenant_id, member_type, user_id)
);

-- Index for tenant-scoped queries (list members, count admins)
CREATE INDEX IF NOT EXISTS idx_tenant_members_tenant
    ON tenant_members (tenant_id);

-- Index for user lookups (middleware role check)
CREATE INDEX IF NOT EXISTS idx_tenant_members_user
    ON tenant_members (member_type, user_id);

-- Index for role-based queries (count admins for last-admin check)
CREATE INDEX IF NOT EXISTS idx_tenant_members_role
    ON tenant_members (tenant_id, role);

-- Auto-update updated_at on any change
CREATE OR REPLACE FUNCTION update_tenant_members_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_tenant_members_updated
    BEFORE UPDATE ON tenant_members
    FOR EACH ROW EXECUTE FUNCTION update_tenant_members_updated_at();

-- ============================================================
-- Migration: promote existing users to admin (zero disruption)
-- ============================================================

-- Migrate tenant_users (active only)
INSERT INTO tenant_members (
    tenant_id, member_type, user_id, role, role_source, invited_at, accepted_at
)
SELECT
    tenant_id,
    'tenant_user',
    id::TEXT,
    'admin',      -- Promote existing users to admin
    'bootstrap',
    created_at,
    created_at
FROM tenant_users
WHERE is_active = TRUE
ON CONFLICT DO NOTHING;

-- Migrate OIDC users
-- Single-tenant assumption: OIDC users associate with the sole tenant.
-- Multi-tenant deployments should use oidc_users.tenant_id if present.
INSERT INTO tenant_members (
    tenant_id, member_type, user_id, role, role_source, invited_at, accepted_at
)
SELECT
    (SELECT id FROM tenants LIMIT 1),
    'oidc_user',
    id,
    CASE WHEN role = 'admin' THEN 'admin' ELSE 'member' END,
    'bootstrap',
    created_at,
    created_at
FROM oidc_users
WHERE EXISTS (SELECT 1 FROM tenants)
  AND (SELECT COUNT(*) FROM tenants) = 1  -- Only for single-tenant deploys
ON CONFLICT DO NOTHING;

-- ============================================================
-- Update oidc_users constraint to allow viewer role
-- ============================================================
ALTER TABLE oidc_users DROP CONSTRAINT IF EXISTS chk_oidc_role;
ALTER TABLE oidc_users ADD CONSTRAINT chk_oidc_role
    CHECK (role IN ('admin', 'member', 'viewer'));
