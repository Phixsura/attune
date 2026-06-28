-- Preserve audit evidence export actor types across async worker handoff and
-- evidence-manifest generation.

ALTER TABLE audit_evidence_export
    ADD COLUMN IF NOT EXISTS created_by_type TEXT;

UPDATE audit_evidence_export e
SET created_by_type = CASE
    WHEN e.created_by LIKE 'apikey:%' THEN 'api_key'
    WHEN EXISTS (SELECT 1 FROM oidc_users ou WHERE ou.id = e.created_by) THEN 'oidc'
    ELSE 'admin'
END
WHERE e.created_by_type IS NULL OR btrim(e.created_by_type) = '';

ALTER TABLE audit_evidence_export
    ALTER COLUMN created_by_type SET NOT NULL;

ALTER TABLE audit_evidence_export
    ALTER COLUMN created_by_type SET DEFAULT 'admin';
