-- Reply draft generation (#26).
--
-- user_feedback.reply_draft holds the LLM-generated, operator-facing draft.
-- It is overwrite-only; llm_audit (purpose='reply_draft') keeps the per-call
-- history. tenants gates generation opt-in (default off) with an optional
-- confidence threshold and prompt override. reply_draft_task is the async
-- outbox drained by the reply-draft worker (taskoutbox.Queue, gated on
-- reply_draft_enabled).

ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS reply_draft              TEXT,
    ADD COLUMN IF NOT EXISTS reply_draft_generated_at TIMESTAMPTZ;

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS reply_draft_enabled         BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS reply_draft_min_confidence  DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS reply_draft_prompt_template TEXT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_tenants_reply_draft_min_confidence'
          AND conrelid = 'tenants'::regclass
    ) THEN
        ALTER TABLE tenants
            ADD CONSTRAINT chk_tenants_reply_draft_min_confidence
            CHECK (reply_draft_min_confidence >= 0 AND reply_draft_min_confidence <= 1);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS reply_draft_task (
    id               BIGSERIAL PRIMARY KEY,
    feedback_id      BIGINT NOT NULL REFERENCES user_feedback(id) ON DELETE CASCADE,
    tenant_id        TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending',
    attempts         INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    claimed_at       TIMESTAMPTZ,
    next_retry_at    TIMESTAMPTZ,
    last_error       TEXT NOT NULL DEFAULT '',
    inbound_trace_id TEXT NOT NULL DEFAULT '', -- carried from enrich so the worker's llm_audit links to the source ingest
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at     TIMESTAMPTZ,
    CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    UNIQUE (feedback_id)
);

-- Status-led claim index so the pending/failed (+ next_retry_at) branch is
-- covered, mirroring embedding_task.
CREATE INDEX IF NOT EXISTS idx_reply_draft_task_claim
    ON reply_draft_task (status, next_retry_at, created_at)
    WHERE status IN ('pending', 'failed');

-- Tenant-scoped queue-depth and stale-processing scans.
CREATE INDEX IF NOT EXISTS idx_reply_draft_task_tenant_status
    ON reply_draft_task (tenant_id, status, created_at DESC);
