-- Migration 032: signature_version column for gradual content-hash signing rollout (#34).
--
-- The #34 outbound adapter framework introduces content-hash signing:
-- sha256(canonical(envelope)) instead of raw bytes. This is field-order
-- independent, solving the "customers verify against byte order" problem.
--
-- Migration strategy:
--   - New column `signature_version` with default 'v2-content-hash'
--   - Existing rows get 'v2-content-hash' (new default, customers must upgrade verifiers)
--   - Ops can set 'v2-bytes' for customers not yet upgraded
--   - Worker checks this column and applies the appropriate signing codepath
--
-- Values:
--   'v2-content-hash' — HMAC of sha256(canonical JSON); field-order independent (new default)
--   'v2-bytes'        — HMAC of raw JSON bytes; field-order dependent (legacy)
--
-- Idempotent: ADD COLUMN IF NOT EXISTS.

ALTER TABLE tenant_notify_targets
    ADD COLUMN IF NOT EXISTS signature_version TEXT NOT NULL DEFAULT 'v2-content-hash'
    CHECK (signature_version IN ('v2-content-hash', 'v2-bytes'));
