-- Migration 096: Add 'jira' to destination_type CHECK constraint (#202).

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
            'teams',
            'jira'
        ));
