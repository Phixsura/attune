-- Add email-based invites to tenant_members (#38).
--
-- Invites are stored as tenant_members with:
--   member_type = 'invite'
--   user_id = '' (empty, filled when user accepts)
--   email = invited email address
--   accepted_at = NULL (pending)
--
-- When an OIDC user logs in, we match by email and convert the invite
-- to an actual membership (set member_type, user_id, accepted_at).

-- Add email column for invite matching
ALTER TABLE tenant_members
    ADD COLUMN IF NOT EXISTS email TEXT;

-- Update constraint to allow 'invite' member_type
ALTER TABLE tenant_members DROP CONSTRAINT IF EXISTS tenant_members_member_type_check;
ALTER TABLE tenant_members ADD CONSTRAINT tenant_members_member_type_check
    CHECK (member_type IN ('admin', 'oidc_user', 'tenant_user', 'invite'));

-- Index for email-based invite lookup
CREATE INDEX IF NOT EXISTS idx_tenant_members_email
    ON tenant_members (tenant_id, email)
    WHERE email IS NOT NULL;

-- Backfill email for existing admin members from admins table
UPDATE tenant_members tm
SET email = a.email
FROM admins a
WHERE tm.member_type = 'admin'
  AND tm.user_id = a.id::TEXT
  AND tm.email IS NULL;
