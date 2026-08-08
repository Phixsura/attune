-- SPDX-License-Identifier: Apache-2.0
--
-- Persist the target share of currently serviceable contacts covered by each
-- recurring relationship-NPS pulse.

ALTER TABLE survey_nps_campaign_settings
    ADD COLUMN IF NOT EXISTS recurrence_sampling_percent INT NOT NULL DEFAULT 25;

ALTER TABLE survey_nps_campaign_settings
    DROP CONSTRAINT IF EXISTS chk_survey_nps_campaign_settings_recurrence_sampling_percent,
    ADD CONSTRAINT chk_survey_nps_campaign_settings_recurrence_sampling_percent
        CHECK (recurrence_sampling_percent BETWEEN 1 AND 100);
