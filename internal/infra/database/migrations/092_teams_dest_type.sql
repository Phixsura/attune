-- Migration 092: Extend tenant_notify_targets.destination_type CHECK
-- to include 'teams' (#202 Microsoft Teams outbound adapter).
--
-- Teams delivery uses an incoming webhook URL (Adaptive Card payload).
-- The URL carries the auth token in the path (URL-as-secret; same
-- pattern as Slack/Discord/Lark). Rendered as Adaptive Card 1.4.
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
        'discord',
        'teams'
    ));
