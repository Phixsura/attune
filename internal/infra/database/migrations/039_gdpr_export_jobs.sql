-- Async GDPR export jobs (#43 follow-up hardening).
--
-- Moves GDPR export generation off the interactive request path and stores a
-- short-lived archive artifact for authenticated download.

CREATE TABLE IF NOT EXISTS gdpr_export_jobs (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE RESTRICT,
    subject_key TEXT NOT NULL,
    subject_hash TEXT NOT NULL,
    subject_display TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    archive BYTEA,
    archive_filename TEXT NOT NULL DEFAULT '',
    feedback_count INTEGER NOT NULL DEFAULT 0,
    tag_assignment_count INTEGER NOT NULL DEFAULT 0,
    feedback_audit_count INTEGER NOT NULL DEFAULT 0,
    llm_audit_count INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    downloaded_at TIMESTAMPTZ,
    claimed_at TIMESTAMPTZ,
    last_heartbeat TIMESTAMPTZ,
    CONSTRAINT chk_gdpr_export_jobs_status CHECK (
        status IN ('queued', 'running', 'completed', 'failed', 'expired')
    ),
    CONSTRAINT chk_gdpr_export_jobs_subject_key_length CHECK (length(subject_key) >= 1 AND length(subject_key) <= 512),
    CONSTRAINT chk_gdpr_export_jobs_subject_hash_length CHECK (length(subject_hash) = 64),
    CONSTRAINT chk_gdpr_export_jobs_feedback_count CHECK (feedback_count >= 0),
    CONSTRAINT chk_gdpr_export_jobs_tag_assignment_count CHECK (tag_assignment_count >= 0),
    CONSTRAINT chk_gdpr_export_jobs_feedback_audit_count CHECK (feedback_audit_count >= 0),
    CONSTRAINT chk_gdpr_export_jobs_llm_audit_count CHECK (llm_audit_count >= 0)
);

CREATE INDEX IF NOT EXISTS idx_gdpr_export_jobs_tenant_created
    ON gdpr_export_jobs (tenant_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_gdpr_export_jobs_status_created
    ON gdpr_export_jobs (status, created_at ASC)
    WHERE status = 'queued';

CREATE INDEX IF NOT EXISTS idx_gdpr_export_jobs_expiry
    ON gdpr_export_jobs (expires_at)
    WHERE status = 'completed' AND expires_at IS NOT NULL;
