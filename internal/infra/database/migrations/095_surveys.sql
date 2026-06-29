-- Migration 095: Add surveys infrastructure (#202 NPS/CSAT/CES).
--
-- Tenants can create surveys with configurable type (nps/csat/ces),
-- questions, and delivery rules. Responses are linked back to feedback
-- items for closed-loop measurement.

CREATE TABLE IF NOT EXISTS tenant_surveys (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT         NOT NULL,
    survey_type TEXT         NOT NULL
        CHECK (survey_type IN ('nps', 'csat', 'ces')),
    question    TEXT         NOT NULL,
    enabled     BOOLEAN      NOT NULL DEFAULT TRUE,
    config      JSONB        NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_surveys_tenant
    ON tenant_surveys (tenant_id);

CREATE TABLE IF NOT EXISTS survey_responses (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT         NOT NULL,
    survey_id   UUID         NOT NULL REFERENCES tenant_surveys(id) ON DELETE CASCADE,
    feedback_id BIGINT,
    score       INT          NOT NULL,
    comment     TEXT         NOT NULL DEFAULT '',
    respondent  TEXT         NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_survey_responses_survey
    ON survey_responses (survey_id);

CREATE INDEX IF NOT EXISTS idx_survey_responses_tenant
    ON survey_responses (tenant_id);
