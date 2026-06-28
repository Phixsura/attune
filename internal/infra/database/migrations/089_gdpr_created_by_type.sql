-- Persist GDPR async actor types so worker-emitted audit rows preserve the
-- original caller identity class instead of collapsing everything to "admin".

ALTER TABLE gdpr_requests
    ADD COLUMN IF NOT EXISTS created_by_type TEXT;

ALTER TABLE gdpr_export_jobs
    ADD COLUMN IF NOT EXISTS created_by_type TEXT;

WITH delete_request_audit AS (
    SELECT DISTINCT ON (a.tenant_id, a.after_json->>'request_id')
        a.tenant_id,
        a.after_json->>'request_id' AS request_id,
        a.actor_type
    FROM audit_log a
    WHERE a.action = 'gdpr.delete.requested'
      AND a.after_json ? 'request_id'
      AND COALESCE(a.after_json->>'request_id', '') <> ''
    ORDER BY a.tenant_id, a.after_json->>'request_id', a.created_at DESC, a.id DESC
)
UPDATE gdpr_requests r
SET created_by_type = COALESCE(
    d.actor_type,
    CASE
        WHEN r.created_by LIKE 'apikey:%' THEN 'api_key'
        WHEN EXISTS (SELECT 1 FROM oidc_users ou WHERE ou.id = r.created_by) THEN 'oidc'
        ELSE 'admin'
    END
)
FROM delete_request_audit d
WHERE r.id = d.request_id
  AND r.tenant_id = d.tenant_id
  AND (r.created_by_type IS NULL OR btrim(r.created_by_type) = '');

UPDATE gdpr_requests r
SET created_by_type = CASE
    WHEN r.created_by LIKE 'apikey:%' THEN 'api_key'
    WHEN EXISTS (SELECT 1 FROM oidc_users ou WHERE ou.id = r.created_by) THEN 'oidc'
    ELSE 'admin'
END
WHERE r.created_by_type IS NULL OR btrim(r.created_by_type) = '';

UPDATE gdpr_export_jobs j
SET created_by_type = r.created_by_type
FROM gdpr_requests r
WHERE j.request_id = r.id
  AND (j.created_by_type IS NULL OR btrim(j.created_by_type) = '');

UPDATE gdpr_export_jobs j
SET created_by_type = CASE
    WHEN j.created_by LIKE 'apikey:%' THEN 'api_key'
    WHEN EXISTS (SELECT 1 FROM oidc_users ou WHERE ou.id = j.created_by) THEN 'oidc'
    ELSE 'admin'
END
WHERE j.created_by_type IS NULL OR btrim(j.created_by_type) = '';

ALTER TABLE gdpr_requests
    ALTER COLUMN created_by_type SET NOT NULL;

ALTER TABLE gdpr_export_jobs
    ALTER COLUMN created_by_type SET NOT NULL;

ALTER TABLE gdpr_requests
    ALTER COLUMN created_by_type SET DEFAULT 'admin';

ALTER TABLE gdpr_export_jobs
    ALTER COLUMN created_by_type SET DEFAULT 'admin';
