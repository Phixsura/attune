-- Preserve the original recovery target plus first contact and terminal
-- disposition as auditable facts. Legacy reviews intentionally retain unknowns.
ALTER TABLE survey_low_score_reviews
    ADD COLUMN IF NOT EXISTS initial_due_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS customer_contacted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS first_terminal_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS terminal_timeliness_unknown BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE survey_low_score_reviews
SET terminal_timeliness_unknown = TRUE
WHERE initial_due_at IS NULL;

ALTER TABLE survey_low_score_reviews
    DROP CONSTRAINT IF EXISTS chk_survey_low_score_reviews_contact_timestamp;
ALTER TABLE survey_low_score_reviews
    ADD CONSTRAINT chk_survey_low_score_reviews_contact_timestamp
    CHECK (customer_contacted_at IS NULL OR customer_contacted);

CREATE OR REPLACE FUNCTION preserve_survey_low_score_review_timeliness()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.initial_due_at IS NULL AND NEW.initial_due_at IS NOT NULL THEN
        RAISE EXCEPTION 'survey_low_score_reviews.initial_due_at is insert-only';
    END IF;
    IF OLD.initial_due_at IS NOT NULL
       AND NEW.initial_due_at IS DISTINCT FROM OLD.initial_due_at THEN
        RAISE EXCEPTION 'survey_low_score_reviews.initial_due_at is immutable';
    END IF;
    IF OLD.customer_contacted AND NOT NEW.customer_contacted THEN
        RAISE EXCEPTION 'survey_low_score_reviews.customer_contacted is monotonic';
    END IF;
    IF NOT OLD.customer_contacted
       AND NEW.customer_contacted
       AND NEW.customer_contacted_at IS NULL THEN
        RAISE EXCEPTION 'survey_low_score_reviews.customer_contacted_at is required for new contact evidence';
    END IF;
    IF OLD.customer_contacted_at IS NOT NULL
       AND NEW.customer_contacted_at IS DISTINCT FROM OLD.customer_contacted_at THEN
        RAISE EXCEPTION 'survey_low_score_reviews.customer_contacted_at is immutable';
    END IF;
    IF OLD.customer_contacted
       AND OLD.customer_contacted_at IS NULL
       AND NEW.customer_contacted_at IS NOT NULL THEN
        RAISE EXCEPTION 'survey_low_score_reviews.customer_contacted_at remains unknown for historical contact evidence';
    END IF;
    IF OLD.terminal_timeliness_unknown IS DISTINCT FROM NEW.terminal_timeliness_unknown THEN
        RAISE EXCEPTION 'survey_low_score_reviews.terminal_timeliness_unknown is immutable';
    END IF;
    IF OLD.first_terminal_at IS NOT NULL
       AND NEW.first_terminal_at IS DISTINCT FROM OLD.first_terminal_at THEN
        RAISE EXCEPTION 'survey_low_score_reviews.first_terminal_at is immutable';
    END IF;
    IF OLD.terminal_timeliness_unknown
       AND OLD.first_terminal_at IS NULL
       AND NEW.first_terminal_at IS NOT NULL THEN
        RAISE EXCEPTION 'survey_low_score_reviews.first_terminal_at remains unknown for historical terminal evidence';
    END IF;
    IF OLD.first_terminal_at IS NULL
       AND NEW.first_terminal_at IS NOT NULL
       AND NOT (
           OLD.status NOT IN ('resolved', 'dismissed')
           AND NEW.status IN ('resolved', 'dismissed')
       ) THEN
        RAISE EXCEPTION 'survey_low_score_reviews.first_terminal_at requires a new terminal disposition';
    END IF;
    IF OLD.status NOT IN ('resolved', 'dismissed')
       AND NEW.status IN ('resolved', 'dismissed')
       AND NOT NEW.terminal_timeliness_unknown
       AND NEW.first_terminal_at IS NULL THEN
        RAISE EXCEPTION 'survey_low_score_reviews.first_terminal_at is required for a new terminal disposition';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_survey_low_score_reviews_preserve_timeliness ON survey_low_score_reviews;
CREATE TRIGGER trg_survey_low_score_reviews_preserve_timeliness
    BEFORE UPDATE ON survey_low_score_reviews
    FOR EACH ROW
    EXECUTE FUNCTION preserve_survey_low_score_review_timeliness();
