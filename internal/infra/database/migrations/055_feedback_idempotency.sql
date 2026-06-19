-- Migration 055: idempotent ingest — columns (#37).
--
-- An at-least-once client retry of POST /v1/feedback/ingest (lost response,
-- timeout, proxy 5xx) must not create a duplicate row. These columns hold the
-- optional client idempotency key and the request fingerprint; the dedup unique
-- index is built separately in 056 (CONCURRENTLY, so it cannot live here — a
-- transactional migration cannot run CREATE INDEX CONCURRENTLY).
--
-- ADD COLUMN ... (nullable, no default) is metadata-only on PostgreSQL 11+, so
-- this migration does not rewrite or long-lock the table.

ALTER TABLE user_feedback ADD COLUMN IF NOT EXISTS idempotency_key TEXT;
ALTER TABLE user_feedback ADD COLUMN IF NOT EXISTS idempotency_hash BYTEA;
