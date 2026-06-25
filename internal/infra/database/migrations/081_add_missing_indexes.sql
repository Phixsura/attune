-- Migration: add_missing_indexes
-- Add indexes for foreign keys and commonly queried columns.

-- feedback_tag_assignments: composite index for common queries
CREATE INDEX IF NOT EXISTS idx_feedback_tag_assignments_feedback_tag
    ON feedback_tag_assignments(feedback_id, tag_id);

-- feedback_tag_assignments: index on tag_id for tag deletion queries
CREATE INDEX IF NOT EXISTS idx_feedback_tag_assignments_tag_id
    ON feedback_tag_assignments(tag_id);

-- notify_outbox: index on feedback_id for FK lookups
CREATE INDEX IF NOT EXISTS idx_notify_outbox_feedback_id
    ON notify_outbox(feedback_id) WHERE feedback_id IS NOT NULL;

-- llm_usage_audit: index on feedback_id for FK lookups
-- Note: This table may not exist in all environments
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'llm_usage_audit') THEN
        CREATE INDEX IF NOT EXISTS idx_llm_usage_audit_feedback_id
            ON llm_usage_audit(feedback_id) WHERE feedback_id IS NOT NULL;
    END IF;
END $$;
