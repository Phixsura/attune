-- Migration 031: Batch operations and semantic search infrastructure.
--
-- Issue #30: Adds DDL for:
-- 1. updated_at / deleted_at columns on user_feedback for soft delete and
--    optimistic locking in batch updates
-- 2. idempotency_keys table for deduplicating batch ingestion requests
-- 3. batch_jobs table for async background processing with progress tracking
-- 4. query_embedding_cache for short-lived caching of search query embeddings
--
-- All statements use IF NOT EXISTS / IF EXISTS for idempotent re-runs.

-- ─── 1. user_feedback soft-delete and updated_at columns ──────────
ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- Auto-update updated_at on row modification.
CREATE OR REPLACE FUNCTION update_user_feedback_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_user_feedback_updated_at ON user_feedback;
CREATE TRIGGER trg_user_feedback_updated_at
    BEFORE UPDATE ON user_feedback
    FOR EACH ROW
    EXECUTE FUNCTION update_user_feedback_updated_at();

-- Partial index for efficiently querying soft-deleted rows per tenant.
CREATE INDEX IF NOT EXISTS idx_user_feedback_deleted
    ON user_feedback (tenant_id, deleted_at)
    WHERE deleted_at IS NOT NULL;

-- Composite index for semantic search filtered by workflow state + embedding model.
CREATE INDEX IF NOT EXISTS idx_uf_embedding_workflow
    ON user_feedback (tenant_id, workflow_state_id, embedding_model)
    WHERE embedding IS NOT NULL;

-- ─── 2. idempotency_keys for batch request deduplication ──────────
CREATE TABLE IF NOT EXISTS idempotency_keys (
    key          TEXT NOT NULL,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    request_hash BYTEA NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',
    response_code INTEGER,
    response_body JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '24 hours',

    PRIMARY KEY (tenant_id, key)
);

ALTER TABLE idempotency_keys ADD CONSTRAINT chk_idempotency_status
    CHECK (status IN ('pending', 'completed', 'failed'));
ALTER TABLE idempotency_keys ADD CONSTRAINT chk_idempotency_key_length
    CHECK (length(key) BETWEEN 8 AND 64);
ALTER TABLE idempotency_keys ADD CONSTRAINT chk_idempotency_key_chars
    CHECK (key ~ '^[a-zA-Z0-9_-]+$');

-- TTL cleanup index (expired keys can be garbage-collected).
CREATE INDEX IF NOT EXISTS idx_idempotency_keys_expires
    ON idempotency_keys (expires_at);

-- Count per tenant (for rate limiting / quota enforcement).
CREATE INDEX IF NOT EXISTS idx_idempotency_keys_tenant_count
    ON idempotency_keys (tenant_id, created_at);

-- ─── 3. batch_jobs for async background processing ────────────────
CREATE TABLE IF NOT EXISTS batch_jobs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'queued',
    request        JSONB NOT NULL,
    total          INTEGER NOT NULL DEFAULT 0,
    progress       INTEGER NOT NULL DEFAULT 0,
    result         JSONB,
    error          TEXT,
    created_by     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at     TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    claimed_at     TIMESTAMPTZ,
    last_heartbeat TIMESTAMPTZ
);

ALTER TABLE batch_jobs ADD CONSTRAINT chk_batch_jobs_status
    CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled'));

-- Pending jobs queue (workers poll this).
CREATE INDEX IF NOT EXISTS idx_batch_jobs_pending
    ON batch_jobs (status, created_at)
    WHERE status IN ('queued', 'running');

-- Per-tenant job history listing.
CREATE INDEX IF NOT EXISTS idx_batch_jobs_tenant
    ON batch_jobs (tenant_id, created_at DESC);

-- Stale heartbeat detection (orphan job recovery).
CREATE INDEX IF NOT EXISTS idx_batch_jobs_heartbeat
    ON batch_jobs (last_heartbeat)
    WHERE status = 'running';

-- ─── 4. query_embedding_cache for semantic search ─────────────────
CREATE TABLE IF NOT EXISTS query_embedding_cache (
    cache_key    TEXT PRIMARY KEY,
    embedding    vector(256) NOT NULL,
    model        TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at   TIMESTAMPTZ NOT NULL DEFAULT NOW() + INTERVAL '5 minutes'
);

-- TTL cleanup index.
CREATE INDEX IF NOT EXISTS idx_query_embed_cache_expires
    ON query_embedding_cache (expires_at);
