-- Migration 058: Extend tenant_notify_targets.destination_type CHECK
-- to include 'discord' (#32 Discord outbound adapter).
--
-- Discord delivery uses an incoming webhook URL whose path carries the
-- auth token (URL-as-secret; no request signing), rendered as embed
-- objects. Same pattern as 'slack' (migration 033).
--
-- Idempotent: DROP IF EXISTS + ADD. Keeps every existing value.

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
        'slack',
        'discord'
    ));
