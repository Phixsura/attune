<!-- markdownlint-disable MD013 -->

# Jira Bidirectional Issue Sync

| Field | Value |
|---|---|
| Issue | [#226](https://github.com/Phixsura/attune/issues/226) |
| Status | Implemented |
| Started | 2026-07-18T17:22:12+08:00 |
| Related | [#214](https://github.com/Phixsura/attune/issues/214), [#212](https://github.com/Phixsura/attune/issues/212), [external sync framework](./2026-07-08-external-sync-framework.md), [customer requests](./2026-07-07-customer-requests.md) |

## Problem

Attune already has Customer Requests and a generic external sync framework, but
Jira is still missing as a first-class bidirectional provider. That leaves a
gap for product teams that manage delivery in Jira Product Discovery or Jira
delivery projects while tracking demand in Attune.

Without a Jira provider, operators cannot:

1. create or link Jira issues from customer requests;
2. keep issue status and labels reflected back into Attune;
3. record Jira webhook events with signing and deduplication;
4. see Jira-specific sync health, retries, and conflicts in the Console.

## Goals

- Add a Jira provider adapter that plugs into the existing external sync
  registry.
- Support Jira Cloud REST API v3 with tenant-scoped encrypted credentials.
- Require project and issue-type configuration for issue creation.
- Pull Jira issues into Attune as normalized external records.
- Push Attune customer requests to Jira issues, including status, labels, and
  request-context backlinks.
- Record Jira webhook deliveries in the existing external sync event ledger.
- Keep the Console flow generic so Jira can be configured with the current
  external sync UI.

## Non-goals

- Build a full Jira admin console for project, workflow, or custom field
  management.
- Support Jira Server/Data Center as a separate integration surface in the
  first pass.
- Recreate Jira workflows inside Attune.
- Add provider-specific UI beyond what the current generic external sync
  Console already supports.

## Acceptance criteria

- A customer request can create or link a Jira issue.
- Jira status and comment updates are reflected in Attune without duplicate
  links.
- Attune request context appears in Jira via backlink or comment.
- Failures and conflicts are visible to operators.

## Proposal

Implement a new `jira` provider under `internal/externalsync/adapter/jiraissue`
and register it from `cmd/attune/main.go`.

### Connection model

Use the existing `external_connections` row shape with provider config JSON to
hold Jira-specific settings such as:

- Jira site / API base URL
- project key
- issue type id or name
- optional auth metadata such as Atlassian account email
- optional status transition mapping for workflow-sensitive pushes

Credentials remain encrypted at rest. The provider will treat the credential as
the API token and compose the Jira Authorization header from provider config
plus stored secret material.

### Pull behavior

Use Jira issue search to fetch issues for the configured project, ordered by
updated time. Normalize each issue into a provider-neutral record that includes
the issue key, URL, status, labels, assignee, timestamps, and a compact payload
with enough detail for Attune diagnostics.

For request linking, the provider should extract an Attune request marker from
issue description or comments and populate `LocalObjectID` when present. That
keeps Jira issues idempotently bridged to the customer-request link ledger.

### Push behavior

Convert Attune customer-request push payloads into Jira create/update requests.
The provider should:

- create new issues with the configured project and issue type;
- update summary, description, and labels on existing issues;
- add or preserve an Attune backlink / request-context comment;
- transition issues into a Jira workflow state that matches the local request
  status, using a configurable mapping with a sensible heuristic fallback.

The adapter should return the created or updated Jira issue key, URL, and
version metadata so the framework can maintain link state and record failures
cleanly.

### Webhooks

Add a Jira webhook endpoint alongside the existing GitHub one and verify
`X-Hub-Signature` using the configured webhook secret. Jira webhook payloads
should be normalized into a compact event envelope with:

- webhook event type
- issue key / id
- changelog or comment summary
- timestamp
- dedupe key / external event id

When a webhook does not carry a usable signature secret, the handler should
reject it instead of silently accepting unauthenticated deliveries.

### Console and diagnostics

Reuse the current external sync Console pages. Jira connection setup should be
done through the existing generic provider, base URL, credential, webhook
secret, and provider config inputs. The connection qualification and sync run
diagnostics already provided by the framework will surface Jira-specific
failures once the adapter is registered.

## Alternatives considered

1. **Build a Jira-only sync path outside the framework.**
   Rejected because it would duplicate connection storage, run history,
   conflicts, retries, and webhook handling that already exist.
2. **Use only polling and skip webhooks.**
   Rejected because Jira already supports signed webhook delivery and the
   framework has a durable event ledger.
3. **Store Jira workflow logic in a new bespoke service layer.**
   Rejected because workflow-specific state belongs in provider config and the
   adapter, not in another parallel orchestration layer.

## Risks / tradeoffs

- Jira workflows differ across tenants, so status transitions may require a
  configuration fallback when the tenant’s workflow names do not match the
  heuristic.
- Jira description/comment payloads are ADF-based, so the adapter needs a small
  normalizer to keep payloads compact and readable.
- Webhook delivery is best-effort, so polling still needs to remain healthy even
  when webhook events are missing.

## Implementation plan

1. Add the Jira adapter package, config parsing, HTTP helpers, issue
   normalization, push logic, and signature classification.
2. Register the provider in `cmd/attune/main.go`.
3. Extend the external sync webhook handler and service with Jira delivery
   support.
4. Add adapter and handler tests with fake Jira responses and webhook fixtures.
5. Document the Jira provider setup in `docs/external-sync-adapters.md`.
6. Update the changelog and mark this proposal `Implemented` once the code
   lands.

## Implementation notes

- The Jira provider now lives under `internal/externalsync/adapter/jiraissue`.
- Signed webhook receipt is handled by `internal/handlers/externalsyncwebhook`
  and normalized in `internal/service/externalsync/jira.go`.
- External sync connection setup and operator guidance are documented in
  `docs/external-sync-adapters.md`.
- Console provider selection now reads the backend registry and hides
  test-only `noop` wiring so the create-connection dialog lists real adapters
  such as GitHub and Jira.
- Router registration and CLI bootstrap are wired through `cmd/attune/router.go`
  and `cmd/attune/main.go`.

## Verification

- `go test ./internal/externalsync/... ./internal/service/externalsync ./internal/handlers/externalsyncwebhook ./internal/repo/externalsync ./cmd/attune`
- focused fake-server coverage for Jira create/update/search/webhook paths
- Console smoke check for external sync connection creation and qualification

## References

- Jira Cloud REST API: issues, comments, links, and search
- Jira Cloud webhooks and HMAC signature verification
- Attune external sync framework and customer request issue-link ledger
