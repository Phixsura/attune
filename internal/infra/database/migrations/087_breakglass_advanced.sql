-- Migration: 087_breakglass_advanced
-- Description: Advanced break-glass features (#158)
--   - Approval workflow
--   - Recovery codes (multiple one-time codes)
--   - Permission scoping
--   - Expiry warnings

-- 1. Approval workflow: pending tokens need approval from another admin
ALTER TABLE break_glass_tokens
ADD COLUMN IF NOT EXISTS requires_approval BOOLEAN DEFAULT FALSE,
ADD COLUMN IF NOT EXISTS approved_by TEXT DEFAULT NULL,
ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ DEFAULT NULL,
ADD COLUMN IF NOT EXISTS approval_expires_at TIMESTAMPTZ DEFAULT NULL,
ADD COLUMN IF NOT EXISTS justification TEXT DEFAULT NULL;

-- 2. Permission scoping: limit what the token can do
-- NULL = full admin, otherwise JSON array of permission strings
ALTER TABLE break_glass_tokens
ADD COLUMN IF NOT EXISTS scoped_permissions TEXT[] DEFAULT NULL;

-- 3. Recovery codes table: multiple one-time codes per tenant
CREATE TABLE IF NOT EXISTS break_glass_recovery_codes (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,
    label TEXT NOT NULL,  -- e.g., "Code 1", "Code 2", etc.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by TEXT NOT NULL,
    used_at TIMESTAMPTZ DEFAULT NULL,
    used_by_ip TEXT DEFAULT NULL,
    revoked_at TIMESTAMPTZ DEFAULT NULL,
    revoked_by TEXT DEFAULT NULL
);

CREATE INDEX IF NOT EXISTS idx_recovery_codes_tenant
ON break_glass_recovery_codes (tenant_id);

CREATE INDEX IF NOT EXISTS idx_recovery_codes_unused
ON break_glass_recovery_codes (tenant_id)
WHERE used_at IS NULL AND revoked_at IS NULL;

-- 4. Expiry warning tracking
CREATE TABLE IF NOT EXISTS break_glass_expiry_warnings (
    id TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    token_id TEXT NOT NULL REFERENCES break_glass_tokens(id) ON DELETE CASCADE,
    warning_type TEXT NOT NULL CHECK (warning_type IN ('24h', '1h', 'expired')),
    sent_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(token_id, warning_type)
);

COMMENT ON COLUMN break_glass_tokens.requires_approval IS
'If true, token cannot be used until approved by another admin.';

COMMENT ON COLUMN break_glass_tokens.approved_by IS
'Admin who approved this token (if requires_approval was true).';

COMMENT ON COLUMN break_glass_tokens.approval_expires_at IS
'Approval request expires after this time (default: 1 hour after issue).';

COMMENT ON COLUMN break_glass_tokens.justification IS
'Business justification for issuing this token (required if approval needed).';

COMMENT ON COLUMN break_glass_tokens.scoped_permissions IS
'Permissions granted by this token. NULL = full admin access.';

COMMENT ON TABLE break_glass_recovery_codes IS
'Multiple one-time recovery codes for offline break-glass access.';

COMMENT ON TABLE break_glass_expiry_warnings IS
'Tracks which expiry warnings have been sent to avoid duplicates.';
