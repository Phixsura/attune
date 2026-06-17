-- SPDX-License-Identifier: Apache-2.0

ALTER TABLE gdpr_requests
    ADD COLUMN IF NOT EXISTS execute_after TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ;

ALTER TABLE gdpr_requests
    DROP CONSTRAINT IF EXISTS chk_gdpr_requests_status;

ALTER TABLE gdpr_requests
    ADD CONSTRAINT chk_gdpr_requests_status CHECK (
        status IN ('queued', 'running', 'ready', 'downloaded', 'completed', 'failed', 'expired', 'scheduled', 'cancelled')
    );

CREATE INDEX IF NOT EXISTS idx_gdpr_requests_delete_schedule
    ON gdpr_requests (status, execute_after, created_at ASC)
    WHERE request_type = 'delete' AND status = 'scheduled' AND execute_after IS NOT NULL;
