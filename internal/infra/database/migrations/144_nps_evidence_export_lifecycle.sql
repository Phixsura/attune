-- Give persisted NPS evidence exports a replay identity and bounded retention.
-- The artifact remains immutable, while lifecycle metadata governs whether a
-- historical snapshot is still downloadable.

ALTER TABLE survey_nps_run_evidence_exports
    ADD COLUMN IF NOT EXISTS client_request_key UUID,
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

UPDATE survey_nps_run_evidence_exports
SET client_request_key = gen_random_uuid()
WHERE client_request_key IS NULL;

UPDATE survey_nps_run_evidence_exports
SET expires_at = created_at + INTERVAL '30 days'
WHERE expires_at IS NULL;

ALTER TABLE survey_nps_run_evidence_exports
    ALTER COLUMN client_request_key SET DEFAULT gen_random_uuid(),
    ALTER COLUMN client_request_key SET NOT NULL,
    ALTER COLUMN expires_at SET DEFAULT (NOW() + INTERVAL '30 days'),
    ALTER COLUMN expires_at SET NOT NULL;

ALTER TABLE survey_nps_run_evidence_exports
    ADD CONSTRAINT chk_survey_nps_run_evidence_exports_expiry
    CHECK (expires_at > created_at);

CREATE UNIQUE INDEX IF NOT EXISTS uq_survey_nps_run_evidence_exports_request
    ON survey_nps_run_evidence_exports (tenant_id, campaign_id, run_id, client_request_key);
CREATE INDEX IF NOT EXISTS idx_survey_nps_run_evidence_exports_expiry
    ON survey_nps_run_evidence_exports (expires_at)
    WHERE expires_at IS NOT NULL;
