-- Embedding-based feedback clustering (#25).
--
-- Groups semantically similar feedback via vector similarity. Uses pgvector
-- for efficient nearest-neighbor search with HNSW indexing.
--
-- Design notes:
-- - 256 dimensions via Matryoshka truncation (~95% quality, 6x storage savings)
-- - embedding_model tracks which model produced the vector (cross-model
--   comparison is garbage; similarity search filters to same model)
-- - embedding_task table implements outbox pattern for reliable async processing
-- - clustering_enabled per-tenant feature flag

-- 1. Enable pgvector extension (requires >= 0.5.0 for HNSW)
CREATE EXTENSION IF NOT EXISTS vector;

-- 2. Add embedding columns to user_feedback
ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS embedding         vector(256),
    ADD COLUMN IF NOT EXISTS embedding_model   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS embedding_dims    INTEGER,
    ADD COLUMN IF NOT EXISTS embedded_at       TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS cluster_id        UUID,
    ADD COLUMN IF NOT EXISTS cluster_label     TEXT,
    ADD COLUMN IF NOT EXISTS cluster_assigned_at TIMESTAMPTZ;

-- HNSW index for cosine similarity (probes ~200 candidates per query).
-- m=24 balances recall vs memory; ef_construction=100 for good graph quality.
CREATE INDEX IF NOT EXISTS idx_user_feedback_embedding_hnsw
    ON user_feedback
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 24, ef_construction = 100)
    WHERE embedding IS NOT NULL;

-- Cluster lookup: list clusters by recency, filter by tenant.
CREATE INDEX IF NOT EXISTS idx_user_feedback_cluster
    ON user_feedback (tenant_id, cluster_id, created_at DESC)
    WHERE cluster_id IS NOT NULL;

-- Model filter for same-model similarity search.
CREATE INDEX IF NOT EXISTS idx_user_feedback_embedding_model
    ON user_feedback (tenant_id, embedding_model, created_at DESC)
    WHERE embedding IS NOT NULL AND embedding_model <> '';

-- 3. Embedding task outbox table (reliable async processing)
CREATE TABLE IF NOT EXISTS embedding_task (
    id              BIGSERIAL PRIMARY KEY,
    feedback_id     BIGINT NOT NULL REFERENCES user_feedback(id) ON DELETE CASCADE,
    tenant_id       TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    claimed_at      TIMESTAMPTZ,
    next_retry_at   TIMESTAMPTZ,
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    UNIQUE (feedback_id)
);

CREATE INDEX IF NOT EXISTS idx_embedding_task_pending
    ON embedding_task (status, next_retry_at, created_at)
    WHERE status IN ('pending', 'failed');

CREATE INDEX IF NOT EXISTS idx_embedding_task_tenant_status
    ON embedding_task (tenant_id, status, created_at DESC);

-- 4. Add clustering config to tenants table
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS clustering_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS clustering_threshold DOUBLE PRECISION NOT NULL DEFAULT 0.75;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_tenants_clustering_threshold'
          AND conrelid = 'tenants'::regclass
    ) THEN
        ALTER TABLE tenants
            ADD CONSTRAINT chk_tenants_clustering_threshold
            CHECK (
                clustering_threshold >= 0.5 AND clustering_threshold <= 1.0
            );
    END IF;
END $$;
