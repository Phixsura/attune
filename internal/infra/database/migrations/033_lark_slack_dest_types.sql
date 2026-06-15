-- Migration 033: Extend tenant_notify_targets.destination_type CHECK
-- to include 'lark' and 'slack' (#34 outbound adapter framework).
--
-- The #34 outbound framework adds native Lark card and Slack Block Kit
-- delivery. These channels use incoming webhook URLs (URL-as-secret for
-- Slack; URL + optional bot signature for Lark).
--
-- Note: 'lark-bot' was in the original migration 004 but was dropped in
-- migration 015. This re-adds Lark support as 'lark' (not 'lark-bot') to
-- distinguish the new outbound-adapter implementation.
--
-- Idempotent: DROP IF EXISTS + ADD.

ALTER TABLE tenant_notify_targets
    DROP CONSTRAINT IF EXISTS tenant_notify_targets_destination_type_check;

ALTER TABLE tenant_notify_targets
    ADD CONSTRAINT tenant_notify_targets_destination_type_check
    CHECK (destination_type IN (
        'raw-webhook',
        'slack-bot',
        'email',
        'github-issue',
        'lark',
        'slack'
    ));
