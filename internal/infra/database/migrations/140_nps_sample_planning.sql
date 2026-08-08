-- SPDX-License-Identifier: Apache-2.0
--
-- Persist the operator's conservative NPS sample-planning assumptions.

ALTER TABLE survey_nps_campaign_settings
    ADD COLUMN IF NOT EXISTS sample_planning_confidence_percent INT NOT NULL DEFAULT 95,
    ADD COLUMN IF NOT EXISTS sample_planning_margin_of_error_percent INT NOT NULL DEFAULT 10,
    ADD COLUMN IF NOT EXISTS sample_planning_expected_response_rate_percent INT NOT NULL DEFAULT 20;

ALTER TABLE survey_nps_campaign_settings
    DROP CONSTRAINT IF EXISTS chk_survey_nps_campaign_settings_sample_planning_confidence,
    ADD CONSTRAINT chk_survey_nps_campaign_settings_sample_planning_confidence
        CHECK (sample_planning_confidence_percent IN (90, 95, 99)),
    DROP CONSTRAINT IF EXISTS chk_survey_nps_campaign_settings_sample_planning_margin_of_error,
    ADD CONSTRAINT chk_survey_nps_campaign_settings_sample_planning_margin_of_error
        CHECK (sample_planning_margin_of_error_percent BETWEEN 1 AND 25),
    DROP CONSTRAINT IF EXISTS chk_survey_nps_campaign_settings_sample_planning_response_rate,
    ADD CONSTRAINT chk_survey_nps_campaign_settings_sample_planning_response_rate
        CHECK (sample_planning_expected_response_rate_percent BETWEEN 1 AND 100);
