-- 018_guard_policies.sql — source-aware LLM guard policy foundation (#20).
--
-- Guard policy is cross-channel governance, not channel adapter config:
-- policies are queryable, explainable, Console-manageable, and applied at the
-- LLM boundary. inbound_sources.config remains encrypted adapter-specific
-- secret material.

CREATE TABLE IF NOT EXISTS guard_policies (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  TEXT REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('baseline', 'default', 'override')),
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    priority   INT NOT NULL DEFAULT 100,
    target     JSONB NOT NULL DEFAULT '{}'::jsonb,
    rules      JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by TEXT,
    updated_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS guard_policies_tenant_enabled
    ON guard_policies (tenant_id, enabled, priority);

ALTER TABLE inbound_sources
    ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}';

ALTER TABLE user_feedback
    ADD COLUMN IF NOT EXISTS inbound_source_id UUID REFERENCES inbound_sources(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_user_feedback_inbound_source
    ON user_feedback (inbound_source_id);

-- Privacy-by-default seed. This is intentionally a default, not a baseline:
-- tenant/source policies may relax it (for local/trusted LLM paths) or tighten
-- it (for regulated support mailboxes).
INSERT INTO guard_policies (tenant_id, name, kind, priority, target, rules, created_by, updated_by)
SELECT
    NULL,
    'System privacy default',
    'default',
    100,
    '{"purposes":["enrich"],"stages":["llm_input"]}'::jsonb,
    '[
      {
        "guard": "pii",
        "stage": "llm_input",
        "entities": ["email", "phone", "cn_mobile", "cn_id", "credit_card"],
        "action": "redact",
        "replacement": "<PII:{entity}>"
      }
    ]'::jsonb,
    'migration:018',
    'migration:018'
WHERE NOT EXISTS (
    SELECT 1 FROM guard_policies
    WHERE tenant_id IS NULL AND name = 'System privacy default'
);
