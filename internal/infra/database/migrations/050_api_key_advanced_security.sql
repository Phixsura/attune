-- 050_api_key_advanced_security.sql
-- Advanced security features: browser detection, concurrent keys, auto-rotation,
-- unused permission detection, auto-revoke on leak, PKCV, managed identity, SSO, SIEM

-- Browser detection settings per tenant
ALTER TABLE api_key_policies
    ADD COLUMN IF NOT EXISTS block_browser_access BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS browser_user_agent_patterns TEXT[] DEFAULT ARRAY[
        'Mozilla/%', 'Chrome/%', 'Safari/%', 'Firefox/%', 'Edge/%', 'Opera/%'
    ];

COMMENT ON COLUMN api_key_policies.block_browser_access IS 'Return 401 if secret key used from browser User-Agent (Supabase pattern)';

-- Concurrent keys: allow multiple active keys per service account / purpose
ALTER TABLE external_api_keys
    ADD COLUMN IF NOT EXISTS key_slot INT DEFAULT 1,
    ADD COLUMN IF NOT EXISTS purpose TEXT,
    ADD COLUMN IF NOT EXISTS is_primary BOOLEAN DEFAULT TRUE;

CREATE INDEX IF NOT EXISTS idx_api_keys_concurrent
    ON external_api_keys (tenant_id, service_account_id, key_slot)
    WHERE is_active = TRUE;

COMMENT ON COLUMN external_api_keys.key_slot IS 'Slot number for concurrent keys (AWS allows 2 per IAM user)';
COMMENT ON COLUMN external_api_keys.is_primary IS 'Primary key for the slot (used for new requests)';

-- Scheduled auto-rotation
CREATE TABLE IF NOT EXISTS api_key_rotation_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    key_id UUID NOT NULL REFERENCES external_api_keys(id) ON DELETE CASCADE,
    rotation_interval_days INT NOT NULL DEFAULT 90,
    next_rotation_at TIMESTAMPTZ NOT NULL,
    last_rotated_at TIMESTAMPTZ,
    grace_period_hours INT NOT NULL DEFAULT 168,  -- 7 days default
    auto_notify BOOLEAN NOT NULL DEFAULT TRUE,
    notify_days_before INT[] DEFAULT ARRAY[30, 7, 1],
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (key_id)
);

CREATE INDEX IF NOT EXISTS idx_rotation_schedules_next
    ON api_key_rotation_schedules (next_rotation_at)
    WHERE is_active = TRUE;

COMMENT ON TABLE api_key_rotation_schedules IS 'Scheduled automatic key rotation (AWS Config 90-day pattern)';

