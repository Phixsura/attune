-- Migration: add_unique_constraints
-- Add unique constraints to columns that should be unique within tenant scope.
-- Note: Uses IF EXISTS checks for tables that may not exist in all environments.

-- inbound_sources: slug should be unique per tenant
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'inbound_sources') THEN
        CREATE UNIQUE INDEX IF NOT EXISTS idx_inbound_sources_tenant_slug
            ON inbound_sources(tenant_id, slug);
    END IF;
END $$;

-- guard_policies: name should be unique per tenant
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'guard_policies') THEN
        CREATE UNIQUE INDEX IF NOT EXISTS idx_guard_policies_tenant_name
            ON guard_policies(tenant_id, name);
    END IF;
END $$;

-- workflow_states: name should be unique (global)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'workflow_states') THEN
        CREATE UNIQUE INDEX IF NOT EXISTS idx_workflow_states_name
            ON workflow_states(name);
    END IF;
END $$;
