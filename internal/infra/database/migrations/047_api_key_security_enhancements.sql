-- Migration 047: API key security enhancements
-- Adds industry-standard security features (#TBD):
--   1. Key expiration (expires_at) - GitHub/Cloudflare/Vercel standard
--   2. IP allowlist (allowed_cidrs) - Stripe/Cloudflare standard
--   3. Usage statistics (usage_count) - audit and monitoring
--   4. Per-key rate limiting (rate_limit_rpm) - OpenAI/Linear standard
--   5. Description field for key purpose documentation
BEGIN;

-- Add new columns to external_api_keys
ALTER TABLE external_api_keys
  ADD COLUMN IF NOT EXISTS expires_at      TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS allowed_cidrs   INET[],
  ADD COLUMN IF NOT EXISTS usage_count     BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS rate_limit_rpm  INT,
  ADD COLUMN IF NOT EXISTS description     TEXT;

-- Index for efficient expiration checks (only active, non-null expiry)
CREATE INDEX IF NOT EXISTS idx_api_keys_expires_at
  ON external_api_keys (expires_at)
  WHERE expires_at IS NOT NULL AND is_active = TRUE;

-- Index for keys expiring soon (dashboard "expiring soon" queries)
CREATE INDEX IF NOT EXISTS idx_api_keys_expiring_soon
  ON external_api_keys (tenant_id, expires_at)
  WHERE expires_at IS NOT NULL AND is_active = TRUE;

-- Comments for documentation
COMMENT ON COLUMN external_api_keys.expires_at IS 'Key expiration timestamp. NULL = never expires (legacy behavior).';
COMMENT ON COLUMN external_api_keys.allowed_cidrs IS 'IP allowlist in CIDR notation. NULL or empty = no IP restriction.';
COMMENT ON COLUMN external_api_keys.usage_count IS 'Total successful API calls made with this key.';
COMMENT ON COLUMN external_api_keys.rate_limit_rpm IS 'Per-key rate limit (requests per minute). NULL = use global limit.';
COMMENT ON COLUMN external_api_keys.description IS 'User-provided description of key purpose.';

COMMIT;