-- Unused permission detection
CREATE TABLE IF NOT EXISTS api_key_permission_usage (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_id UUID NOT NULL REFERENCES external_api_keys(id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL,
    scope TEXT NOT NULL,
    first_used_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    usage_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (key_id, scope)
);

CREATE INDEX IF NOT EXISTS idx_permission_usage_key ON api_key_permission_usage(key_id);
CREATE INDEX IF NOT EXISTS idx_permission_usage_unused
    ON api_key_permission_usage (key_id)
    WHERE first_used_at IS NULL;

COMMENT ON TABLE api_key_permission_usage IS 'Track actual scope usage for unused permission detection (AWS Access Analyzer pattern)';

-- Auto-revoke configuration
ALTER TABLE api_key_policies
    ADD COLUMN IF NOT EXISTS auto_revoke_on_leak BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS leak_detection_sources TEXT[] DEFAULT ARRAY[
        'github', 'gitlab', 'bitbucket', 'postman', 'trufflehog'
    ];

ALTER TABLE api_key_leak_detections
    ADD COLUMN IF NOT EXISTS auto_revoked BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS auto_revoked_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS confidence_score DECIMAL(3,2);  -- 0.00 to 1.00

COMMENT ON COLUMN api_key_policies.auto_revoke_on_leak IS 'Automatically revoke keys when leak is detected (Postman pattern)';

-- Public Key Cryptography Verification (PKCV) - Twilio pattern
DO $$ BEGIN
    CREATE TYPE key_type AS ENUM ('secret', 'public_key');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

ALTER TABLE external_api_keys
    ADD COLUMN IF NOT EXISTS key_type TEXT DEFAULT 'secret',
    ADD COLUMN IF NOT EXISTS public_key BYTEA,
    ADD COLUMN IF NOT EXISTS key_algorithm TEXT;  -- RS256, ES256, etc.

CREATE TABLE IF NOT EXISTS api_key_signing_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_id UUID NOT NULL REFERENCES external_api_keys(id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL,
    algorithm TEXT NOT NULL,  -- RS256, RS384, RS512, ES256, ES384, ES512
    public_key_pem TEXT NOT NULL,
    key_fingerprint TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    UNIQUE (key_id, key_fingerprint)
);

CREATE INDEX IF NOT EXISTS idx_signing_keys_fingerprint
    ON api_key_signing_keys (key_fingerprint)
    WHERE is_active = TRUE;

COMMENT ON TABLE api_key_signing_keys IS 'Public keys for request signing verification (Twilio PKCV pattern)';

-- Managed Identity / Workload Identity (Azure/GCP pattern)
DO $$ BEGIN
    CREATE TYPE identity_provider AS ENUM (
        'aws_iam', 'azure_ad', 'gcp_workload', 'kubernetes', 'github_actions', 'custom_oidc'
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS managed_identities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,  -- identity_provider enum
    external_id TEXT NOT NULL,  -- ARN, Object ID, etc.
    audience TEXT,  -- expected audience claim
    issuer TEXT,  -- expected issuer
    subject_pattern TEXT,  -- regex for subject claim
    allowed_scopes TEXT[],
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, provider, external_id)
);

CREATE INDEX IF NOT EXISTS idx_managed_identities_lookup
    ON managed_identities (provider, external_id)
    WHERE is_active = TRUE;

COMMENT ON TABLE managed_identities IS 'Managed/workload identities for secretless authentication (Azure/GCP pattern)';

-- SSO enforcement
ALTER TABLE api_key_policies
    ADD COLUMN IF NOT EXISTS require_sso BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS sso_provider TEXT,  -- okta, auth0, azure_ad, etc.
    ADD COLUMN IF NOT EXISTS sso_session_max_age_hours INT DEFAULT 24;

ALTER TABLE external_api_keys
    ADD COLUMN IF NOT EXISTS sso_linked BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS sso_user_id TEXT,
    ADD COLUMN IF NOT EXISTS sso_session_id TEXT,
    ADD COLUMN IF NOT EXISTS sso_session_expires_at TIMESTAMPTZ;

COMMENT ON COLUMN api_key_policies.require_sso IS 'Require SSO authentication to use API keys (GitHub/Okta pattern)';

