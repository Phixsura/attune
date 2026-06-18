-- 048_api_key_advanced_features.sql
-- Advanced API key features: rotation, request logs, environment tags, service accounts, resource binding

-- Environment enum for API keys
DO $$ BEGIN
    CREATE TYPE api_key_environment AS ENUM ('production', 'staging', 'development', 'test');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- Add environment and rotation columns to api_keys
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS environment api_key_environment NOT NULL DEFAULT 'production',
    ADD COLUMN IF NOT EXISTS rotated_from_id UUID REFERENCES api_keys(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS grace_period_ends_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS service_account_id UUID;

-- Service accounts table
CREATE TABLE IF NOT EXISTS service_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_service_accounts_tenant ON service_accounts(tenant_id);

-- Add foreign key for service_account_id
DO $$ BEGIN
    ALTER TABLE api_keys
        ADD CONSTRAINT fk_api_keys_service_account
        FOREIGN KEY (service_account_id) REFERENCES service_accounts(id) ON DELETE SET NULL;
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- API key request logs (partitioned by month for performance)
CREATE TABLE IF NOT EXISTS api_key_request_logs (
    id BIGSERIAL,
    key_id UUID NOT NULL,
    tenant_id TEXT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    method TEXT NOT NULL,
    path TEXT NOT NULL,
    status_code INT NOT NULL,
    client_ip INET,
    user_agent TEXT,
    latency_ms INT,
    request_id TEXT,
    PRIMARY KEY (id, timestamp)
) PARTITION BY RANGE (timestamp);

-- Create partitions for current and next 3 months
DO $$
DECLARE
    start_date DATE;
    end_date DATE;
    partition_name TEXT;
BEGIN
    FOR i IN 0..3 LOOP
        start_date := DATE_TRUNC('month', CURRENT_DATE + (i || ' month')::INTERVAL);
        end_date := start_date + '1 month'::INTERVAL;
        partition_name := 'api_key_request_logs_' || TO_CHAR(start_date, 'YYYY_MM');

        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF api_key_request_logs
             FOR VALUES FROM (%L) TO (%L)',
            partition_name, start_date, end_date
        );
    END LOOP;
END $$;

CREATE INDEX IF NOT EXISTS idx_api_key_request_logs_key_id ON api_key_request_logs(key_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_api_key_request_logs_tenant ON api_key_request_logs(tenant_id, timestamp DESC);

-- Resource bindings for API keys
CREATE TABLE IF NOT EXISTS api_key_resource_bindings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_id UUID NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (key_id, resource_type, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_api_key_resource_bindings_key ON api_key_resource_bindings(key_id);

-- API key event types enum
DO $$ BEGIN
    CREATE TYPE api_key_event_type AS ENUM (
        'created', 'rotated', 'revoked', 'expired',
        'expiring_soon', 'used', 'scope_denied', 'ip_denied'
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- API key event subscriptions (webhook notifications)
CREATE TABLE IF NOT EXISTS api_key_event_subscriptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    event_types api_key_event_type[] NOT NULL,
    webhook_url TEXT NOT NULL,
    secret TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_api_key_event_subscriptions_tenant ON api_key_event_subscriptions(tenant_id);

-- API key events log (for webhook delivery and audit)
CREATE TABLE IF NOT EXISTS api_key_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    key_id UUID REFERENCES api_keys(id) ON DELETE SET NULL,
    event_type api_key_event_type NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_api_key_events_tenant ON api_key_events(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_key_events_key ON api_key_events(key_id, created_at DESC);

-- Leaked key detection table
CREATE TABLE IF NOT EXISTS api_key_leak_detections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_id UUID NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    tenant_id TEXT NOT NULL,
    source TEXT NOT NULL,
    source_url TEXT,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    resolution TEXT,
    UNIQUE (key_id, source, source_url)
);

CREATE INDEX IF NOT EXISTS idx_api_key_leak_detections_key ON api_key_leak_detections(key_id);
CREATE INDEX IF NOT EXISTS idx_api_key_leak_detections_unresolved ON api_key_leak_detections(tenant_id) WHERE resolved_at IS NULL;

-- Function to create next month's partition automatically
CREATE OR REPLACE FUNCTION create_api_key_request_logs_partition()
RETURNS void AS $$
DECLARE
    next_month DATE;
    partition_name TEXT;
BEGIN
    next_month := DATE_TRUNC('month', CURRENT_DATE + '1 month'::INTERVAL);
    partition_name := 'api_key_request_logs_' || TO_CHAR(next_month, 'YYYY_MM');

    EXECUTE format(
        'CREATE TABLE IF NOT EXISTS %I PARTITION OF api_key_request_logs
         FOR VALUES FROM (%L) TO (%L)',
        partition_name, next_month, next_month + '1 month'::INTERVAL
    );
END;
$$ LANGUAGE plpgsql;

-- Comment for documentation
COMMENT ON TABLE service_accounts IS 'Non-human identities for CI/CD and automation';
COMMENT ON TABLE api_key_request_logs IS 'Per-request audit log for API keys (partitioned monthly)';
COMMENT ON TABLE api_key_resource_bindings IS 'Restrict API key access to specific resources';
COMMENT ON TABLE api_key_event_subscriptions IS 'Webhook subscriptions for API key lifecycle events';
COMMENT ON TABLE api_key_events IS 'Audit log of API key lifecycle events';
COMMENT ON TABLE api_key_leak_detections IS 'Detected leaks of API keys in public sources';
