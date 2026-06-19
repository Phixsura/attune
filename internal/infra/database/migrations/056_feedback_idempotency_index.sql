-- migrate:no-transaction
--
-- Migration 056: idempotent ingest — dedup index (#37).
--
-- Partial unique index that makes a retried ingest atomic: a concurrent retry's
-- INSERT blocks on this index until the first commits, then hits ON CONFLICT and
-- reads back the original id (see repo/feedback.InsertIdempotent). Built
-- CONCURRENTLY so it does NOT lock writes (ingest) on a large existing
-- user_feedback table — which requires running outside a transaction (the
-- no-transaction directive on line 1; this file is therefore a SINGLE statement).
--
-- Idempotent re-run: IF NOT EXISTS skips a committed index. If a prior build was
-- interrupted it can leave an INVALID index of this name — recover by
-- `DROP INDEX CONCURRENTLY ux_user_feedback_idempotency;` then redeploy.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS ux_user_feedback_idempotency
    ON user_feedback (tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
