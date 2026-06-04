-- Migration 011: Extend tenant_notify_targets.destination_type CHECK
-- to include 'github-issue' (Sprint 1 of Y1 工程, 2026-05-17).
--
-- Sprint 1 ships native GitHub Issue dispatch — listen converts each
-- enriched feedback row into a `POST /repos/{owner}/{repo}/issues`
-- against the customer's GitHub repo, using the same outbox + Transport
-- pipeline that raw-webhook uses. This unblocks the outcome pricing
-- story (¥80/PR) — until this lands the only path was the generic raw-
-- webhook framework, which requires the customer to host a receiver.
--
-- The CHECK constraint name follows PG's auto-generated convention
-- (<table>_<column>_check). DROP IF EXISTS keeps re-runs idempotent.

ALTER TABLE tenant_notify_targets
    DROP CONSTRAINT IF EXISTS tenant_notify_targets_destination_type_check;

ALTER TABLE tenant_notify_targets
    ADD CONSTRAINT tenant_notify_targets_destination_type_check
    CHECK (destination_type IN (
        'raw-webhook',
        'lark-bot',
        'slack-bot',
        'email',
        'github-issue'
    ));

-- url + secret semantics for the new type:
--   url    = https://github.com/{owner}/{repo}    (repo HTTP URL)
--   secret = GitHub Personal Access Token (ghp_…) or App installation
--            token; needs `issues:write` scope on the target repo
--
-- destination_target column (the actual sender address) for github-issue
-- is the same as url here — the worker derives the API endpoint
-- (https://api.github.com/repos/{owner}/{repo}/issues) at send time.
