-- Migration 046: API key scopes
-- Adds fine-grained scope control for API keys (#41)
BEGIN;

-- Normalized scope table: one row per (key, scope) pair
CREATE TABLE IF NOT EXISTS api_key_scopes (
    key_id      UUID        NOT NULL REFERENCES external_api_keys(id) ON DELETE CASCADE,
    scope       TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (key_id, scope)
);

-- Index for fast scope lookup by key
CREATE INDEX IF NOT EXISTS idx_api_key_scopes_key
  ON api_key_scopes (key_id);

-- Seed existing active keys with all scopes EXCEPT apikey:admin
-- Security: prevents migrated keys from managing other keys
INSERT INTO api_key_scopes (key_id, scope)
SELECT k.id, s.scope
FROM external_api_keys k
CROSS JOIN (
    VALUES
    ('ingest:write'),
    ('feedback:read'), ('feedback:write'),
    ('usage:read'), ('audit:read'),
    ('llm:read'), ('llm:write'),
    ('enrich:read'), ('enrich:write'),
    ('guard:read'), ('guard:write'),
    ('notify:read'), ('notify:write'),
    ('inbound:read'), ('inbound:write'),
    ('digest:read'), ('digest:write'),
    ('tags:read'), ('tags:write'),
    ('workflow:read'), ('workflow:write'),
    ('gdpr:admin'), ('members:admin')
) AS s(scope)
WHERE k.revoked_at IS NULL;

COMMIT;
