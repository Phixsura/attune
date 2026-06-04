-- Migration 003: Lark Base mirror claim column.
--
-- Adds lark_mirror_claimed_at so the inline mirror trigger and the 30s
-- background sweeper can atomically race for the same row without
-- producing two records in the Lark Base mirror.
--
-- Recovery semantics: a claim older than 5 minutes is treated as stuck
-- and may be re-claimed (handles crashed mirror calls).

ALTER TABLE user_feedback
  ADD COLUMN IF NOT EXISTS lark_mirror_claimed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_user_feedback_mirror_pending
  ON user_feedback (enrichment_status, lark_base_record_id, enriched_at)
  WHERE enrichment_status = 'done' AND lark_base_record_id IS NULL;
