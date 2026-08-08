-- SPDX-License-Identifier: Apache-2.0
--
-- Separate relationship-NPS program cadence from per-contact survey cadence.

ALTER TABLE survey_nps_campaign_settings
    ADD COLUMN IF NOT EXISTS recurrence_contact_cooldown_days INT NOT NULL DEFAULT 365;

ALTER TABLE survey_nps_campaign_settings
    DROP CONSTRAINT IF EXISTS chk_survey_nps_campaign_settings_recurrence_contact_cooldown,
    ADD CONSTRAINT chk_survey_nps_campaign_settings_recurrence_contact_cooldown
        CHECK (recurrence_contact_cooldown_days BETWEEN 30 AND 3650);
