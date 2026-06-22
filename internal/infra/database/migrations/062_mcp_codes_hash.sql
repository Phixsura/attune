-- Migration 062: Hash authorization codes
--
-- Authorization codes should be stored hashed (like refresh tokens) to prevent
-- exposure if an attacker gains database read access.

BEGIN;

-- Rename code column to code_hash
ALTER TABLE mcp_oauth_codes RENAME COLUMN code TO code_hash;

-- Update the length constraint comment (hash is still 64 chars hex-encoded SHA-256)
-- The existing constraint chk_mcp_code_length already checks for 64 chars

COMMIT;
