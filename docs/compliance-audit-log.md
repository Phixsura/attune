# Compliance Audit Log

Attune records sensitive Console mutations in the append-only `audit_log` table.

## What is captured

- actor type and actor id
- request IP and user-agent
- action name
- target type and target id
- sanitized before/after payloads
- UTC creation timestamp

## What is intentionally excluded

- raw API keys
- inbound webhook secrets
- encrypted secret ciphertexts
- notify target URL credentials or query tokens in audit snapshots

## Access model

- read access is admin-only
- the Console exposes `/fb/v1/console/audit-log`
- CSV export is admin-only at `/fb/v1/console/audit-log/export.csv`

## Retention

- `audit.retention_days` controls how long rows are kept
- `audit.prune_interval` controls how often the background pruner runs
- the default policy keeps rows for 365 days and prunes every hour

## Export guidance

- the Settings > Audit Log table shows the latest 100 matching rows
- CSV export includes the full current filter result, not just the visible table page
- exported CSV contains sensitive operational metadata and should be handled like other internal security artifacts

## Operational notes

- ordinary `UPDATE`, `DELETE`, and `TRUNCATE` against `audit_log` are rejected
- retention pruning is the only supported delete path
- failed audit writes fail the calling Console mutation rather than silently dropping the audit event
