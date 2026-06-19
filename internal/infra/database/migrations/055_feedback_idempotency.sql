-- Migration 055: idempotent ingest (#37).
--
-- An at-least-once client retry of POST /v1/feedback/ingest (lost response,
-- timeout, proxy 5xx) must not create a duplicate row. The dedup point is a
-- partial unique index on the optional client-supplied idempotency key: a
-- concurrent retry's INSERT blocks on the index until the first commits, then
-- hits the conflict and reads back the original id — atomic and race-free,
-- unlike an application-level check-then-insert.
--
-- idempotency_hash stores the request fingerprint so the same key reused with a
-- different body is reported as a conflict rather than silently deduped.

ALTER TABLE user_feedback ADD COLUMN IF NOT EXISTS idempotency_key TEXT;
ALTER TABLE user_feedback ADD COLUMN IF NOT EXISTS idempotency_hash BYTEA;

CREATE UNIQUE INDEX IF NOT EXISTS ux_user_feedback_idempotency
    ON user_feedback (tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
