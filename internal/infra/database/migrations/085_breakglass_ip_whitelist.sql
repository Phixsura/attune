-- Migration: 085_breakglass_ip_whitelist
-- Description: Add IP whitelist support for break-glass tokens (#158)
--
-- Industry standard: Okta, AWS IAM, Google Workspace all support IP restrictions
-- on break-glass/emergency access. This prevents stolen tokens from being used
-- from unauthorized locations.

-- Add allowed_ips column to store CIDR ranges (e.g., '10.0.0.0/8', '192.168.1.100/32')
-- NULL means no IP restriction (backwards compatible)
ALTER TABLE break_glass_tokens
ADD COLUMN IF NOT EXISTS allowed_ips TEXT[] DEFAULT NULL;

-- Add used_from_ip to record the actual IP that used the token
-- Essential for security forensics
ALTER TABLE break_glass_tokens
ADD COLUMN IF NOT EXISTS used_from_ip TEXT DEFAULT NULL;

-- Add index for audit queries filtering by IP
CREATE INDEX IF NOT EXISTS idx_breakglass_used_ip
ON break_glass_tokens (used_from_ip)
WHERE used_from_ip IS NOT NULL;

COMMENT ON COLUMN break_glass_tokens.allowed_ips IS
'CIDR ranges allowed to use this token. NULL = any IP allowed.';

COMMENT ON COLUMN break_glass_tokens.used_from_ip IS
'Client IP that consumed the token. Recorded for security audit.';
