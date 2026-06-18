-- Workflow state names become i18n (#64 / WS2). The human label moves into a
-- localized display_name JSONB; the existing `name` column is repurposed as the
-- stable machine key (a slug), mirroring the dimensions "stable key + i18n
-- display" model (014_enrich_dimensions). The (tenant_id, name) unique index
-- and the name CHECK constraints carry over unchanged — they now guard the key.
ALTER TABLE tenant_workflow_states
    ADD COLUMN IF NOT EXISTS display_name JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Backfill: preserve the historical label as both the default and zh entry,
-- and repurpose `name` as a slug key. Known seed labels map to english slugs so
-- re-running SeedDefaults stays idempotent (ON CONFLICT (tenant_id, name));
-- tenant-authored (possibly non-ASCII) labels fall back to a deterministic
-- id-based key, with the original label preserved in display_name.
UPDATE tenant_workflow_states
SET display_name = jsonb_build_object('default', name, 'zh', name),
    name = CASE name
        WHEN '待处理' THEN 'pending'
        WHEN '已分拣' THEN 'triaged'
        WHEN '处理中' THEN 'in_progress'
        WHEN '已修复' THEN 'fixed'
        WHEN '不处理' THEN 'wont_fix'
        ELSE 'state_' || replace(id::text, '-', '')
    END
WHERE display_name = '{}'::jsonb;
