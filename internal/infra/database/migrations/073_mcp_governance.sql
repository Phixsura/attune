-- Migration 073: MCP governance runtime fields
--
-- Adds:
-- - per-client policy mode + default rate limits
-- - per-client per-tool policy overrides
-- - session last-activity governance context

BEGIN;

ALTER TABLE mcp_oauth_clients
    ADD COLUMN tool_policy_mode TEXT NOT NULL DEFAULT 'legacy_allow_all',
    ADD COLUMN rate_limit_rpm INTEGER,
    ADD COLUMN rate_limit_burst INTEGER;

ALTER TABLE mcp_oauth_clients
    ADD CONSTRAINT chk_mcp_client_tool_policy_mode
        CHECK (tool_policy_mode IN ('legacy_allow_all', 'allow_list')),
    ADD CONSTRAINT chk_mcp_client_rate_limit_rpm
        CHECK (rate_limit_rpm IS NULL OR rate_limit_rpm > 0),
    ADD CONSTRAINT chk_mcp_client_rate_limit_burst
        CHECK (rate_limit_burst IS NULL OR rate_limit_burst > 0);

CREATE TABLE mcp_client_tool_policies (
    client_id         UUID        NOT NULL REFERENCES mcp_oauth_clients(id) ON DELETE CASCADE,
    tool_name         TEXT        NOT NULL,
    effect            TEXT        NOT NULL,
    rate_limit_rpm    INTEGER,
    rate_limit_burst  INTEGER,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (client_id, tool_name),
    CONSTRAINT chk_mcp_tool_policy_name CHECK (length(tool_name) BETWEEN 1 AND 128),
    CONSTRAINT chk_mcp_tool_policy_effect CHECK (effect IN ('allow', 'deny')),
    CONSTRAINT chk_mcp_tool_policy_rpm CHECK (rate_limit_rpm IS NULL OR rate_limit_rpm > 0),
    CONSTRAINT chk_mcp_tool_policy_burst CHECK (rate_limit_burst IS NULL OR rate_limit_burst > 0)
);

ALTER TABLE mcp_sessions
    ADD COLUMN last_tool_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN last_decision TEXT NOT NULL DEFAULT '',
    ADD COLUMN last_ip TEXT NOT NULL DEFAULT '',
    ADD COLUMN last_user_agent TEXT NOT NULL DEFAULT '',
    ADD COLUMN closed_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN closed_by TEXT NOT NULL DEFAULT '';

COMMIT;
