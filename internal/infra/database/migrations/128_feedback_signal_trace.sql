-- SPDX-License-Identifier: Apache-2.0
--
-- Durable feedback signal trace for end-to-end source-to-survey evidence.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS signal_trace_id TEXT NOT NULL DEFAULT gen_random_uuid()::text;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_user_feedback_signal_trace_id_shape'
          AND conrelid = 'user_feedback'::regclass
    ) THEN
        ALTER TABLE user_feedback
            ADD CONSTRAINT chk_user_feedback_signal_trace_id_shape
            CHECK (length(signal_trace_id) BETWEEN 1 AND 160);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_user_feedback_signal_trace
    ON user_feedback (tenant_id, signal_trace_id);
