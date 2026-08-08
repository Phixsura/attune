-- Enforce the content-addressed contract for persisted NPS evidence exports.
-- The checksum is part of the audit evidence, so format validation alone is
-- insufficient: the database must reject a mismatched artifact or digest.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM survey_nps_run_evidence_exports
        WHERE artifact_sha256 <> 'sha256:' || encode(digest(artifact, 'sha256'), 'hex')
    ) THEN
        RAISE EXCEPTION 'existing NPS evidence export has an invalid artifact digest';
    END IF;
END $$;

CREATE OR REPLACE FUNCTION verify_survey_nps_run_evidence_export_artifact()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.artifact_sha256 <> 'sha256:' || encode(digest(NEW.artifact, 'sha256'), 'hex') THEN
        RAISE EXCEPTION 'NPS evidence export artifact digest does not match content'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_survey_nps_run_evidence_export_integrity
    ON survey_nps_run_evidence_exports;

CREATE TRIGGER trg_survey_nps_run_evidence_export_integrity
BEFORE INSERT OR UPDATE OF artifact, artifact_sha256
ON survey_nps_run_evidence_exports
FOR EACH ROW
EXECUTE FUNCTION verify_survey_nps_run_evidence_export_artifact();
