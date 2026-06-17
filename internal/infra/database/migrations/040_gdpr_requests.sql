-- SPDX-License-Identifier: Apache-2.0

CREATE TABLE IF NOT EXISTS gdpr_requests (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    request_type TEXT NOT NULL,
    status TEXT NOT NULL,
    subject_key TEXT NOT NULL,
    subject_hash TEXT NOT NULL,
    subject_display TEXT NOT NULL DEFAULT '',
    feedback_count INTEGER NOT NULL DEFAULT 0,
    tag_assignment_count INTEGER NOT NULL DEFAULT 0,
    feedback_audit_count INTEGER NOT NULL DEFAULT 0,
    llm_audit_count INTEGER NOT NULL DEFAULT 0,
    archive_filename TEXT NOT NULL DEFAULT '',
    error TEXT,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    downloaded_at TIMESTAMPTZ,
    CONSTRAINT chk_gdpr_requests_type CHECK (request_type IN ('export', 'delete')),
    CONSTRAINT chk_gdpr_requests_status CHECK (status IN ('queued', 'running', 'ready', 'downloaded', 'completed', 'failed', 'expired')),
    CONSTRAINT chk_gdpr_requests_subject_key_length CHECK (length(subject_key) >= 1 AND length(subject_key) <= 512),
    CONSTRAINT chk_gdpr_requests_subject_hash_length CHECK (length(subject_hash) = 64),
    CONSTRAINT chk_gdpr_requests_feedback_count CHECK (feedback_count >= 0),
    CONSTRAINT chk_gdpr_requests_tag_assignment_count CHECK (tag_assignment_count >= 0),
    CONSTRAINT chk_gdpr_requests_feedback_audit_count CHECK (feedback_audit_count >= 0),
    CONSTRAINT chk_gdpr_requests_llm_audit_count CHECK (llm_audit_count >= 0)
);

CREATE INDEX IF NOT EXISTS idx_gdpr_requests_tenant_created
    ON gdpr_requests (tenant_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_gdpr_requests_tenant_status_created
    ON gdpr_requests (tenant_id, status, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_gdpr_requests_export_expiry
    ON gdpr_requests (expires_at)
    WHERE request_type = 'export' AND status IN ('ready', 'downloaded') AND expires_at IS NOT NULL;

ALTER TABLE gdpr_export_jobs
    ADD COLUMN IF NOT EXISTS request_id TEXT;

INSERT INTO gdpr_requests (
    id,
    tenant_id,
    request_type,
    status,
    subject_key,
    subject_hash,
    subject_display,
    feedback_count,
    tag_assignment_count,
    feedback_audit_count,
    llm_audit_count,
    archive_filename,
    error,
    created_by,
    created_at,
    started_at,
    completed_at,
    expires_at,
    downloaded_at
)
SELECT
    j.id,
    j.tenant_id,
    'export',
    CASE
        WHEN j.status = 'completed' AND j.downloaded_at IS NOT NULL THEN 'downloaded'
        WHEN j.status = 'completed' THEN 'ready'
        ELSE j.status
    END,
    j.subject_key,
    j.subject_hash,
    COALESCE(j.subject_display, ''),
    j.feedback_count,
    j.tag_assignment_count,
    j.feedback_audit_count,
    j.llm_audit_count,
    COALESCE(j.archive_filename, ''),
    j.error,
    j.created_by,
    j.created_at,
    j.started_at,
    j.completed_at,
    j.expires_at,
    j.downloaded_at
FROM gdpr_export_jobs j
LEFT JOIN gdpr_requests r ON r.id = j.id
WHERE r.id IS NULL;

UPDATE gdpr_export_jobs
SET request_id = id
WHERE request_id IS NULL;

ALTER TABLE gdpr_export_jobs
    ALTER COLUMN request_id SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_gdpr_export_jobs_request'
    ) THEN
        ALTER TABLE gdpr_export_jobs
            ADD CONSTRAINT fk_gdpr_export_jobs_request
            FOREIGN KEY (request_id) REFERENCES gdpr_requests(id) ON DELETE CASCADE;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_gdpr_export_jobs_request_id
    ON gdpr_export_jobs (request_id);
