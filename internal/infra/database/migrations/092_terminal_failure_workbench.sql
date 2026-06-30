-- Migration 092: add terminal failure snapshot columns for the feedback workbench.
--
-- The terminal-failure workbench groups rows by failure-time metadata, not the
-- live tenant config. These columns snapshot the last failed enrich attempt so
-- the workbench can group by reason class, model/channel, and config version
-- without dereferencing mutable runtime state.

ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS enrichment_failure_reason_class TEXT,
    ADD COLUMN IF NOT EXISTS enrichment_failure_model TEXT,
    ADD COLUMN IF NOT EXISTS enrichment_failure_channel_id TEXT,
    ADD COLUMN IF NOT EXISTS enrichment_failure_channel_name TEXT,
    ADD COLUMN IF NOT EXISTS enrichment_failure_config_fingerprint TEXT,
    ADD COLUMN IF NOT EXISTS enrichment_failure_prompt_version TEXT;
