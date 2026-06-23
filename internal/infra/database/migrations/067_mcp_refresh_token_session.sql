-- Migration 060: Add session_id to refresh tokens
--
-- Refresh tokens must track their associated session for proper revocation
-- and session-based access control.

BEGIN;

ALTER TABLE mcp_oauth_refresh_tokens
    ADD COLUMN session_id UUID REFERENCES mcp_sessions(id) ON DELETE CASCADE;

CREATE INDEX idx_mcp_refresh_tokens_session ON mcp_oauth_refresh_tokens(session_id) WHERE revoked_at IS NULL;

COMMIT;
