-- Migration 005: notify_outbox — at-least-once outbound delivery queue.
--
-- enricher INSERTs one row per (feedback, destination) inside the same
-- tx that flips user_feedback.enrichment_status='done'. A separate worker
-- claims rows with `FOR UPDATE SKIP LOCKED`, delivers, and either marks
-- 'delivered' or schedules the next retry.
--
-- Saving the full envelope JSON on the row means a destination URL/secret
-- change doesn't retroactively change what gets resent — the contract
-- the customer originally received is preserved.
--
-- Idempotent: IF NOT EXISTS on every object.

CREATE TABLE IF NOT EXISTS notify_outbox (
    id                 BIGSERIAL   PRIMARY KEY,
    feedback_id        BIGINT      NOT NULL REFERENCES user_feedback(id),
    tenant_id          TEXT        NOT NULL,
    destination_type   TEXT        NOT NULL,
    destination_target TEXT        NOT NULL,
    audience           TEXT        NOT NULL,
    payload            JSONB       NOT NULL,
    status             TEXT        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'delivered', 'failed', 'dead')),
    attempts           SMALLINT    NOT NULL DEFAULT 0,
    next_retry_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    trace_id           TEXT        NOT NULL,
    last_error         TEXT,
    dead_reason        TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at       TIMESTAMPTZ,
    claimed_at         TIMESTAMPTZ
);

-- Worker hot path: pull next batch of rows ready to send.
CREATE INDEX IF NOT EXISTS idx_notify_outbox_pending
    ON notify_outbox (next_retry_at)
    WHERE status IN ('pending', 'failed');

-- Console + audit queries by tenant.
CREATE INDEX IF NOT EXISTS idx_notify_outbox_tenant_status
    ON notify_outbox (tenant_id, status);

-- Lookup all delivery attempts for a single feedback row.
CREATE INDEX IF NOT EXISTS idx_notify_outbox_feedback
    ON notify_outbox (feedback_id);
