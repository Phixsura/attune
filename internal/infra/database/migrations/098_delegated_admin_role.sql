-- SPDX-License-Identifier: Apache-2.0
--
-- Add delegated_admin as an explicit tenant member role so operational
-- governance can be separated from full tenant ownership.

ALTER TABLE tenant_members DROP CONSTRAINT IF EXISTS tenant_members_role_check;
ALTER TABLE tenant_members DROP CONSTRAINT IF EXISTS chk_tenant_members_role;
ALTER TABLE tenant_members ADD CONSTRAINT chk_tenant_members_role
    CHECK (role IN ('admin', 'delegated_admin', 'member', 'viewer'));

ALTER TABLE oidc_users DROP CONSTRAINT IF EXISTS chk_oidc_role;
ALTER TABLE oidc_users ADD CONSTRAINT chk_oidc_role
    CHECK (role IN ('admin', 'delegated_admin', 'member', 'viewer'));
