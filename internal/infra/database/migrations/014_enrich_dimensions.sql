-- Metadata-driven Enrich Dimensions (#10 → E3 proposal).
--
-- The earlier `flat-labels` design (#10) made `kind` and `severity`
-- per-tenant strings, then collapsed both into one labels TEXT[] set.
-- The reviewer pointed out that hard-coded N-axis models always break
-- at v2 of every SaaS-grade ticketing / feedback / observability tool
-- (GitHub Projects v2, Jira Custom Fields, Linear Custom Properties,
-- Sentry tags, Datadog universal tagging). So we skip B and land
-- straight on metadata-driven Dimensions:
--
--   tenants.enrich_dimensions       JSONB  -- the dim metadata
--   user_feedback.enriched_attrs    JSONB  -- the LLM-emitted values
--   user_feedback.is_urgent         BOOL   -- derived; persisted snapshot
--
-- Each Dimension is { name, display_name (i18n map), kind, taxonomy
-- ([{value, display_name (i18n map)}, ...]), urgent_set, required }.
-- Adding any new dim = settings edit, zero code change.
--
-- Pre-1.0: the demo tenant has no production data worth preserving.
-- All of the prior B-direction columns (enriched_labels,
-- enrich_label_taxonomy, urgent_labels) and the older A-direction
-- columns (enriched_kind, enriched_severity, enriched_modules,
-- enriched_priority, tenants.enrich_modules) all DROP in one shot.
-- No 015 cleanup tail.

-- 1) Tenant-side metadata: the dim definitions.
--
-- The column DEFAULT carries the three OSS seed dims (type / severity /
-- labels) so EVERY tenant — both rows backfilled at migration time and
-- INSERT-INTO-tenants on a fresh deploy — starts with a usable config.
-- Operators may delete or relabel any dim via the Settings UI; the
-- column is stored verbatim, no triggers.
ALTER TABLE tenants
    DROP COLUMN IF EXISTS enrich_modules,
    DROP COLUMN IF EXISTS enrich_label_taxonomy,
    DROP COLUMN IF EXISTS urgent_labels,
    ADD COLUMN IF NOT EXISTS enrich_dimensions JSONB NOT NULL DEFAULT '[
       {
         "name": "type",
         "display_name": {"default": "Type", "zh": "类型", "en": "Type"},
         "kind": "single",
         "taxonomy": [
           {"value": "bug",      "display_name": {"default": "Bug",      "zh": "缺陷", "en": "Bug"}},
           {"value": "feature",  "display_name": {"default": "Feature",  "zh": "特性", "en": "Feature"}},
           {"value": "question", "display_name": {"default": "Question", "zh": "咨询", "en": "Question"}},
           {"value": "other",    "display_name": {"default": "Other",    "zh": "其他", "en": "Other"}}
         ],
         "urgent_set": [],
         "required": false
       },
       {
         "name": "severity",
         "display_name": {"default": "Severity", "zh": "严重程度", "en": "Severity"},
         "kind": "single",
         "taxonomy": [
           {"value": "critical", "display_name": {"default": "Critical", "zh": "严重", "en": "Critical"}},
           {"value": "major",    "display_name": {"default": "Major",    "zh": "重要", "en": "Major"}},
           {"value": "minor",    "display_name": {"default": "Minor",    "zh": "次要", "en": "Minor"}}
         ],
         "urgent_set": ["critical"],
         "required": false
       },
       {
         "name": "labels",
         "display_name": {"default": "Labels", "zh": "标签", "en": "Labels"},
         "kind": "multi",
         "taxonomy": [],
         "urgent_set": [],
         "required": false
       }
   ]'::jsonb;

-- 2) Row-side: the LLM-populated values + derived urgent flag.
ALTER TABLE user_feedback
    DROP COLUMN IF EXISTS enriched_kind,
    DROP COLUMN IF EXISTS enriched_severity,
    DROP COLUMN IF EXISTS enriched_modules,
    DROP COLUMN IF EXISTS enriched_priority,
    DROP COLUMN IF EXISTS enriched_labels,
    ADD COLUMN IF NOT EXISTS enriched_attrs JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS is_urgent BOOLEAN NOT NULL DEFAULT FALSE;

-- 3) GIN with jsonb_path_ops supports efficient `@>` containment for
-- per-dim filter queries: enriched_attrs @> '{"severity":"critical"}'.
-- jsonb_path_ops is smaller and faster than the default jsonb_ops for
-- pure containment, which is the only access pattern the console uses.
CREATE INDEX IF NOT EXISTS idx_user_feedback_enriched_attrs_gin
    ON user_feedback USING GIN (enriched_attrs jsonb_path_ops);

-- (The column DEFAULT above already seeds rows backfilled at migration
-- time and INSERT-INTO-tenants on a fresh deploy. No explicit UPDATE
-- needed.)