-- SIEM integration
DO $$ BEGIN
    CREATE TYPE siem_provider AS ENUM (
        'splunk', 'datadog', 'elastic', 'sumo_logic', 'chronicle', 'sentinel', 'custom_webhook'
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS siem_integrations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    provider TEXT NOT NULL,  -- siem_provider enum
    name TEXT NOT NULL,
    endpoint_url TEXT NOT NULL,
    auth_config JSONB NOT NULL,  -- encrypted credentials
    event_types TEXT[] NOT NULL DEFAULT ARRAY[
        'key.created', 'key.rotated', 'key.revoked', 'key.expired',
        'key.leaked', 'auth.success', 'auth.failure', 'scope.denied'
    ],
    batch_size INT NOT NULL DEFAULT 100,
    flush_interval_seconds INT NOT NULL DEFAULT 60,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_sent_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_siem_integrations_active
    ON siem_integrations (tenant_id)
    WHERE is_active = TRUE;

-- SIEM event buffer for batching
CREATE TABLE IF NOT EXISTS siem_event_buffer (
    id BIGSERIAL PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    integration_id UUID NOT NULL REFERENCES siem_integrations(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    event_data JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_siem_buffer_pending
    ON siem_event_buffer (integration_id, created_at)
    WHERE sent_at IS NULL;

COMMENT ON TABLE siem_integrations IS 'SIEM integrations for real-time security event streaming';

-- AI Agent / MCP support
CREATE TABLE IF NOT EXISTS ai_agent_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    agent_type TEXT NOT NULL,  -- 'mcp_server', 'langchain', 'openai_function', 'anthropic_tool'
    allowed_scopes TEXT[],
    max_tokens_per_request INT DEFAULT 100000,
    max_requests_per_minute INT DEFAULT 60,
    allowed_models TEXT[],  -- restrict to specific LLM models
    require_human_approval BOOLEAN NOT NULL DEFAULT FALSE,
    approval_timeout_seconds INT DEFAULT 300,
    audit_all_requests BOOLEAN NOT NULL DEFAULT TRUE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

-- AI agent request audit
CREATE TABLE IF NOT EXISTS ai_agent_requests (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    agent_config_id UUID NOT NULL REFERENCES ai_agent_configs(id) ON DELETE CASCADE,
    key_id UUID REFERENCES external_api_keys(id) ON DELETE SET NULL,
    request_type TEXT NOT NULL,  -- 'tool_call', 'function_call', 'mcp_request'
    tool_name TEXT,
    input_tokens INT,
    output_tokens INT,
    model TEXT,
    status TEXT NOT NULL,  -- 'pending_approval', 'approved', 'rejected', 'completed', 'failed'
    approved_by TEXT,
    approved_at TIMESTAMPTZ,
    error_message TEXT,
    duration_ms INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE ai_agent_requests_2026_06 PARTITION OF ai_agent_requests
    FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');
CREATE TABLE ai_agent_requests_2026_07 PARTITION OF ai_agent_requests
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');

CREATE INDEX IF NOT EXISTS idx_ai_agent_requests_agent
    ON ai_agent_requests (agent_config_id, created_at DESC);

COMMENT ON TABLE ai_agent_configs IS 'AI agent configurations for LLM-based API access (Okta MCP pattern)';
COMMENT ON TABLE ai_agent_requests IS 'Audit trail for AI agent API requests';

-- Key health score (composite metric)
ALTER TABLE external_api_keys
    ADD COLUMN IF NOT EXISTS health_score INT,  -- 0-100
    ADD COLUMN IF NOT EXISTS health_factors JSONB;  -- breakdown of score components

COMMENT ON COLUMN external_api_keys.health_score IS 'Composite security health score (0-100)';

-- Function to calculate key health score
CREATE OR REPLACE FUNCTION calculate_key_health_score(p_key_id UUID)
RETURNS INT AS $$
DECLARE
    v_score INT := 100;
    v_key RECORD;
    v_unused_scopes INT;
    v_days_since_rotation INT;
BEGIN
    SELECT * INTO v_key FROM external_api_keys WHERE id = p_key_id;
    IF NOT FOUND THEN RETURN 0; END IF;

    -- Deduct for no expiration
    IF v_key.expires_at IS NULL THEN
        v_score := v_score - 15;
    END IF;

    -- Deduct for no IP restriction
    IF v_key.allowed_cidrs IS NULL OR array_length(v_key.allowed_cidrs, 1) IS NULL THEN
        v_score := v_score - 10;
    END IF;

    -- Deduct for no rate limit
    IF v_key.rate_limit_rpm IS NULL THEN
        v_score := v_score - 5;
    END IF;

    -- Deduct for unused scopes
    SELECT COUNT(*) INTO v_unused_scopes
    FROM api_key_permission_usage
    WHERE key_id = p_key_id AND first_used_at IS NULL;
    v_score := v_score - LEAST(v_unused_scopes * 2, 20);

    -- Deduct for old key (no rotation in 90+ days)
    v_days_since_rotation := EXTRACT(DAY FROM NOW() - COALESCE(v_key.rotated_at, v_key.created_at));
    IF v_days_since_rotation > 90 THEN
        v_score := v_score - LEAST((v_days_since_rotation - 90) / 30 * 5, 20);
    END IF;

    RETURN GREATEST(v_score, 0);
END;
$$ LANGUAGE plpgsql;

-- Comments for documentation
COMMENT ON FUNCTION calculate_key_health_score IS 'Calculate composite security health score for an API key';
