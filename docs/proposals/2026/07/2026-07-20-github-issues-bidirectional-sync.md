<!-- markdownlint-disable MD013 -->

# GitHub Issues Bidirectional Sync

| Field | Value |
|---|---|
| Issue | [#228](https://github.com/Phixsura/attune/issues/228) |
| Status | Implemented |
| Started | 2026-07-20T10:55:59+08:00 |
| Related | [#202](https://github.com/Phixsura/attune/issues/202), [#214](https://github.com/Phixsura/attune/issues/214), [#226](https://github.com/Phixsura/attune/issues/226), [external sync framework](./2026-07-08-external-sync-framework.md), [customer requests](./2026-07-07-customer-requests.md), [customer request delivery health rollup](./2026-07-07-customer-request-delivery-health-rollup.md) |

## Problem

Attune can already create GitHub Issues through the legacy outbound path and can
pull or push basic GitHub issue fields through the generic external sync
framework. That is enough to prove connectivity, but it is not enough for
GitHub to act as a real delivery system for Product Signal OS.

Issue #228 asks for a bidirectional GitHub Issues sync that can create or link
issues, keep delivery state visible in Attune, sync comments without duplicate
comments across retries, preserve existing outbound behavior, and use webhook
or polling delivery with retry and dedupe. The current implementation leaves
several important gaps:

- the GitHub external sync provider advertises issue fields but not comments;
- GitHub webhook receipt validates and records deliveries, but does not
  automatically enqueue a sync run;
- manual Customer Request issue links write `customer_request_issue_links`
  without creating the generic `external_object_links` identity bridge;
- the legacy `github-issue` outbound adapter creates GitHub issues without
  storing the created issue key or URL in external sync link state;
- GitHub status, labels, assignees, close events, and comments are not yet
  projected into a provider-aware delivery timeline that operators can trust.

The product risk is not only missing data. A careless bidirectional sync can
overwrite engineer-written issue bodies, leak private request notes into
GitHub, duplicate comments during webhook redelivery, or let the same external
issue become linked to multiple requests. The design needs to make the durable
identity model and the safe comment boundary explicit before adding more
provider behavior.

## Goals

- Let an operator create a GitHub Issue for one selected Customer Request
  through the shared external sync framework.
- Let an operator link an existing GitHub Issue to a Customer Request by URL, or
  by `connection_id` plus issue number.
- Normalize GitHub issue identity so manual links, provider pulls, provider
  pushes, and managed Customer Request issue creates converge on the same
  `external_object_links` row.
- Pull GitHub issue state, labels, assignees, close timestamps, URLs, and
  comment summaries into Attune delivery context.
- Push safe Attune request context to GitHub without replacing
  engineer-authored issue body content.
- Add a provider-aware delivery comment ledger that dedupes inbound GitHub
  comments and the Attune-managed backlink comment across polling, webhook
  delivery, manual replay, and push retries.
- Convert verified GitHub `issues` and `issue_comment` webhooks into queued
  external sync runs through the existing event ledger, with enough run metadata
  to fetch the touched issue and comment.
- Keep polling/backfill healthy so missed webhooks, disabled webhooks, and
  provider outages are recoverable.
- Preserve the public `github-issue` outbound target behavior while making the
  new Customer Request issue-create path use external sync.
- Add focused tests, operator-facing diagnostics, adapter documentation, and
  changelog coverage with the implementation.

## Non-goals

- Do not build a GitHub Projects, Pull Request, branch, commit, or check-run
  integration in this issue.
- Do not mirror every GitHub Issue field into first-class Attune columns.
- Do not make GitHub the source of truth for the Customer Request lifecycle.
- Do not auto-sync private Customer Request notes or public portal comments to
  GitHub.
- Do not auto-publish GitHub comments into the public portal.
- Do not add arbitrary operator-authored outbound GitHub comments. The only
  outbound comment in scope is the Attune-managed backlink/request-context
  comment.
- Do not support destructive deletes as a default behavior.
- Do not add GitHub OAuth or GitHub App installation flows in this issue.
- Do not replace the generic external sync framework with a GitHub-specific
  orchestration path.
- Do not implicitly create or enable push-capable mappings from a manual link
  action.
- Do not retrofit feedback-only legacy `github-issue` outbox deliveries into
  managed external links unless the delivery envelope carries a Customer Request
  id and the outbound transport exposes a response-capture hook.

## MVP Acceptance Criteria

- An operator can create a new GitHub Issue for one selected Customer Request,
  and Attune stores the resulting external key, URL, version, and link state.
- A Customer Request can link to an existing GitHub Issue and subsequent pulls use
  the same generic external object link when a matching managed GitHub
  connection and enabled mapping exist.
- GitHub issue state changes, labels, assignees, close events, and updated
  timestamps update request delivery context.
- GitHub comments appear in a provider-scoped delivery timeline without
  becoming private notes or public portal comments.
- The Attune-managed GitHub backlink/request-context comment carries stable
  markers and is not duplicated by retries, webhook redelivery, polling, or
  manual replay.
- Verified GitHub `issues` and `issue_comment` webhooks create or reuse a
  queued sync run, include issue/comment hints for the provider, and link the
  event row to that run.
- Polling/backfill can recover missed issue and comment updates.
- Existing `github-issue` notify targets remain compatible.
- Operators can diagnose connection, run, event, record-failure, conflict, and
  comment-sync state from the existing Console surfaces or small extensions to
  those surfaces.

## MVP Scope

This proposal intentionally keeps #228 to the smallest end-to-end slice that
makes GitHub Issues a managed delivery system:

- managed link-existing for GitHub Issues when a tenant already has an enabled
  GitHub connection and customer-request issue mapping for the repository;
- managed single-request create through external sync, not through the legacy
  feedback outbox adapter;
- pull of issue state, labels, assignees, close timestamps, URLs, provider
  updated timestamps, and inbound comments;
- push of a managed issue body section, managed labels, conservative state
  changes, and one managed backlink/request-context comment;
- provider-neutral child-record support for comments so generic repository code
  does not parse GitHub-specific payloads;
- webhook-to-run automation with event hints for `issues` and `issue_comment`;
- polling and backfill as reconciliation paths;
- legacy `github-issue` direct POST behavior preserved unchanged for existing
  feedback-oriented notify targets.

Out of scope for #228:

- GitHub Projects, Pull Requests, branches, commits, checks, and deployments;
- arbitrary operator-authored outbound GitHub comments;
- public portal comment publishing from GitHub comments;
- full legacy outbox response capture and retroactive link creation for
  feedback-only events;
- broad Console timeline redesign beyond the fields needed to inspect runs,
  events, conflicts, links, and comment sync rows.

## Current State

### External Sync Framework

The generic framework already owns the hard operational parts: tenant-scoped
connections, encrypted credentials and webhook secrets, mappings, object links,
cursors, runs, attempts, record failures, conflicts, events, worker retry, and
Console diagnostics. GitHub is already registered as provider token `github`.

`external_object_links` is the right identity bridge for GitHub Issues. The
table already has a unique active local link per mapping and a unique active
external key per mapping, which prevents one request from silently taking over
another request's GitHub issue.

`external_sync_events` is the right webhook ledger. It stores provider, event
type, external event id, dedupe key, signature status, normalized payload, and
the queued run id for verified GitHub deliveries that match an enabled mapping.

### GitHub Provider

The GitHub adapter can:

- validate connection settings and token access;
- discover an `issue` object schema;
- pull issues from `GET /repos/{owner}/{repo}/issues` with `state=all`,
  `sort=updated`, and ascending updated order;
- skip GitHub Pull Requests, which are returned by the Issues API;
- normalize issue number, title, state, labels, assignees, URL, and timestamps;
- extract `<!-- attune:customer_request_id=<uuid> -->` from issue bodies;
- fetch issue comments as provider-neutral child records;
- emit deleted comment tombstones from verified `issue_comment.deleted`
  webhook metadata;
- create or update issues through `POST` and `PATCH`;
- map Attune `shipped` and `cancelled` request statuses to closed GitHub
  issues while keeping other statuses open.

The provider advertises comment fields in discovery and keeps webhook-triggered
single-issue pulls from advancing the scheduled cursor. It still relies on the
framework for durable comment dedupe, event dedupe, retries, and record
timeline projection.

### Customer Request Issue Links

Customer Requests already expose manual issue linking and issue sync state.
That path now bridges into generic external sync when an active GitHub
connection and enabled Customer Request issue mapping match the linked
repository. The GitHub URL parser may keep an operator-facing key in
`owner/repo#number` form, while the generic `external_object_links` row stores
the connection-scoped issue number and becomes the durable identity shared by
manual links, pulls, pushes, tombstones, and timeline entries.

### Legacy GitHub Outbound

The legacy `github-issue` outbound adapter still creates GitHub Issues directly
from notify target configuration. This behavior needs to keep working because
existing tenants may rely on it. The legacy path is feedback-oriented and does
not currently expose a transport hook that can read a successful GitHub response
body and write the created issue number back into domain state. The managed
Customer Request create path therefore needs to use external sync directly,
while the legacy adapter remains compatible and unchanged unless a response
capture hook is deliberately added.

## Industry Synthesis

Sampled systems converge on a small set of patterns:

| Product family | Examples | Observed pattern | Decision for Attune |
|---|---|---|---|
| Engineering issue trackers | Linear, Plane, Huly, Zenhub, Zube | GitHub issues are first-class delivery objects. Comments, labels, assignees, status, and links are explicit sync surfaces. | Treat GitHub Issue identity and comments as first-class external sync records. |
| Product feedback and roadmap tools | Productboard, Canny, Featurebase, Aha!, UserVoice, airfocus | The product object stays primary. GitHub is delivery context. Status mapping and backlink comments are common; full field mirroring is rare. | Keep Customer Requests primary and sync a safe, provider-aware delivery context. |
| Support and customer communication tools | Zendesk, Intercom, Freshdesk, Help Scout, Front | Tickets can create or link GitHub issues, but private support conversations are not blindly mirrored. | Never auto-sync private notes or portal comments to GitHub. |
| Error monitoring tools | Sentry, Bugsnag, Rollbar, Honeybadger, Raygun | Errors create or link GitHub issues. Resolution, assignment, and selected comments may sync back. | Make comments opt-in and marker-based, with inbound GitHub comments visible in delivery history. |
| Sync platforms | Unito, Exalate, Zapier, Make, n8n, Workato | Durable identity mappings, field direction, retry, cursor, dedupe, and conflict handling define reliability. | Reuse external sync framework primitives instead of adding a GitHub-specific worker. |
| GitHub API tools | GitHub-native boards, importers, local sync tools | Incremental polling and idempotent upsert remain necessary even when webhooks exist. | Keep polling/backfill as the recovery path for every webhook-driven behavior. |

The strongest designs do not implement an unrestricted two-way mirror. They
define object identity, field ownership, comment ownership, event dedupe, and
operator recovery paths. Attune should follow that model.

## Design Principles

1. **Customer Requests remain primary.** GitHub is a delivery system and a
   source of delivery activity, not the owner of demand, scoring, votes, or
   public visibility.
2. **One external identity row per linked issue.** Every managed Customer
   Request issue create, manual link, provider pull, and webhook replay must
   converge on one `external_object_links` row.
3. **Fields have owners.** Attune owns request context, managed labels, and
   request lifecycle intent. GitHub owns engineer edits, native discussion,
   issue close reason, assignee, title on linked-existing issues, and provider
   timestamps unless a mapping explicitly says otherwise.
4. **Comments are not notes.** GitHub comments are delivery comments. They do
   not become internal collaboration notes or public portal comments.
5. **Issue bodies are shared documents.** Attune may write a bounded
   sync-managed section with stable markers, but must preserve content outside
   that section.
6. **Webhook handling is asynchronous.** The handler verifies, records, and
   enqueues work. Provider API reads and writes happen in worker runs.
7. **Polling is the consistency backstop.** Every webhook-derived state change
   must also be discoverable through polling or backfill.
8. **At-least-once work requires idempotency.** Every outbound issue create,
   update, and comment create needs stable markers or external IDs so retries
   become no-ops or updates.
9. **Conflicts are visible.** Ambiguous local/external ownership, duplicate
   markers, or cross-request links produce conflict records, not silent
   reassignment.
10. **Provider data is redacted and compact.** Normalized payloads should be
    useful for diagnosis without storing unnecessary raw GitHub response data.

## Proposal

Upgrade the existing `github` provider and shared external sync service rather
than adding a new GitHub-specific sync service. The implementation has eight
cooperating pieces:

1. canonical GitHub issue identity and link bridging;
2. provider contract extensions for event-scoped runs and child records;
3. single-request create through external sync;
4. safer GitHub issue pull and push behavior;
5. delivery comment ledger and comment sync;
6. webhook-to-run automation;
7. polling and backfill coverage;
8. Console, metrics, audit, docs, and tests.

### Canonical GitHub Issue Identity

Keep `external_object_links.external_key` as the GitHub issue number string
inside a GitHub connection. A connection already points at one repository
through `provider_config.owner` and `provider_config.repo`, so the issue number
is stable and unique within the mapping.

Normalize every operator input into:

- `owner`
- `repo`
- `number`
- `external_key`: the decimal issue number string
- `external_url`: the canonical browser URL
- `provider_display_key`: `owner/repo#number`

`provider_display_key` can live in normalized payloads, API responses, and
Console text. It should not replace the provider-scoped `external_key` in the
generic link table because the mapping already scopes the repository.

When `LinkCustomerRequestIssue` receives a GitHub URL, or a `connection_id`
plus issue number, and the tenant has a matching managed GitHub connection, it
should:

1. parse and validate the issue locator;
2. validate that the locator repository matches the selected connection or the
   repository described by connection provider config;
3. require an existing enabled `customer_request` to `issue` mapping for that
   connection whose direction allows pull or bidirectional sync;
4. upsert `external_object_links` with local object type `customer_request`,
   local object id, external object type `issue`, external key number, URL, and
   sync state `pending`;
5. upsert `customer_request_issue_links` with `external_object_link_id`;
6. enqueue a pull run with input metadata containing the issue number so Attune
   reads provider state instead of trusting only operator-entered URL and title.

When `LinkCustomerRequestIssue` receives a GitHub URL but no matching managed
connection or enabled mapping exists, it should keep the current passive
`customer_request_issue_links` behavior and record a sync state explaining that
no managed GitHub connection owns the URL. It must not create or enable a
mapping as a side effect of manual linking.

For GitHub Enterprise, URL matching must compare the browser host and
repository path against the connection's configured browser/API host pair. A
connection configured with `base_url` or `provider_config.api_base_url` should
also provide or derive a browser base URL; link-existing should reject URLs
whose host cannot be tied to the selected connection.

Deprecated manual forms such as `owner/repo#number` may still be accepted by
Console helpers when the target connection is selected. Raw issue numbers are
accepted only with an explicit `connection_id`.

### Single-Request Create

Add a managed create action that takes:

- tenant id;
- customer request id;
- optional GitHub connection id;
- optional mapping id;
- actor.

The HTTP contract is
`POST /fb/v1/console/customer-requests/{id}/issue-links:create-github`, returning
the refreshed Customer Request detail plus the queued run, connection, and
mapping ids. The service resolves an existing enabled GitHub mapping whose
direction allows push or bidirectional sync. When selectors are omitted, there
must be exactly one eligible mapping; otherwise the request returns conflict so
operators do not accidentally create issues in the wrong repository.

The service then inserts or reuses a sync run with trigger `manual`, direction
`push`, and input metadata:

```json
{
  "local_object_id": "00000000-0000-0000-0000-000000000000",
  "source": "customer_request_issue_create"
}
```

`PreparePushRecords` honors this metadata by selecting only the requested
Customer Request. The managed create action rejects requests that already have a
GitHub issue link or active external object link for the selected mapping,
because the operator-facing command is create, not update. If a queued or
running create run already exists for the same request and mapping, the repo
returns that run instead of enqueueing another write.

### Provider Contract Extensions

Add two small generic extensions to the external sync framework.

`external_sync_runs.input_metadata`

- JSON object, default `{}`;
- set by manual single-object create, webhook event replay, and webhook
  auto-enqueue;
- redacted in API responses when needed;
- not part of provider credentials or mapping config.

`externalsync.PullRequest.InputMetadata`

- raw JSON object copied from the run;
- used by providers as hints, not as the only source of correctness.

`externalsync.PullResult.Children`

- provider-neutral child records associated with parent external records;
- first supported child type is `comment`;
- each child record includes parent key, child type, child key, URL, version,
  updated timestamp, deleted flag, and normalized payload.

The repository layer should apply child records by child type and generic
identity fields. Provider-specific JSON parsing stays in adapters or
provider-aware service translation code, not in generic repository functions.

For #228, GitHub webhook input metadata should use this shape:

```json
{
  "provider_event_id": "github-delivery-id",
  "event_type": "issue_comment",
  "action": "created",
  "repository": {
    "id": "123",
    "full_name": "acme/app"
  },
  "issue": {
    "number": 42
  },
  "comment": {
    "id": "987654321"
  }
}
```

Provider hints let a webhook-triggered pull fetch the touched issue or comment
directly. Polling and backfill remain the reconciliation path when hints are
missing, stale, or insufficient.

### Link Conflict Rules

The generic uniqueness constraints already prevent most silent conflicts. The
service should add product-level conflict records for the cases operators need
to understand:

- the same GitHub issue is already linked to another active Customer Request;
- one Customer Request already has a different active GitHub issue in the same
  mapping;
- a pulled GitHub issue marker points at one request but the existing external
  link points at another.

Default conflict policy should be `manual`. A conflict should not unlink or
reassign objects unless an operator resolves it.

### GitHub Pull Behavior

Extend the GitHub provider discovery schema for `issue` with:

- `comment_count`
- `comments`
- `request_marker`
- `provider_display_key`

Issue list polling should continue to use `state=all`, updated ordering, and
pagination. The provider should continue to skip Pull Requests returned by the
Issues API.

For each changed issue, normalize:

- issue number, URL, title, state, state reason, locked flag;
- labels and assignees;
- created, updated, and closed timestamps;
- repository owner, repository name, and display key;
- request marker found in the issue body or sync-managed body section;
- comment count and a compact comment summary.

The provider should fetch comments for records that are likely to be relevant:

- issues with a known Attune request marker;
- issues with an existing external link;
- issues included in a webhook-triggered pull;
- issues whose comment count or updated timestamp changed since the previous
  version.

The provider should request comments through GitHub's issue comments API and
normalize only the fields needed for sync:

- provider comment id;
- author login and type;
- body digest and bounded body text;
- created and updated timestamps;
- browser URL;
- Attune comment marker, when present.

The pull result remains provider-neutral. Issue fields stay in the issue record
payload. Comments are emitted as `PullResult.Children` with child type
`comment`, and are then stored in the comment ledger described below. Generic
repository code should not parse GitHub-shaped issue payloads to find comments.

### GitHub Push Behavior

Push should resolve the target issue in this order:

1. use `LocalRecord.ExternalKey` when it is present;
2. use an existing `external_object_links` row for the mapping and local object
   id when the push run is single-object scoped;
3. use an operator-provided URL or connection-scoped issue number from the link
   action when present;
4. search for an existing issue with an Attune managed label, backlink marker,
   or request marker when the local record has a customer request id but no
   external key;
5. create a new issue when no existing issue is found;
6. record a conflict if multiple existing issues contain the same request
   marker.

GitHub search indexing can lag behind issue writes, so marker search is a
fallback, not the primary identity source.

Create should set:

- title from the Customer Request summary;
- body with a sync-managed Attune section and hidden request marker;
- configured default labels plus request-derived labels;
- optional assignees only when the mapping config explicitly allows Attune to
  write assignees.

Update should preserve GitHub-authored body content. For linked-existing issues,
the default write policy is `read_state_with_backlink`: Attune may write or
update only the managed backlink/request-context comment and may pull provider
state. Updating title, managed labels, issue body sections, or reopen state
requires mapping config to opt in to `write_managed_fields`.

When body writes are enabled, the adapter should replace only the sync-managed
section delimited by stable comments such as:

```text
<!-- attune:section:start customer_request -->
...
<!-- attune:customer_request_id=<uuid> -->
<!-- attune:section:end customer_request -->
```

When the marker exists without a section, the provider should append or replace
only the marker and managed summary block, leaving unrelated body text intact.

Status push should be conservative:

- local `shipped` and `cancelled` can close GitHub issues;
- local `open`, `planned`, and `in_progress` can reopen GitHub issues only when
  mapping config allows reopening;
- GitHub close reason is read from GitHub and exposed in delivery context, not
  invented by Attune.

Label push should write only configured Attune-managed labels. Because GitHub
issue label updates can replace the label set, the adapter must perform a
read-modify-write operation: read existing labels, remove only labels that match
the configured managed prefix or explicit allowlist, merge the desired managed
labels, and write the merged set back.

### Delivery Comment Ledger

Add a provider-neutral comment ledger in the next migration, currently
`112_github_bidirectional_issue_sync.sql`.

`external_object_comments`

- `id`
- `tenant_id`
- `external_object_link_id`
- `provider`
- `external_object_type`
- `external_key`
- `direction`
- `origin`
- `provider_comment_id`
- `local_comment_id`
- `author_display`
- `author_external_id`
- `body`
- `body_digest`
- `marker`
- `external_url`
- `external_created_at`
- `external_updated_at`
- `last_synced_at`
- `sync_state`
- `sync_error`
- `external_sync_event_id`
- `first_run_id`
- `last_run_id`
- `created_by`
- `updated_by`
- `body_truncated`
- `deleted_at`
- `created_at`
- `updated_at`

Constraints:

- `direction` in `pull`, `push`;
- `origin` in `external`, `attune`, `system`;
- `sync_state` in `pending`, `synced`, `conflict`, `failed`, `deleted`;
- unique active row by `(tenant_id, external_object_link_id,
  provider_comment_id)` when `provider_comment_id` is not empty;
- unique active row by `(tenant_id, external_object_link_id, local_comment_id)`
  when `local_comment_id` is not empty;
- unique active row by `(tenant_id, external_object_link_id, marker)` when
  `marker` is not empty;
- `body_digest` is `sha256` over the normalized body text;
- body length cap matching Customer Request comments unless a smaller cap is
  selected for provider payload storage; truncated rows set `body_truncated`;
- payload redaction rules must exclude raw tokens and private provider headers.

Inbound GitHub comments should be stored in this ledger and exposed as delivery
timeline entries. They should not create `customer_request_notes` rows and
should not create `customer_request_comments` rows.

Outbound Attune comments should be limited to the request context/backlink
comment that Attune manages. Arbitrary operator-authored outbound comments need
a separate local source model and are not part of #228.

Each outbound comment body must include a hidden marker:

```text
<!-- attune:comment_id=<uuid> -->
<!-- attune:customer_request_id=<uuid> -->
```

Before creating a GitHub comment, the provider should list issue comments and
search for the marker. If the marker exists and the body digest matches, push is
a no-op. If the marker exists and the digest differs, update the existing
GitHub comment when the token has permission. If update is not permitted, record
a conflict or record failure instead of creating a duplicate comment.

Webhook redelivery and polling should dedupe inbound comments by GitHub comment
id. Outbound echo events should be matched by marker and update the existing
outbound ledger row with the provider comment id.

### Webhook-To-Run Automation

Extend GitHub webhook normalization to support these events:

- `ping`
- `issues`
- `issue_comment`

The handler should continue to verify `X-Hub-Signature-256` with the encrypted
connection webhook secret, record `X-GitHub-Delivery` as the external event id,
and store a compact normalized payload. For GitHub, the dedupe key should remain
the delivery id when present, with a deterministic fallback based on event type,
repository id/full name, issue number, action, comment id, and payload digest.
Verified `ping` deliveries are recorded for setup diagnostics and do not enqueue
sync runs.

After a verified `issues` or `issue_comment` event is recorded or deduped, the
service should:

1. resolve the enabled mapping for the connection and object type `issue`;
2. ignore unsupported events with status `ignored`;
3. build run input metadata from the normalized event payload;
4. create one queued pull run with trigger `webhook` for a newly recorded event,
   or reuse the existing `external_sync_events.run_id` when the delivery was
   already deduped;
5. attach `external_sync_events.run_id` to that run;
6. record audit metadata that includes provider, event type, delivery id, issue
   number, and run id.

The worker should process webhook-triggered pull runs through the same provider
contract used by manual and scheduled pulls. This keeps retry, quarantine,
record failure, and cursor behavior consistent.

Webhook-triggered pulls should use input metadata as a provider hint. For an
`issues` event, the GitHub provider should fetch the touched issue directly
before or instead of scanning the cursor page. For an `issue_comment` event, it
should fetch the touched issue and the touched comment directly, then emit the
comment as a child record. The correctness requirement remains eventual
convergence: polling and backfill must still find the same state if a webhook
hint is missing, stale, or fails.

### Polling And Backfill

Polling remains required. Operators should be able to run:

- ordinary pull sync to catch issue state and comment updates;
- backfill from an empty or reset cursor;
- record-failure retry for records that failed to apply;
- event replay for verified events that did not produce a successful run.

Cursor advancement should stay conservative:

- do not advance cursor on connection-wide failure;
- advance cursor for successful records only when record failures are replayable
  without losing provider updates;
- use provider updated timestamps as high watermarks and include a small overlap
  window to handle equal timestamps and GitHub pagination boundaries.

### Delivery Context Projection

When a GitHub issue is linked to a Customer Request, pull should update:

- `customer_request_issue_links.title`;
- `customer_request_issue_links.status`;
- `customer_request_issue_links.sync_state`;
- `customer_request_issue_links.external_status_category`;
- `customer_request_issue_links.external_assignee`;
- `customer_request_issue_links.external_updated_at`;
- `customer_request_issue_links.last_synced_at`;
- `customer_request_issue_links.sync_error`;
- `customer_request_issue_links.external_object_link_id`.

GitHub status category should be normalized as:

- `open` when issue state is `open`;
- `closed` when issue state is `closed`;
- `unknown` when the provider payload is malformed or missing.

Labels and assignees should remain in normalized payloads and delivery timeline
details unless a migration adds first-class columns. The request delivery
health rollup can read those provider payloads through the external link when
needed.

Close events should not directly set `customer_requests.status` by default.
They should update delivery context and may produce an operator-visible
suggestion when GitHub closed state conflicts with Attune lifecycle state.

### Legacy Outbound Compatibility

Keep `destination_type = "github-issue"` working for existing notify targets.
For #228, the managed Customer Request GitHub create action uses external sync
directly. The existing feedback-oriented outbound adapter remains a direct POST
path and should not attempt to infer Customer Request links from feedback-only
payloads.

If a legacy outbound envelope already carries an explicit Customer Request id
and the outbound transport gains a success response-capture hook, the same
external link upsert helper used by managed creates can be reused. Without both
conditions, the legacy delivery remains a notification-only GitHub issue create.
This preserves compatibility without promising link state that the current
outbound framework cannot observe.

### Provider Configuration

Extend GitHub provider config with optional keys:

```json
{
  "owner": "acme",
  "repo": "app",
  "managed_label_prefix": "attune/",
  "default_labels": ["attune/request"],
  "sync_comments": true,
  "sync_assignees": false,
  "allow_reopen": false,
  "linked_existing_write_policy": "read_state_with_backlink",
  "body_section_mode": "managed_section"
}
```

Defaults:

- `managed_label_prefix`: `attune/`
- `default_labels`: empty
- `sync_comments`: true for inbound comment capture and request-context
  comments
- `sync_assignees`: false
- `allow_reopen`: false
- `linked_existing_write_policy`: `read_state_with_backlink`
- `body_section_mode`: `managed_section`

Provider config validation should reject malformed owners, repos, label
prefixes, unsupported linked-existing write policies, and unsupported body
section modes. Validation errors should be redacted and size-capped.

### Console And API

The generic external sync Console can remain the main operator surface. Small
extensions are needed:

- connection setup help for GitHub `issues` and `issue_comment` webhook events;
- link-existing GitHub issue action from Customer Request detail;
- delivery timeline entries for GitHub issue changes and comments;
- conflict cards for duplicate links, duplicate markers, and comment write
  conflicts;
- run detail links that show event id, issue number, comment id, and provider
  browser URLs when present;
- mapping preview warnings for comment sync enabled without issue read/write
  scopes.

Generated API changes should remain provider-neutral where possible. If a new
comment endpoint is required, prefer an external sync or Customer Request
delivery-comment API rather than a GitHub-specific handler.

### Security And Privacy

- Managed GitHub webhooks require a configured webhook secret.
- Reject unsigned GitHub webhook deliveries and deliveries whose
  `X-Hub-Signature-256` does not match the configured secret.
- Reject webhook bodies that exceed the existing handler body cap.
- Store compact normalized payloads, not full raw GitHub payloads.
- Never log GitHub tokens, webhook secrets, Authorization headers, raw webhook
  signatures, or private request content.
- Do not sync internal Customer Request notes to GitHub.
- Do not sync public portal comments to GitHub without a separate explicit
  product decision and consent model.
- Treat GitHub user logins as provider identity data; display them in operator
  surfaces but do not merge them into customer identity records.
- Preserve GitHub-authored body content outside the Attune managed section.

### Observability

Add or extend metrics with low-cardinality labels:

- GitHub pull records seen, changed, failed;
- GitHub push write results and record failures;
- GitHub webhook deliveries received, ignored, deduped, and replayed;
- GitHub comment records inserted, updated, ignored, and failed;
- webhook-triggered runs queued and completed;
- conflict counts by kind.

Audit events should cover:

- GitHub issue linked to request;
- GitHub issue unlinked from request;
- GitHub webhook event replayed;
- GitHub delivery comment created or ignored by sync;
- conflict resolved for GitHub issue or comment sync.

## Alternatives Considered

### GitHub-Specific Sync Tables Only

Rejected. GitHub needs the same connection, mapping, run, retry, cursor,
failure, conflict, and event behavior that the generic external sync framework
already provides. Provider-specific tables would split diagnostics and make
Jira, Linear, and other providers harder to operate consistently.

### Full Two-Way Field Mirror

Rejected. A full mirror is unsafe because GitHub issue bodies and labels are
shared with engineers and automation. Attune should own a managed section and
managed labels, while GitHub remains the owner of native engineering metadata
unless mapping config explicitly grants Attune write authority.

### Treat GitHub Comments As Customer Request Notes

Rejected. Customer Request notes are internal collaboration artifacts. GitHub
comments are external delivery discussion. Merging those models would risk
privacy leaks and make public portal behavior ambiguous.

### Webhook-Only Sync

Rejected. Webhooks can be disabled, delayed, redelivered, or missed during
provider outages. Polling/backfill is required for reconciliation.

### Polling-Only Sync

Rejected. Polling alone makes comment and close-event feedback slow, and the
framework already has a signed event ledger suitable for webhook-triggered
runs.

### Keep Legacy Outbound Separate

Accepted for feedback-only deliveries. The old outbound behavior must remain
compatible, and the current outbound framework cannot observe the GitHub issue
number returned by a successful POST. Customer Request issue creates should use
the managed external sync path instead of trying to infer link state from the
legacy feedback envelope.

### Add Arbitrary Operator GitHub Comments

Rejected for #228. Operator-authored outbound comments need a local source
model, explicit permissions, audit text, and deletion/edit semantics. The MVP
only writes the Attune-managed backlink/request-context comment and captures
inbound GitHub comments.

## Risks / Tradeoffs

- Comment sync can create noise if every GitHub discussion entry appears in
  request detail. The delivery timeline should group and filter comments
  without hiding sync failures.
- GitHub issue bodies may already contain Attune markers from manual tests or
  older runs. The adapter needs deterministic marker parsing and duplicate
  marker conflict handling.
- GitHub API rate limits can be hit if every issue pull fetches all comments.
  The provider should fetch comments only for relevant or changed issues and
  rely on webhook events plus comment count/version changes.
- Legacy outbound target configuration may not match external connection
  configuration exactly. The MVP avoids implicit migration and keeps legacy
  direct POST behavior unchanged.
- Status mapping can surprise teams if Attune reopens GitHub issues
  automatically. Reopen should be disabled unless mapping config enables it.
- GitHub comments edited after creation need update handling. Provider comment
  id plus updated timestamp should drive ledger updates.
- Deleted GitHub comments need tombstone representation if webhook payloads or
  polling reveal deletion. The ledger should support `deleted_at` even if
  initial UI only shows active comments.
- Storing comment bodies improves operator context but adds privacy surface.
  Body caps, redaction, and clear retention ownership are required.

## Implementation Plan

1. Add this proposal and keep it linked from the implementing PR.
2. Add migration `112_github_bidirectional_issue_sync.sql` with
   `external_sync_runs.input_metadata`, `external_object_comments`, indexes,
   constraints, provenance columns, and new audit actions.
3. Extend `internal/externalsync` with pull input metadata and child-record
   output support.
4. Add GitHub issue locator parsing and canonicalization helpers shared by the
   Customer Request issue-link service and GitHub provider.
5. Bridge manual GitHub issue links into `external_object_links` only when an
   existing enabled managed connection and mapping match the repository.
6. Add the managed single-request create action and make `PreparePushRecords`
   honor run input metadata.
7. Extend the GitHub adapter schema, issue normalization, marker parsing, body
   section handling, managed-label read-modify-write behavior, linked-existing
   write policy, and existing-issue resolution.
8. Add GitHub comment fetch, child-record normalization, inbound ledger apply,
   managed backlink comment marker dedupe, and provider write/update behavior.
9. Extend webhook normalization for `issues`, `issue_comment`, and `ping`, then
   enqueue or reuse webhook-triggered pull runs with issue/comment input
   metadata after verified event receipt.
10. Preserve legacy `github-issue` direct POST behavior and keep managed
    Customer Request issue creates on the external sync path.
11. Extend repository apply paths to update Customer Request issue-link delivery
    context and comment ledger rows idempotently.
12. Add Console/API diagnostics only where the generic external sync views
    cannot already show the data.
13. Update `docs/external-sync-adapters.md` with GitHub comment, webhook,
    link-existing, Enterprise host matching, and legacy outbound compatibility
    guidance.
14. Update `CHANGELOG.md`, mark this proposal `Implemented`, and include
    verification output in the PR.

## Verification

Required focused checks:

```sh
go test ./internal/externalsync/adapter/githubissue
go test ./internal/service/externalsync
go test ./internal/repo/externalsync
go test ./internal/handlers/externalsyncwebhook
go test ./internal/service/customerrequest
go test ./internal/repo/customerrequest
go test ./cmd/attune
```

Required repository checks before merge:

```sh
go vet ./...
go build ./...
go test -race ./...
go mod tidy && git diff --exit-code go.mod go.sum
scripts/lint-slog.sh --strict
scripts/lint-rawptr.sh
scripts/lint-errorcode.sh
scripts/lint-integration-layout.sh
scripts/lint-artifacts.sh --strict
```

When Console or generated contracts change, also run the relevant frontend and
proto checks:

```sh
make proto
pnpm --dir console tsc -b --noEmit
pnpm --dir console biome check
pnpm --dir console exec vite build
pnpm --dir console vitest run --coverage
```

Required full-stack browser acceptance:

- build the production image from the source state under review;
- run the image with real pgvector PostgreSQL and a provider mock or disposable
  provider sandbox;
- open the deployed Console in a visible browser and drive the acceptance path
  with mouse clicks, mouse scrolling, and ordinary form entry;
- create or select a Customer Request, configure a GitHub connection, save a
  push-capable Customer Request to Issue mapping, test the connection, create a
  GitHub Issue from the request detail page, and inspect the external sync run;
- collect screenshots, provider mock or sandbox request logs, Postgres rows,
  and service logs for the same time window;
- fail the acceptance if the UI state, provider calls, run state, durable links,
  or logs disagree.

The reusable project checklist lives in `docs/testing.md` under
"Full-stack browser acceptance".

Implementation verification completed on 2026-07-20:

```sh
make proto
go test ./internal/repo/externalsync ./internal/service/customerrequest ./internal/handlers/console/customerrequest ./internal/infra/database
pnpm --dir console vitest run src/lib/customer-request-api.test.tsx src/features/customer-requests/components/customer-requests-page.test.tsx
go test -tags integration ./test/integration/postgres/externalsync -run 'TestRepoCreateCustomerRequestIssueRun|TestRepoPrepareAndApplyPushResult'
go test ./...
pnpm --dir console tsc -b --noEmit
pnpm --dir console biome check
go test ./internal/service/externalsync ./internal/handlers/console/externalsync ./internal/repo/externalsync ./internal/externalsync/adapter/githubissue ./internal/proto/attune/v1
go test -race ./internal/service/externalsync ./internal/repo/externalsync ./internal/repo/customerrequest ./internal/externalsync/adapter/githubissue ./internal/handlers/console/externalsync ./internal/handlers/externalsyncwebhook
go test -tags integration ./test/integration/postgres/customerrequest -run TestPGCustomerRequestLinkGitHubIssueByConnectionAndNumber -count=1
go test -tags=integration -count=1 -p 1 ./test/integration/postgres/externalsync
go test -tags=integration -count=1 -p 1 ./test/integration/postgres/database
scripts/lint-artifacts.sh --strict
lizard . -l go -C 15 -T nloc=100 --warnings_only
git diff --check
PATH=/Users/phj/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin:/Users/phj/.cache/codex-runtimes/codex-primary-runtime/dependencies/bin/fallback:$PATH make ci-check
make test-integration
```

Observed result: `make ci-check` passed with Go vet/build/test, module tidy,
search-quality baseline, golangci-lint, lizard, slog/artifact/raw pointer/error
code/integration/maturity/outbound checks, jscpd at 4.54%, Console biome,
Console Vite build, Console TypeScript, 1,554 Vitest tests, and dependency
cruiser. The focused package run covered service, handler, repository, GitHub
provider, generated protobuf code, and the new Customer Request GitHub create
entrypoint, plus managed link-existing by GitHub connection and issue number and
by GitHub URL. The managed link-existing integrations also assert that a
successful link queues a targeted manual pull run with issue-number input
metadata, that push-only mappings stay as passive manual links, that one
managed GitHub issue cannot be rebound to a second Customer Request, and that
the same Customer Request cannot be rebound to a second GitHub issue through the
same managed mapping. They also assert that ambiguous same-repository URL
matches are rejected instead of silently selecting one connection. The repo unit
coverage checks that GitHub Enterprise API/browser hosts match only the intended
repository host, and service unit coverage rejects mixed URL plus issue-number
locators before any repository mutation. The affected
service, repository, handler, provider, and webhook packages also passed
`go test -race`. Postgres integration initially exposed a migration-file format
mistake in the new migration; after converting it to the repository's
forward-only SQL format, both the focused externalsync/database/customerrequest
integration scopes and the full `make test-integration` suite passed. The
default shell Node v23 rejected dependency-cruiser, so the final `make ci-check`
run used the bundled Codex Node runtime; TruffleHog was skipped because it is
not installed in this local environment.

Full-stack browser acceptance completed on 2026-07-20 against a production
Docker image and real pgvector PostgreSQL:

- image `attune:codex-full-deploy` booted with Console served from the image and
  `/healthz` returning `ok`;
- the browser was opened against `http://127.0.0.1:55011/console/`;
- the operator logged into Console, inspected the Control Tower, and verified a
  real API-ingested feedback row in the deployed feedback list;
- a Customer Request named `Browser deployed GitHub sync request` was created
  through Console;
- a GitHub connection named `Mock GitHub HTTPS` was created through Console
  with base URL `https://github-mock:8443`, tested successfully, and persisted
  with `last_test_status = ok`;
- the Customer Request to Issue mapping was changed through Console to
  `bidirectional` and saved as mapping version 2;
- the request detail page queued GitHub issue creation through
  `POST /fb/v1/console/customer-requests/{id}/issue-links:create-github`;
- the external sync worker completed run
  `04865bc0-a1af-4c02-8640-b1be5cbd60df` with status `succeeded`, direction
  `push`, one record seen, one record changed, zero failures, and zero
  conflicts;
- the HTTPS GitHub mock recorded one `POST /repos/acme/app/issues`, one
  `GET /repos/acme/app/issues/700/comments`, and one
  `POST /repos/acme/app/issues/700/comments`, all with authorization present;
- `customer_request_issue_links` and `external_object_links` stored the durable
  link `https://github.com/acme/app/issues/700` with `sync_state = synced`;
- mouse-driven verification reopened the deployed Console, clicked the request
  card, scrolled to the delivery link, navigated through the sidebar to
  External Sync, selected the mock connection, opened the succeeded run detail,
  and clicked Test Connection; the UI showed the synced issue link, the
  succeeded run details, and a successful connection-test toast;
- the repeated mouse-triggered connection test added a fresh provider
  `GET /repos/acme/app` request and updated `last_tested_at`;
- service logs for the validation window showed the Console
  `CreateGitHubIssue` request returning 200 and the worker processing the run
  successfully. Unrelated local-demo enrichment warnings were present because no
  LLM provider was configured.

Test coverage should include:

- GitHub URL and `owner/repo#number` parser cases;
- GitHub Enterprise browser/API host matching cases;
- manual link with and without matching managed connection;
- manual link refusing to create or enable mappings implicitly;
- single-request create selecting only the requested Customer Request;
- duplicate local link and duplicate external issue conflict behavior;
- issue create, update, close, reopen-disabled, and managed-label handling;
- managed-label read-modify-write preserving unmanaged labels;
- linked-existing default write policy preserving title/body/labels;
- body managed-section replacement that preserves surrounding text;
- Pull Request entries skipped from issue polling;
- child-record comment contract translation;
- comment pull insert/update/delete or tombstone handling;
- outbound comment retry dedupe by Attune marker;
- inbound webhook redelivery dedupe by `X-GitHub-Delivery`;
- `issue_comment` webhook event creating or reusing a queued pull run with
  issue/comment input metadata;
- event replay attaching `external_sync_events.run_id`;
- managed issue unlink marking the external object link as a local tombstone,
  pull and scheduled push runs skipping that tombstone, and explicit manual
  relink or create actions clearing or replacing it;
- legacy outbound direct POST behavior unchanged.

## References

- [Issue #228: upgrade GitHub Issues to bidirectional sync](https://github.com/Phixsura/attune/issues/228)
- [Issue #202: Product Signal OS umbrella](https://github.com/Phixsura/attune/issues/202)
- [External Sync Framework proposal](./2026-07-08-external-sync-framework.md)
- [Issue #226: Jira bidirectional issue sync](https://github.com/Phixsura/attune/issues/226)
- [PR #251: Jira bidirectional issue sync implementation](https://github.com/Phixsura/attune/pull/251)
- [GitHub REST API: Issues](https://docs.github.com/en/rest/issues/issues?apiVersion=2022-11-28)
- [GitHub REST API: Issue comments](https://docs.github.com/en/rest/issues/comments?apiVersion=2022-11-28)
- [GitHub webhook events and payloads](https://docs.github.com/en/webhooks/webhook-events-and-payloads)
- [GitHub webhook best practices](https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks)
- [Linear GitHub integration](https://linear.app/integrations/github)
- [Sentry GitHub integration](https://getsentry-sentry.mintlify.app/integrations/github)
- [Canny GitHub integration](https://help.canny.io/en/articles/3481076-github-integration)
- [Productboard GitHub Issues integration](https://support.productboard.com/hc/en-us/articles/360056319254-Integrate-with-GitHub-Issues)
- [Aha! Roadmaps GitHub integration](https://support.aha.io/aha-roadmaps/integrations/github/github-integration-version-2~7444659558515549344)
