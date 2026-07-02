-- Migration 096: add the control-tower quality action ledger.

CREATE TABLE IF NOT EXISTS feedback_quality_actions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id          TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    action_key         TEXT NOT NULL,
    signal             TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'open',
    severity           TEXT NOT NULL DEFAULT 'watch',
    target_path        TEXT NOT NULL DEFAULT '',
    metric_label       TEXT NOT NULL DEFAULT '',
    metric_value       TEXT NOT NULL DEFAULT '',
    recommendation_key TEXT NOT NULL DEFAULT '',
    evidence           JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    first_seen_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_at    TIMESTAMPTZ,
    resolved_at        TIMESTAMPTZ,
    dismissed_at       TIMESTAMPTZ,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by         TEXT NOT NULL DEFAULT '',
    UNIQUE (tenant_id, action_key),
    CHECK (length(action_key) BETWEEN 1 AND 120),
    CHECK (length(signal) BETWEEN 1 AND 80),
    CHECK (status IN ('open', 'acknowledged', 'resolved', 'dismissed')),
    CHECK (severity IN ('alert', 'watch', 'normal', 'insufficient_data'))
);

CREATE INDEX IF NOT EXISTS idx_feedback_quality_actions_tenant_status
    ON feedback_quality_actions (tenant_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_feedback_quality_actions_tenant_signal
    ON feedback_quality_actions (tenant_id, signal, updated_at DESC);
