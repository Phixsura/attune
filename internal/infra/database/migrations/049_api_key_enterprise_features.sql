-- 049_api_key_enterprise_features.sql
-- Enterprise API key features: org policies, project binding, budgets, tags, temp tokens, approvals

-- Org-level API key policies
CREATE TABLE IF NOT EXISTS api_key_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL UNIQUE,
    max_expiry_days INT,                          -- max allowed expiration (NULL = unlimited)
    require_expiry BOOLEAN NOT NULL DEFAULT FALSE, -- force all keys to have expiration
    require_ip_allowlist BOOLEAN NOT NULL DEFAULT FALSE,
    require_description BOOLEAN NOT NULL DEFAULT FALSE,
    max_keys_per_service_account INT,             -- limit keys per service account
    allowed_environments TEXT[],                   -- restrict to specific environments
    require_mfa_for_create BOOLEAN NOT NULL DEFAULT FALSE,
    require_approval_for_prod BOOLEAN NOT NULL DEFAULT FALSE,
    auto_revoke_unused_days INT,                  -- auto-revoke after N days unused
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Projects/workspaces for key isolation
CREATE TABLE IF NOT EXISTS projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_projects_tenant ON projects(tenant_id);

-- Add project binding to api_keys
ALTER TABLE external_api_keys
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS budget_limit_usd DECIMAL(10,2),
    ADD COLUMN IF NOT EXISTS budget_spent_usd DECIMAL(10,2) DEFAULT 0,
    ADD COLUMN IF NOT EXISTS budget_reset_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS is_temporary BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS approval_status TEXT DEFAULT 'approved',
    ADD COLUMN IF NOT EXISTS approved_by TEXT,
    ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS mfa_verified_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_api_keys_project ON external_api_keys(project_id) WHERE project_id IS NOT NULL;

-- API key custom metadata/tags
CREATE TABLE IF NOT EXISTS api_key_tags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_id UUID NOT NULL REFERENCES external_api_keys(id) ON DELETE CASCADE,
    tag_key TEXT NOT NULL,
    tag_value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (key_id, tag_key)
);

CREATE INDEX IF NOT EXISTS idx_api_key_tags_key ON api_key_tags(key_id);
CREATE INDEX IF NOT EXISTS idx_api_key_tags_lookup ON api_key_tags(tag_key, tag_value);

-- Approval workflow for API keys
DO $$ BEGIN
    CREATE TYPE api_key_approval_status AS ENUM ('pending', 'approved', 'rejected', 'expired');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS api_key_approval_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    requester_id TEXT NOT NULL,
    requester_type TEXT NOT NULL,               -- 'admin', 'member', 'service_account'
    key_label TEXT NOT NULL,
    key_description TEXT,
    requested_scopes TEXT[],
    requested_environment TEXT,
    requested_expiry_days INT,
    justification TEXT,
    status api_key_approval_status NOT NULL DEFAULT 'pending',
    reviewer_id TEXT,
    reviewer_notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reviewed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '7 days')
);

