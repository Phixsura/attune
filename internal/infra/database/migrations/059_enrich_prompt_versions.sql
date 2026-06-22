-- Enrichment prompt policy version history (#107).
--
-- The active tenant config stays denormalized on tenants for the hot enrich
-- path. This table records immutable snapshots whenever operators save or
-- reactivate a prompt policy, so Console can diff/rollback and enrichment runs
-- can be tied back to a concrete policy identity.

CREATE TABLE IF NOT EXISTS tenant_enrich_prompt_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    prompt_template TEXT,
    dimensions JSONB NOT NULL,
    policy_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    prompt_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    prompt_version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_tenant_enrich_prompt_versions_tenant_id_id
    ON tenant_enrich_prompt_versions (tenant_id, id);

CREATE INDEX IF NOT EXISTS idx_tenant_enrich_prompt_versions_tenant_created
    ON tenant_enrich_prompt_versions (tenant_id, created_at DESC, id DESC);

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS enrich_prompt_policy JSONB NOT NULL DEFAULT '{
        "output_language_policy":"source_and_display",
        "title_max_chars":30,
        "rationale_max_chars":30,
        "display_fields_required":true,
        "tone":"concise"
    }'::jsonb,
    ADD COLUMN IF NOT EXISTS active_enrich_prompt_version_id UUID;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conname = 'fk_tenants_active_enrich_prompt_version'
    ) THEN
        ALTER TABLE tenants
            ADD CONSTRAINT fk_tenants_active_enrich_prompt_version
            FOREIGN KEY (active_enrich_prompt_version_id)
            REFERENCES tenant_enrich_prompt_versions(id)
            ON DELETE SET NULL;
    END IF;
END $$;

WITH tenant_policy AS (
    SELECT
        t.id AS tenant_id,
        t.enrich_prompt_template AS prompt_template,
        t.enrich_dimensions AS dimensions,
        t.enrich_prompt_policy AS policy_config,
        CASE
            WHEN NULLIF(BTRIM(COALESCE(t.enrich_prompt_template, '')), '') IS NULL THEN 'enrich.default'
            ELSE 'enrich.legacy_custom_template'
        END AS policy_id,
        CASE
            WHEN NULLIF(BTRIM(COALESCE(t.enrich_prompt_template, '')), '') IS NULL THEN '1'
            ELSE 'sha256:' || SUBSTRING(
                ENCODE(
                    DIGEST(
                        CONVERT_TO(REPLACE(t.enrich_prompt_template, '{{ content }}', '{{content}}'), 'UTF8') ||
                        DECODE('00', 'hex') || CONVERT_TO('attune.enrich.prompt_contract.v1', 'UTF8') ||
                        DECODE('00', 'hex') || CONVERT_TO('attune.enrich.output_contract.v1', 'UTF8'),
                        'sha256'
                    ),
                    'hex'
                )
                FROM 1 FOR 16
            )
        END AS policy_version,
        CASE
            WHEN NULLIF(BTRIM(COALESCE(t.enrich_prompt_template, '')), '') IS NULL THEN 'default'
            ELSE 'legacy_custom_override'
        END AS mode,
        CASE
            WHEN NULLIF(BTRIM(COALESCE(t.enrich_prompt_template, '')), '') IS NULL THEN 'built_in'
            ELSE 'custom_template'
        END AS prompt_source,
        CASE
            WHEN NULLIF(BTRIM(COALESCE(t.enrich_prompt_template, '')), '') IS NOT NULL THEN 'custom'
            WHEN LOWER(COALESCE(t.locale, 'en')) LIKE 'zh%' THEN 'zh'
            ELSE 'en'
        END AS template_language,
        COALESCE(t.locale, 'en') AS display_locale,
        CASE
            WHEN LOWER(COALESCE(t.locale, 'en')) LIKE 'zh-tw%' OR
                 LOWER(COALESCE(t.locale, 'en')) LIKE 'zh-hk%' OR
                 LOWER(COALESCE(t.locale, 'en')) LIKE 'zh-mo%' OR
                 LOWER(COALESCE(t.locale, 'en')) LIKE '%hant%' THEN 'Traditional Chinese'
            WHEN LOWER(COALESCE(t.locale, 'en')) LIKE 'zh%' THEN 'Simplified Chinese'
            WHEN LOWER(COALESCE(t.locale, 'en')) LIKE 'ja%' THEN 'Japanese'
            ELSE 'English'
        END AS display_language_name
    FROM tenants t
    WHERE t.active_enrich_prompt_version_id IS NULL
      AND NOT EXISTS (
          SELECT 1
            FROM tenant_enrich_prompt_versions v
           WHERE v.tenant_id = t.id
      )
),
seed_versions AS (
    INSERT INTO tenant_enrich_prompt_versions (
        tenant_id, prompt_template, dimensions, policy_config, prompt_policy, prompt_version
    )
    SELECT
        tenant_id,
        prompt_template,
        dimensions,
        policy_config,
        jsonb_build_object(
            'policy_id', policy_id,
            'policy_version', policy_version,
            'prompt_version', policy_id || '@' || policy_version,
            'mode', mode,
            'prompt_source', prompt_source,
            'template_language', template_language,
            'display_locale', display_locale,
            'display_language_name', display_language_name,
            'variables', jsonb_build_array(
                jsonb_build_object('name', 'content', 'required', true, 'meaning', 'Raw feedback text to classify.'),
                jsonb_build_object('name', 'dimensions', 'required', true, 'meaning', 'Rendered Dimension contract and taxonomy hints.'),
                jsonb_build_object('name', 'display_locale', 'required', true, 'meaning', 'Tenant display locale for operator-facing fields.'),
                jsonb_build_object('name', 'display_language', 'required', true, 'meaning', 'Normalized display language code.'),
                jsonb_build_object('name', 'display_language_name', 'required', true, 'meaning', 'Human-readable display language name.')
            ),
            'outputs', jsonb_build_array(
                jsonb_build_object('name', 'title', 'required', true, 'language', 'source'),
                jsonb_build_object('name', 'rationale', 'required', true, 'language', 'source'),
                jsonb_build_object('name', 'display_title', 'required', true, 'language', 'display'),
                jsonb_build_object('name', 'display_rationale', 'required', true, 'language', 'display'),
                jsonb_build_object('name', 'classification_confidence', 'required', true, 'language', 'numeric'),
                jsonb_build_object('name', 'dimension_fields', 'required', true, 'language', 'contract')
            ),
            'warnings', '[]'::jsonb
        ),
        policy_id || '@' || policy_version
    FROM tenant_policy
    RETURNING tenant_id, id
)
UPDATE tenants t
   SET active_enrich_prompt_version_id = s.id,
       updated_at = NOW()
  FROM seed_versions s
 WHERE t.id = s.tenant_id
   AND t.active_enrich_prompt_version_id IS NULL;

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
