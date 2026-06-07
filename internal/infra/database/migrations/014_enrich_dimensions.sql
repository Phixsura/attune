-- Flatten the classification axes into a single per-tenant labels set
-- (#10 → flat-labels proposal). Until now the enricher emitted three
-- axes — kind (bug|feature|ops|question|other), severity (P0..P3) and
-- modules — all hardcoded except modules. Customer-support SaaS,
-- consumer apps, and game ops shops all want their own classification
-- vocabulary, not our software-engineering one. GitHub's flat per-repo
-- label set is the proven shape; this migration aligns attune.
--
-- New tenant config:
--   enrich_label_taxonomy : the customer's full label vocabulary
--   urgent_labels         : subset that triggers the 雷达 (urgent) route
--                           previously implicit in P0/P1
--
-- New per-row enrichment columns:
--   enriched_labels : the labels the LLM picked (whitelist-filtered)
--   is_urgent       : derived — any picked label ∈ urgent_labels
--
-- Old columns (enriched_kind, enriched_severity, enriched_modules,
-- enriched_priority, tenants.enrich_modules) intentionally left in
-- place: the application stops reading and writing them this PR, a
-- follow-up issue drops them after a quiet release.

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS enrich_label_taxonomy TEXT[],
    ADD COLUMN IF NOT EXISTS urgent_labels         TEXT[];

ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS enriched_labels TEXT[],
    ADD COLUMN IF NOT EXISTS is_urgent       BOOLEAN NOT NULL DEFAULT FALSE;
