-- Track the most recent failure per notify target so customers see
-- "your webhook has been failing for 2h" in the console + via a
-- lark-bot self-report (Phase 3.2).
--
-- Cleared on next successful delivery — represents "current alert state",
-- not historical log. Full delivery history lives in notify_outbox.

ALTER TABLE tenant_notify_targets
    ADD COLUMN IF NOT EXISTS last_failure_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_error      TEXT NOT NULL DEFAULT '';
