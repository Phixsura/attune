-- Migration 002: claim column for atomic enrichment locking.
--
-- Adds enrichment_claimed_at, set when a worker takes a row into the
-- 'enriching' state. The poller treats rows whose claim is older than
-- 5 minutes as stale and re-claims them, so a crashed worker doesn't
-- leave a row stuck.

ALTER TABLE user_feedback
  ADD COLUMN IF NOT EXISTS enrichment_claimed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_user_feedback_enrichment_claim
  ON user_feedback (enrichment_status, enrichment_claimed_at)
  WHERE enrichment_status IN ('pending', 'failed', 'enriching');
