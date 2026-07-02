-- Migration 095: add search relevance operations telemetry.

CREATE TABLE IF NOT EXISTS feedback_search_runs (
    id BIGSERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    run_id UUID NOT NULL,
    query_hash TEXT NOT NULL CHECK (length(query_hash) = 64),
    query_preview TEXT NOT NULL DEFAULT '',
    filter_hash TEXT NOT NULL DEFAULT '',
    ranking_version TEXT NOT NULL DEFAULT '',
    embedding_model TEXT NOT NULL DEFAULT '',
    result_count INTEGER NOT NULL DEFAULT 0 CHECK (result_count >= 0),
    used_keyword_fallback BOOLEAN NOT NULL DEFAULT FALSE,
    fallback_reason TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
    total_live_feedback INTEGER NOT NULL DEFAULT 0 CHECK (total_live_feedback >= 0),
    total_with_embeddings INTEGER NOT NULL DEFAULT 0 CHECK (total_with_embeddings >= 0),
    coverage_ratio DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (coverage_ratio >= 0 AND coverage_ratio <= 1),
    actor_user_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, run_id)
);

CREATE INDEX IF NOT EXISTS idx_feedback_search_runs_tenant_created
    ON feedback_search_runs (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_feedback_search_runs_tenant_query
    ON feedback_search_runs (tenant_id, query_hash, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_feedback_search_runs_tenant_zero
    ON feedback_search_runs (tenant_id, created_at DESC)
    WHERE result_count = 0;

CREATE INDEX IF NOT EXISTS idx_feedback_search_runs_tenant_fallback
    ON feedback_search_runs (tenant_id, created_at DESC)
    WHERE used_keyword_fallback;

CREATE TABLE IF NOT EXISTS feedback_search_result_events (
    id BIGSERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    run_id UUID NOT NULL,
    feedback_id BIGINT NOT NULL CHECK (feedback_id > 0),
    action TEXT NOT NULL CHECK (action IN ('impression', 'open', 'copy', 'transition', 'retry')),
    rank INTEGER NOT NULL DEFAULT 0 CHECK (rank >= 0),
    match_type TEXT NOT NULL DEFAULT '',
    actor_user_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (tenant_id, run_id)
        REFERENCES feedback_search_runs (tenant_id, run_id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_feedback_search_result_events_tenant_created
    ON feedback_search_result_events (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_feedback_search_result_events_tenant_run
    ON feedback_search_result_events (tenant_id, run_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_feedback_search_result_events_tenant_feedback
    ON feedback_search_result_events (tenant_id, feedback_id, created_at DESC);

CREATE TABLE IF NOT EXISTS feedback_search_ranking_versions (
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    ranking_version TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('draft', 'shadow', 'canary', 'active', 'disabled')),
    traffic_percent INTEGER NOT NULL DEFAULT 100 CHECK (traffic_percent >= 0 AND traffic_percent <= 100),
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    notes TEXT NOT NULL DEFAULT '',
    activated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, ranking_version)
);

CREATE INDEX IF NOT EXISTS idx_feedback_search_ranking_versions_tenant_status
    ON feedback_search_ranking_versions (tenant_id, status, updated_at DESC);
