-- Operator-localized enrichment display text. The original
-- enriched_title/enriched_rationale stay in the detected source language;
-- these fields hold the tenant-locale display copy used by the console.
ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS enriched_display_title TEXT,
    ADD COLUMN IF NOT EXISTS enriched_display_rationale TEXT,
    ADD COLUMN IF NOT EXISTS enriched_display_locale TEXT;
