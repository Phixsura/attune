-- SPDX-License-Identifier: Apache-2.0
--
-- Persist exact NPS evidence artifacts so operators can reproduce and
-- re-download the report that was actually generated.

CREATE TABLE IF NOT EXISTS survey_nps_run_evidence_exports (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    campaign_id     UUID NOT NULL,
    run_id          UUID NOT NULL,
    report_version  TEXT NOT NULL,
    generated_at    TIMESTAMPTZ NOT NULL,
    artifact        BYTEA NOT NULL,
    artifact_sha256 TEXT NOT NULL,
    created_by_type TEXT NOT NULL,
    created_by      TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    downloaded_at   TIMESTAMPTZ,
    CONSTRAINT fk_survey_nps_run_evidence_exports_run
        FOREIGN KEY (tenant_id, run_id, campaign_id)
        REFERENCES survey_campaign_runs(tenant_id, id, campaign_id)
        ON DELETE CASCADE,
    CONSTRAINT chk_survey_nps_run_evidence_exports_report_version
        CHECK (length(report_version) BETWEEN 1 AND 32),
    CONSTRAINT chk_survey_nps_run_evidence_exports_artifact_size
        CHECK (octet_length(artifact) BETWEEN 1 AND 1048576),
    CONSTRAINT chk_survey_nps_run_evidence_exports_artifact_sha256
        CHECK (artifact_sha256 ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT chk_survey_nps_run_evidence_exports_created_by_type
        CHECK (length(created_by_type) BETWEEN 1 AND 64),
    CONSTRAINT chk_survey_nps_run_evidence_exports_created_by
        CHECK (length(created_by) BETWEEN 1 AND 256)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_survey_nps_run_evidence_exports_tenant_id
    ON survey_nps_run_evidence_exports (tenant_id, id);
CREATE INDEX IF NOT EXISTS idx_survey_nps_run_evidence_exports_history
    ON survey_nps_run_evidence_exports (tenant_id, campaign_id, run_id, generated_at DESC, id DESC);
