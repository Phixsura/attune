-- Ensure an active prompt version belongs to the same tenant row that points at it.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conname = 'fk_tenants_active_enrich_prompt_version_tenant'
    ) THEN
        ALTER TABLE tenants
            ADD CONSTRAINT fk_tenants_active_enrich_prompt_version_tenant
            FOREIGN KEY (id, active_enrich_prompt_version_id)
            REFERENCES tenant_enrich_prompt_versions(tenant_id, id)
            DEFERRABLE INITIALLY IMMEDIATE;
    END IF;
END $$;
