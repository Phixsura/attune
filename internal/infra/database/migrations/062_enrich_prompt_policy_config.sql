-- Migration 062: converge typed enrichment prompt-policy config.
--
-- Databases that already advanced through an earlier 061 need a new migration
-- number to add the typed policy config and semantic provenance columns.

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS enrich_prompt_policy JSONB NOT NULL DEFAULT '{
        "output_language_policy":"source_and_display",
        "title_max_chars":30,
        "rationale_max_chars":30,
        "display_fields_required":true,
        "tone":"concise"
    }'::jsonb,
    ADD COLUMN IF NOT EXISTS active_enrich_prompt_version_id UUID;

ALTER TABLE tenant_enrich_prompt_versions
    ADD COLUMN IF NOT EXISTS policy_config JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE tenant_enrich_prompt_versions
   SET policy_config = '{
        "output_language_policy":"source_and_display",
        "title_max_chars":30,
        "rationale_max_chars":30,
        "display_fields_required":true,
        "tone":"concise"
    }'::jsonb
 WHERE policy_config = '{}'::jsonb;

CREATE UNIQUE INDEX IF NOT EXISTS uq_tenant_enrich_prompt_versions_tenant_id_id
    ON tenant_enrich_prompt_versions (tenant_id, id);

UPDATE tenants t
   SET active_enrich_prompt_version_id = (
       SELECT v.id
         FROM tenant_enrich_prompt_versions v
        WHERE v.tenant_id = t.id
        ORDER BY v.created_at DESC, v.id DESC
        LIMIT 1
   ),
       updated_at = NOW()
 WHERE t.active_enrich_prompt_version_id IS NULL
   AND EXISTS (
       SELECT 1
         FROM tenant_enrich_prompt_versions v
        WHERE v.tenant_id = t.id
   );

ALTER TABLE semantic_extraction_runs
    ADD COLUMN IF NOT EXISTS prompt_version_id UUID;
