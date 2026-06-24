-- Migration 058: MCP OAuth clients, tokens, and sessions
--
-- Tables for MCP OAuth 2.1 Authorization Server:
-- - mcp_oauth_clients: registered agents
-- - mcp_oauth_codes: short-lived authorization codes
-- - mcp_oauth_refresh_tokens: long-lived refresh tokens
-- - mcp_sessions: MCP session tracking (Mcp-Session-Id)

BEGIN;

CREATE TABLE mcp_oauth_clients (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name            TEXT        NOT NULL,
    redirect_uris   TEXT[]      NOT NULL,
    scopes          TEXT[]      NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      TEXT        NOT NULL,
    revoked_at      TIMESTAMPTZ,

    CONSTRAINT uq_mcp_client_name_tenant UNIQUE (tenant_id, name),
    CONSTRAINT chk_mcp_client_name_length CHECK (length(name) BETWEEN 1 AND 128),
    CONSTRAINT chk_mcp_client_redirect_uris CHECK (array_length(redirect_uris, 1) >= 1)
);

CREATE INDEX idx_mcp_oauth_clients_tenant ON mcp_oauth_clients(tenant_id) WHERE revoked_at IS NULL;

CREATE TABLE mcp_oauth_codes (
    code            TEXT        PRIMARY KEY,
    client_id       UUID        NOT NULL REFERENCES mcp_oauth_clients(id) ON DELETE CASCADE,
    redirect_uri    TEXT        NOT NULL,
    scopes          TEXT[]      NOT NULL,
    code_challenge  TEXT        NOT NULL,
    user_id         TEXT        NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_mcp_code_length CHECK (length(code) = 64)
);

CREATE INDEX idx_mcp_oauth_codes_expires ON mcp_oauth_codes(expires_at);

CREATE TABLE mcp_oauth_refresh_tokens (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID        NOT NULL REFERENCES mcp_oauth_clients(id) ON DELETE CASCADE,
    token_hash      TEXT        NOT NULL UNIQUE,
    scopes          TEXT[]      NOT NULL,
    user_id         TEXT        NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at      TIMESTAMPTZ,

    CONSTRAINT chk_mcp_refresh_token_hash_length CHECK (length(token_hash) = 64)
);

CREATE INDEX idx_mcp_refresh_tokens_client ON mcp_oauth_refresh_tokens(client_id) WHERE revoked_at IS NULL;

CREATE TABLE mcp_sessions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id       UUID        NOT NULL REFERENCES mcp_oauth_clients(id) ON DELETE CASCADE,
    tenant_id       TEXT        NOT NULL,
    scopes          TEXT[]      NOT NULL,
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at       TIMESTAMPTZ
);

CREATE INDEX idx_mcp_sessions_client ON mcp_sessions(client_id) WHERE closed_at IS NULL;
CREATE INDEX idx_mcp_sessions_last_active ON mcp_sessions(last_active_at) WHERE closed_at IS NULL;

COMMIT;
