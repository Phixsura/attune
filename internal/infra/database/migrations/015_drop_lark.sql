-- 015_drop_lark.sql — integral Lark removal (#66 Plan T19).
--
-- Hard-delete all Lark-bound data + schema. Pre-1.0 product; no
-- customer retention guarantee. The upstream cmd/attune startup path
-- runs database.ConfirmLarkDelete BEFORE this migration to guard
-- against silent loss when lark-* rows exist and the operator has not
-- opted in via ATTUNE_CONFIRM_LARK_DELETE=yes (see #66 Plan T10).

-- 1. Delete user_feedback rows produced via Lark source enums.
DELETE FROM user_feedback WHERE source LIKE 'lark-%';

-- 2. Delete notify_outbox rows targeting lark-bot destinations (if any
--    leaked through from earlier wave 2 work; harmless when zero).
DELETE FROM notify_outbox WHERE destination_type ILIKE '%lark%';

-- 3. Delete lark-typed notify_targets rows.
DELETE FROM tenant_notify_targets WHERE destination_type ILIKE '%lark%';

-- 4. Drop the Lark-origin user_feedback rows tagged with the legacy
--    inbound-webhook user_id prefix 'ext_<nil-uuid>:<open_id>'. The
--    tenant_users table itself stays — it holds tenant-scoped admins
--    that may exist for other (future) reasons; only the OAuth-bound
--    identity column (lark_open_id) is dropped if it ever existed.
DELETE FROM user_feedback WHERE user_id LIKE 'ext_00000000-0000-0000-0000-000000000000:%';
ALTER TABLE tenant_users DROP COLUMN IF EXISTS lark_open_id;

-- 5. Drop the Lark install columns / tables.
ALTER TABLE tenants DROP COLUMN IF EXISTS lark_install;
ALTER TABLE tenants DROP COLUMN IF EXISTS lark_tenant_key;
DROP TABLE IF EXISTS tenant_lark_install;
DROP TABLE IF EXISTS lark_install;
