-- Migration: add_unique_constraints
-- Add unique constraints to columns that should be unique within tenant scope.

-- inbound_sources: slug should be unique per tenant
CREATE UNIQUE INDEX IF NOT EXISTS idx_inbound_sources_tenant_slug
    ON inbound_sources(tenant_id, slug);

-- guard_policies: name should be unique per tenant
CREATE UNIQUE INDEX IF NOT EXISTS idx_guard_policies_tenant_name
    ON guard_policies(tenant_id, name);

-- workflow_states: name should be unique (global)
CREATE UNIQUE INDEX IF NOT EXISTS idx_workflow_states_name
    ON workflow_states(name);
