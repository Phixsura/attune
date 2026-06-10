-- Language-aware enrichment metadata (#22).
--
-- Language is row/enrichment metadata, not a tenant-owned semantic Dimension.
-- It is populated by the enricher after claim/load and exposed to the console
-- for multilingual triage surfaces.

ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS language TEXT;
