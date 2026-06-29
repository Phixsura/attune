-- Migration 097: Add 'linear' to destination_type CHECK constraint (#202).

ALTER TABLE notify_targets
    DROP CONSTRAINT IF EXISTS chk_destination_type;

ALTER TABLE notify_targets
    ADD CONSTRAINT chk_destination_type
        CHECK (destination_type IN (
            'raw_webhook', 'github_issue', 'slack', 'discord', 'email',
            'lark', 'generic', 'teams', 'jira', 'linear'
        ));
