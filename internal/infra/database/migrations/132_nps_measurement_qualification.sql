-- SPDX-License-Identifier: Apache-2.0
--
-- Frozen operator-defined NPS measurement evidence thresholds (#236).

ALTER TABLE survey_nps_campaign_settings
    ADD COLUMN IF NOT EXISTS minimum_completed_responses INT NOT NULL DEFAULT 30,
    ADD COLUMN IF NOT EXISTS minimum_response_rate_percent INT NOT NULL DEFAULT 10;

ALTER TABLE survey_nps_campaign_settings
    ADD CONSTRAINT chk_survey_nps_campaign_settings_minimum_completed_responses
        CHECK (minimum_completed_responses BETWEEN 1 AND 100000),
    ADD CONSTRAINT chk_survey_nps_campaign_settings_minimum_response_rate_percent
        CHECK (minimum_response_rate_percent BETWEEN 1 AND 100);

-- Preserve a concrete qualification policy for runs scheduled by an earlier
-- binary before the new frozen-definition keys existed.
UPDATE survey_campaign_runs
SET definition_snapshot = jsonb_set(
        jsonb_set(
            definition_snapshot,
            '{minimum_completed_responses}',
            '30'::jsonb,
            true
        ),
        '{minimum_response_rate_percent}',
        '10'::jsonb,
        true
    )
WHERE NOT (definition_snapshot ? 'minimum_completed_responses')
   OR NOT (definition_snapshot ? 'minimum_response_rate_percent');
