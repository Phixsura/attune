-- Migration 091: Backfill granular GDPR API-key scopes
-- Existing scoped keys seeded before granular GDPR permissions existed only hold
-- gdpr:admin. Keep them working by granting the finer scopes that gdpr:admin now
-- implies.
BEGIN;

INSERT INTO api_key_scopes (key_id, scope)
SELECT s.key_id, v.scope
FROM api_key_scopes s
CROSS JOIN (
    VALUES
    ('gdpr:read'),
    ('gdpr:export'),
    ('gdpr:delete')
) AS v(scope)
WHERE s.scope = 'gdpr:admin'
ON CONFLICT (key_id, scope) DO NOTHING;

COMMIT;
