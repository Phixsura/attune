-- SPDX-License-Identifier: Apache-2.0
--
-- Persist an optional relationship-NPS pulse cadence and its recovery state.

ALTER TABLE survey_nps_campaign_settings
    ADD COLUMN IF NOT EXISTS recurrence_interval_days INT NOT NULL DEFAULT 0;

ALTER TABLE survey_nps_campaign_settings
    DROP CONSTRAINT IF EXISTS chk_survey_nps_campaign_settings_recurrence_interval,
    ADD CONSTRAINT chk_survey_nps_campaign_settings_recurrence_interval
        CHECK (recurrence_interval_days = 0 OR recurrence_interval_days BETWEEN 30 AND 365);

ALTER TABLE survey_campaign_runs
    ADD COLUMN IF NOT EXISTS recurrence_source_run_id UUID,
    ADD COLUMN IF NOT EXISTS recurrence_claimed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS recurrence_claimed_by TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS recurrence_processed_at TIMESTAMPTZ;

ALTER TABLE survey_campaign_runs
    DROP CONSTRAINT IF EXISTS fk_survey_campaign_runs_recurrence_source,
    ADD CONSTRAINT fk_survey_campaign_runs_recurrence_source
        FOREIGN KEY (tenant_id, recurrence_source_run_id, campaign_id)
        REFERENCES survey_campaign_runs(tenant_id, id, campaign_id)
        ON DELETE CASCADE;

ALTER TABLE survey_campaign_runs
    DROP CONSTRAINT IF EXISTS chk_survey_campaign_runs_recurrence_claimed_by_length,
    ADD CONSTRAINT chk_survey_campaign_runs_recurrence_claimed_by_length
        CHECK (length(recurrence_claimed_by) <= 256);

CREATE UNIQUE INDEX IF NOT EXISTS uq_survey_campaign_runs_recurrence_source
    ON survey_campaign_runs (tenant_id, campaign_id, recurrence_source_run_id)
    WHERE recurrence_source_run_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_survey_campaign_runs_recurrence_due
    ON survey_campaign_runs (closes_at ASC, created_at ASC, id ASC)
    WHERE status = 'closed' AND recurrence_processed_at IS NULL;
