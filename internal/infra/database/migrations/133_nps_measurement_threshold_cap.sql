-- SPDX-License-Identifier: Apache-2.0
--
-- A completed-response threshold above the per-run invitation cap can never
-- be reached, regardless of the response rate.

ALTER TABLE survey_nps_campaign_settings
    ADD CONSTRAINT chk_survey_nps_campaign_settings_minimum_completed_within_cap
        CHECK (minimum_completed_responses <= maximum_run_recipients);
