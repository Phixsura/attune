-- 121: Add unique constraint on cohort source name per tenant (#233).
-- Prevents operators from creating duplicate-named sources which would
-- be confusing in the UI and in audit logs.
ALTER TABLE cohort_sources ADD CONSTRAINT uq_cohort_sources_tenant_name UNIQUE (tenant_id, name);