CREATE INDEX IF NOT EXISTS idx_api_key_approval_requests_tenant ON api_key_approval_requests(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_api_key_approval_requests_pending ON api_key_approval_requests(tenant_id) WHERE status = 'pending';

-- Temporary token tracking (short-lived, auto-expire)
CREATE TABLE IF NOT EXISTS api_key_temporary_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_key_id UUID NOT NULL REFERENCES external_api_keys(id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL,
    token_hash BYTEA NOT NULL,
    token_prefix TEXT NOT NULL,
    purpose TEXT,                               -- 'ci_job', 'one_time', 'session'
    expires_at TIMESTAMPTZ NOT NULL,
    max_uses INT,                               -- NULL = unlimited
    use_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_temp_tokens_hash ON api_key_temporary_tokens(token_hash) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_temp_tokens_parent ON api_key_temporary_tokens(parent_key_id);
CREATE INDEX IF NOT EXISTS idx_temp_tokens_cleanup ON api_key_temporary_tokens(expires_at) WHERE revoked_at IS NULL;

-- OAuth2 client credentials
CREATE TABLE IF NOT EXISTS oauth2_clients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    client_id TEXT NOT NULL UNIQUE,
    client_secret_hash BYTEA NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    redirect_uris TEXT[],
    allowed_scopes TEXT[],
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_oauth2_clients_tenant ON oauth2_clients(tenant_id);
CREATE INDEX IF NOT EXISTS idx_oauth2_clients_client_id ON oauth2_clients(client_id);

-- OAuth2 access tokens
CREATE TABLE IF NOT EXISTS oauth2_access_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES oauth2_clients(id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL,
    token_hash BYTEA NOT NULL,
    scopes TEXT[],
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_oauth2_tokens_hash ON oauth2_access_tokens(token_hash) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_oauth2_tokens_client ON oauth2_access_tokens(client_id);

-- Secret manager integration config
DO $$ BEGIN
    CREATE TYPE secret_manager_type AS ENUM ('hashicorp_vault', 'aws_secrets_manager', 'gcp_secret_manager', 'azure_key_vault');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS secret_manager_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    manager_type secret_manager_type NOT NULL,
    name TEXT NOT NULL,
    config JSONB NOT NULL,                      -- encrypted connection details
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_sync_at TIMESTAMPTZ,
    last_sync_status TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_secret_manager_configs_tenant ON secret_manager_configs(tenant_id);

-- API key analytics aggregates (hourly rollups for dashboard)
CREATE TABLE IF NOT EXISTS api_key_analytics_hourly (
    id BIGSERIAL PRIMARY KEY,
    key_id UUID NOT NULL,
    tenant_id TEXT NOT NULL,
    hour TIMESTAMPTZ NOT NULL,
    request_count BIGINT NOT NULL DEFAULT 0,
    error_count BIGINT NOT NULL DEFAULT 0,
    total_latency_ms BIGINT NOT NULL DEFAULT 0,
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    unique_ips INT NOT NULL DEFAULT 0,
    status_2xx INT NOT NULL DEFAULT 0,
    status_4xx INT NOT NULL DEFAULT 0,
    status_5xx INT NOT NULL DEFAULT 0,
    UNIQUE (key_id, hour)
);

CREATE INDEX IF NOT EXISTS idx_api_key_analytics_hourly_tenant ON api_key_analytics_hourly(tenant_id, hour DESC);
CREATE INDEX IF NOT EXISTS idx_api_key_analytics_hourly_key ON api_key_analytics_hourly(key_id, hour DESC);

-- Budget tracking history
CREATE TABLE IF NOT EXISTS api_key_budget_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_id UUID NOT NULL REFERENCES external_api_keys(id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    budget_limit_usd DECIMAL(10,2) NOT NULL,
    spent_usd DECIMAL(10,2) NOT NULL,
    overage_action TEXT,                        -- 'block', 'alert', 'none'
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_api_key_budget_history_key ON api_key_budget_history(key_id, period_start DESC);

-- Function to aggregate request logs to hourly analytics
CREATE OR REPLACE FUNCTION aggregate_api_key_analytics()
RETURNS void AS $$
BEGIN
    INSERT INTO api_key_analytics_hourly (key_id, tenant_id, hour, request_count, error_count, total_latency_ms, status_2xx, status_4xx, status_5xx)
    SELECT
        key_id,
        tenant_id,
        DATE_TRUNC('hour', timestamp) as hour,
        COUNT(*) as request_count,
        COUNT(*) FILTER (WHERE status_code >= 400) as error_count,
        SUM(COALESCE(latency_ms, 0)) as total_latency_ms,
        COUNT(*) FILTER (WHERE status_code >= 200 AND status_code < 300) as status_2xx,
        COUNT(*) FILTER (WHERE status_code >= 400 AND status_code < 500) as status_4xx,
        COUNT(*) FILTER (WHERE status_code >= 500) as status_5xx
    FROM api_key_request_logs
    WHERE timestamp >= DATE_TRUNC('hour', NOW() - INTERVAL '1 hour')
      AND timestamp < DATE_TRUNC('hour', NOW())
    GROUP BY key_id, tenant_id, DATE_TRUNC('hour', timestamp)
    ON CONFLICT (key_id, hour) DO UPDATE SET
        request_count = EXCLUDED.request_count,
        error_count = EXCLUDED.error_count,
        total_latency_ms = EXCLUDED.total_latency_ms,
        status_2xx = EXCLUDED.status_2xx,
        status_4xx = EXCLUDED.status_4xx,
        status_5xx = EXCLUDED.status_5xx;
END;
$$ LANGUAGE plpgsql;

-- Comments for documentation
COMMENT ON TABLE api_key_policies IS 'Org-level policies enforced on all API keys';
COMMENT ON TABLE projects IS 'Projects/workspaces for API key isolation';
COMMENT ON TABLE api_key_tags IS 'Custom key-value metadata for API keys';
COMMENT ON TABLE api_key_approval_requests IS 'Approval workflow for production API keys';
COMMENT ON TABLE api_key_temporary_tokens IS 'Short-lived tokens derived from parent keys';
COMMENT ON TABLE oauth2_clients IS 'OAuth2 client credentials for M2M auth';
COMMENT ON TABLE oauth2_access_tokens IS 'OAuth2 access tokens issued to clients';
COMMENT ON TABLE secret_manager_configs IS 'External secret manager integration configs';
COMMENT ON TABLE api_key_analytics_hourly IS 'Hourly aggregated analytics for API keys';
