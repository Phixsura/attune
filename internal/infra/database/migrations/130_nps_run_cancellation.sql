-- SPDX-License-Identifier: Apache-2.0
--
-- Permit an operator to safely cancel an NPS run before it has materialized
-- its recipient ledger.

ALTER TABLE survey_campaign_runs
    ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS cancelled_by TEXT NOT NULL DEFAULT '';

ALTER TABLE survey_campaign_runs
    DROP CONSTRAINT IF EXISTS chk_survey_campaign_runs_status,
    ADD CONSTRAINT chk_survey_campaign_runs_status
        CHECK (status IN ('scheduled', 'evaluating', 'collecting', 'closed', 'failed', 'cancelled')),
    ADD CONSTRAINT chk_survey_campaign_runs_cancellation
        CHECK (
            (status = 'cancelled' AND cancelled_at IS NOT NULL AND length(cancelled_by) BETWEEN 1 AND 256)
            OR (status <> 'cancelled' AND cancelled_at IS NULL AND cancelled_by = '')
        );
