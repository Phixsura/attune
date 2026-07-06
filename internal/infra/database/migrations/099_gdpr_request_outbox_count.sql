-- Persist notify_outbox purge counts on GDPR request history so the Console
-- can show the full deletion footprint for scheduled erasures.

ALTER TABLE gdpr_requests
    ADD COLUMN IF NOT EXISTS outbox_count INTEGER;

WITH delete_request_audit AS (
    SELECT DISTINCT ON (a.tenant_id, a.after_json->>'request_id')
        a.tenant_id,
        a.after_json->>'request_id' AS request_id,
        COALESCE((a.after_json->'counts'->>'notify_outbox')::int, 0) AS outbox_count
    FROM audit_log a
    WHERE a.action = 'gdpr.delete.requested'
      AND a.after_json ? 'request_id'
      AND COALESCE(a.after_json->>'request_id', '') <> ''
    ORDER BY a.tenant_id, a.after_json->>'request_id', a.created_at DESC, a.id DESC
)
UPDATE gdpr_requests r
SET outbox_count = d.outbox_count
FROM delete_request_audit d
WHERE r.request_type = 'delete'
  AND r.id = d.request_id
  AND r.tenant_id = d.tenant_id
  AND (r.outbox_count IS NULL OR r.outbox_count = 0);

UPDATE gdpr_requests
SET outbox_count = 0
WHERE outbox_count IS NULL;

ALTER TABLE gdpr_requests
    ALTER COLUMN outbox_count SET DEFAULT 0;

ALTER TABLE gdpr_requests
    ALTER COLUMN outbox_count SET NOT NULL;

ALTER TABLE gdpr_requests
    DROP CONSTRAINT IF EXISTS chk_gdpr_requests_outbox_count;

ALTER TABLE gdpr_requests
    ADD CONSTRAINT chk_gdpr_requests_outbox_count CHECK (outbox_count >= 0);
