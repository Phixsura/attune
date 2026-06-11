-- Retry metadata for enrichment workers.
--
-- Failed rows remain visible as enrichment_status='failed', but workers only
-- retry them after the scheduled backoff and stop once attempts reach the
-- service cap. This keeps permanent provider/config/persistence failures from
-- burning tokens forever while preserving the existing public status enum.

ALTER TABLE user_feedback
  ADD COLUMN IF NOT EXISTS enrichment_attempts INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS enrichment_next_retry_at TIMESTAMPTZ;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
      FROM pg_constraint
     WHERE conname = 'chk_user_feedback_enrichment_attempts_nonnegative'
  ) THEN
    ALTER TABLE user_feedback
      ADD CONSTRAINT chk_user_feedback_enrichment_attempts_nonnegative
      CHECK (enrichment_attempts >= 0);
  END IF;
END $$;

DROP INDEX IF EXISTS idx_user_feedback_enrichment_status;
CREATE INDEX IF NOT EXISTS idx_user_feedback_enrichment_status
  ON user_feedback (enrichment_status, enrichment_next_retry_at, created_at)
  WHERE enrichment_status IN ('pending', 'failed');
