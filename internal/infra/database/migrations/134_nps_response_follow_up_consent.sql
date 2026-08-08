-- SPDX-License-Identifier: Apache-2.0

-- Preserve a response-level NPS follow-up preference independently from the
-- contact's subscription state. Existing NPS responses remain unknown.
ALTER TABLE survey_responses
    ADD COLUMN IF NOT EXISTS follow_up_consent BOOLEAN;

ALTER TABLE survey_responses
    DROP CONSTRAINT IF EXISTS chk_survey_responses_follow_up_consent;
ALTER TABLE survey_responses
    ADD CONSTRAINT chk_survey_responses_follow_up_consent
        CHECK (survey_type = 'nps' OR follow_up_consent IS NULL);
