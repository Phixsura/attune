-- LLM confidence and cost observability (#24).
--
-- user_feedback keeps the current, fast-query classification snapshot.
-- llm_audit is the call-level fact table used for cost aggregation.

ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS classification_confidence DOUBLE PRECISION;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_user_feedback_classification_confidence'
          AND conrelid = 'user_feedback'::regclass
    ) THEN
        ALTER TABLE user_feedback
            ADD CONSTRAINT chk_user_feedback_classification_confidence
            CHECK (
                classification_confidence IS NULL
                OR (classification_confidence >= 0 AND classification_confidence <= 1)
            );
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_user_feedback_tenant_confidence_created
    ON user_feedback (tenant_id, classification_confidence, created_at DESC)
    WHERE classification_confidence IS NOT NULL;

CREATE TABLE IF NOT EXISTS llm_audit (
    id                BIGSERIAL PRIMARY KEY,
    tenant_id         TEXT NOT NULL DEFAULT '',
    feedback_id       BIGINT,
    inbound_trace_id  TEXT NOT NULL DEFAULT '',
    otel_trace_id     TEXT NOT NULL DEFAULT '',
    model_id          TEXT NOT NULL DEFAULT '',
    purpose           TEXT NOT NULL DEFAULT '',
    prompt_tokens     INTEGER NOT NULL DEFAULT 0 CHECK (prompt_tokens >= 0),
    completion_tokens INTEGER NOT NULL DEFAULT 0 CHECK (completion_tokens >= 0),
    cost_usd          NUMERIC(14, 8) NOT NULL DEFAULT 0 CHECK (cost_usd >= 0),
    status            TEXT NOT NULL DEFAULT 'ok',
    error             TEXT NOT NULL DEFAULT '',
    latency_ms        INTEGER NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (status IN ('ok', 'error'))
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_llm_audit_feedback'
          AND conrelid = 'llm_audit'::regclass
    ) THEN
        ALTER TABLE llm_audit
            ADD CONSTRAINT fk_llm_audit_feedback
            FOREIGN KEY (feedback_id)
            REFERENCES user_feedback(id)
            ON DELETE SET NULL
            NOT VALID;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_llm_audit_tenant_created
    ON llm_audit (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_llm_audit_tenant_model_created
    ON llm_audit (tenant_id, model_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_llm_audit_feedback_created
    ON llm_audit (feedback_id, created_at DESC)
    WHERE feedback_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_llm_audit_tenant_inbound_trace
    ON llm_audit (tenant_id, inbound_trace_id)
    WHERE inbound_trace_id <> '';

CREATE INDEX IF NOT EXISTS idx_llm_audit_tenant_otel_trace
    ON llm_audit (tenant_id, otel_trace_id)
    WHERE otel_trace_id <> '';
