-- Migration 061: Add FK and index to mcp_sessions
--
-- Adds missing foreign key on tenant_id and index for tenant-scoped queries.

BEGIN;

-- Add FK constraint on tenant_id (matches mcp_oauth_clients pattern)
ALTER TABLE mcp_sessions
    ADD CONSTRAINT fk_mcp_sessions_tenant
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;

-- Add index for tenant-scoped session queries
CREATE INDEX idx_mcp_sessions_tenant ON mcp_sessions(tenant_id) WHERE closed_at IS NULL;

COMMIT;
