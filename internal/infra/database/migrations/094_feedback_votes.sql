-- Migration 094: Add feedback_votes table (#202 public voting portal).
--
-- Tracks upvotes on feedback items from the public portal. Votes are
-- identified by a browser fingerprint (hashed) to allow one-vote-per-visitor
-- without requiring authentication.

CREATE TABLE IF NOT EXISTS feedback_votes (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT        NOT NULL,
    feedback_id     BIGINT      NOT NULL,
    voter_hash      TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, feedback_id, voter_hash)
);

CREATE INDEX IF NOT EXISTS idx_feedback_votes_feedback
    ON feedback_votes (tenant_id, feedback_id);
