-- 015_drop_lark.sql — integral Lark removal (#66 Plan T19).
--
-- Hard-delete all Lark-bound data + schema. Pre-1.0 product; no
-- customer retention guarantee. The upstream cmd/attune startup path
-- runs database.ConfirmLarkDelete BEFORE this migration to guard
-- against silent loss when lark-* rows exist and the operator has not
-- opted in via ATTUNE_CONFIRM_LARK_DELETE=yes (see #66 Plan T10).

-- 1. Delete user_feedback rows produced via Lark source enums.
DELETE FROM user_feedback WHERE source LIKE 'lark-%';

-- 2. Delete outbox rows targeting lark-bot channels (if any leaked
--    through from earlier wave 2 work; harmless when zero).
DELETE FROM outbox WHERE channel ILIKE 'lark%';

-- 3. Delete lark-typed notify_targets rows.
DELETE FROM tenant_notify_targets WHERE destination_type ILIKE '%lark%';

-- 4. Drop the Lark-origin tenant_users rows + their identity column.
--    The user_id prefix 'ext_<nil-uuid>:<open_id>' was the legacy Lark
--    inbound-webhook identity stamp; the column lark_open_id was the
--    OAuth identity stamp.
DELETE FROM tenant_users WHERE user_id LIKE 'ext_00000000-0000-0000-0000-000000000000:%';
ALTER TABLE tenant_users DROP COLUMN IF EXISTS lark_open_id;

-- 5. Drop the Lark install columns / tables.
ALTER TABLE tenants DROP COLUMN IF EXISTS lark_install;
ALTER TABLE tenants DROP COLUMN IF EXISTS lark_tenant_key;
DROP TABLE IF EXISTS tenant_lark_install;
DROP TABLE IF EXISTS lark_install;
