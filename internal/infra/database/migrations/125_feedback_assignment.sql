-- Feedback assignment turns operational triage into durable ownership.

ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS owner_member_id UUID,
    ADD COLUMN IF NOT EXISTS owner_assigned_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS owner_assigned_by TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS feedback_sla_due_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS owner_assignment_note TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_user_feedback_owner_member'
    ) THEN
        ALTER TABLE user_feedback
            ADD CONSTRAINT fk_user_feedback_owner_member
            FOREIGN KEY (owner_member_id)
            REFERENCES tenant_members(id)
            ON DELETE SET NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_user_feedback_assignment_note_length'
    ) THEN
        ALTER TABLE user_feedback
            ADD CONSTRAINT chk_user_feedback_assignment_note_length
            CHECK (length(owner_assignment_note) <= 1000) NOT VALID;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_user_feedback_assignment_owner
    ON user_feedback (tenant_id, owner_member_id, created_at DESC)
    WHERE owner_member_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_user_feedback_assignment_sla
    ON user_feedback (tenant_id, feedback_sla_due_at, created_at DESC)
    WHERE feedback_sla_due_at IS NOT NULL;
