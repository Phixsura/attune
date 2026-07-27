# External Sync Adapters

External sync adapters connect Attune object mappings to provider systems such
as GitHub, Jira, Linear, or tenant-owned issue trackers. The framework owns
connection storage, credential encryption, cursor persistence, run history,
record failures, conflicts, retries, audit events, metrics, and the Console
operations page. Adapters only normalize provider behavior into the shared
contract in `internal/externalsync`.

## Adapter Contract

An adapter implements `externalsync.Provider`:

- `Provider()` returns the stable provider token stored in
  `external_connections.provider`.
- `Check(ctx, conn)` validates decrypted credentials and non-secret provider
  configuration.
- `Discover(ctx, conn)` returns supported provider object schemas.
- `Pull(ctx, req)` returns normalized external records, optional child records,
  and the next cursor.
- `Push(ctx, req)` writes normalized local records to the provider.
- `ClassifyError(err)` returns a redacted error kind, message, HTTP status,
  provider request id, optional `Retry-After`, and retryability decision.

`Push` returns one `WriteResult` for each local record. Successful write results
should include the provider key, URL, and version when the provider exposes
them. Record-level failures should be returned inside `WriteResult.Error`; the
framework records those as replayable push failures without failing the whole
run. Reserve a top-level `Push` error for connection-wide failures such as
invalid credentials, unavailable provider APIs, or malformed connection config.

Register adapters from their package `init()`:

```go
func init() {
	externalsync.Register("github", "GitHub", func() externalsync.Provider {
		return NewProvider()
	})
}
```

The provider token is a persisted identifier. Once shipped, do not rename or
repurpose it. If a provider needs aliases, add an explicit compatibility layer
rather than changing stored rows.

## Connection Inputs

`externalsync.Connection` is already decrypted before it reaches the adapter.
Adapters must treat `Credential` as sensitive:

- never log raw credential bytes;
- never copy credentials into provider config or record payloads;
- use `ClassifyError` to redact provider URLs, tokens, and request bodies before
  returning messages to the framework.

Use `ProviderConfig` for non-secret JSON such as repository, cloud/site, team,
workspace, or project selectors. Validate required keys in `Check`, and keep
validation errors concise and redacted.

`Discover` output is exposed through the Console schema endpoint and rendered
beside object mappings. Keep schema data provider-neutral: object type names and
field names are fine, but do not include credentials, tenant-specific sample
payloads, or raw provider responses. Console uses discovered field names to warn
operators when mapping JSON references fields that are not advertised by the
provider, so keep the names stable across adapter releases. Populate
`RequiredFields` and `WritableFields` when the provider exposes that distinction;
Console mapping preview uses those lists to surface missing required outputs and
write-path mismatches before a mapping is saved or a backfill is requested.

## Qualification And Recovery

Console can qualify a connection through the generated
`QualifyExternalConnection` API. The framework decrypts the connection
credential, runs the adapter `Check`, runs `Discover`, inspects schema metadata,
and reports whether configured scopes are visible. Adapters should make `Check`
small, deterministic, and safe to run repeatedly. Return a provider request id
and latency when available, and keep failure messages redacted because the
qualification report is stored as an audit event.

Qualification is a runtime readiness report, not a replacement for adapter
tests. A production adapter should still have contract coverage for token
validation, cursor translation, schema discovery, tombstones, rate limits,
redaction, idempotent writes, and record-level failure behavior.

When repeated terminal runs quarantine a connection, Console can resume it
through `ResumeExternalConnection`. Resume enables the connection, restores
status `active`, clears the redacted last error, and records an audit event.
Adapters should not keep hidden provider-local circuit state that would survive
this operation. If a provider needs a half-open probe, expose it through
`Check`/qualification so operators can verify readiness before or after resume.

## Built-in GitHub Issues Adapter

The built-in GitHub adapter registers provider token `github` and supports
external object type `issue`.

Connection setup:

```json
{
  "provider": "github",
  "auth_type": "token",
  "webhook_secret": "replace-with-at-least-16-characters",
  "provider_config": {
    "owner": "acme",
    "repo": "app"
  },
  "scopes": ["issues"]
}
```

`provider_config` also accepts `repo_url` instead of `owner` and `repo`:

```json
{
  "repo_url": "https://github.com/acme/app"
}
```

For GitHub Enterprise, set either connection `base_url` or
`provider_config.api_base_url` to the REST API base URL, such as
`https://github.example.com/api/v3`. The adapter validates that this URL passes
the same SSRF egress policy used by webhooks and LLM provider calls. The
credential is a bearer token; for pull-only mappings it needs issue read access,
and for push mappings it needs issue write access.

The Console connection editor updates name, enabled state, base URL, provider
configuration, and scopes. It rotates the credential or webhook secret only
when a new value is submitted.

The Console provider picker lists registered adapters from the backend
registry, so the create-connection dialog can surface GitHub and Jira without
hard-coded provider tokens. Test-only `noop` wiring is omitted from that list.

GitHub Issue sync changes require the full-stack browser acceptance checklist in
[`docs/testing.md`](testing.md). The checklist is intentionally mouse-driven:
the deployed Console must be operated in a visible browser, while provider
mock or sandbox logs, Postgres rows, and service logs prove that the UI state
matches durable sync behavior.

GitHub webhook setup:

- set the connection `webhook_secret` when creating or updating the connection;
- configure GitHub to deliver JSON webhooks to
  `/v1/external-sync/webhooks/github/{tenant_id}/{connection_id}`;
- enable the `issues` and `issue_comment` events for issue sync, plus `ping`
  for setup validation;
- Attune verifies `X-Hub-Signature-256`, records `X-GitHub-Delivery` as the
  external event id, stores only a compact normalized event payload, and queues
  a webhook-triggered pull run for verified `issues` and `issue_comment`
  deliveries that match an enabled Customer Request issue mapping. Verified
  `ping` deliveries are recorded for setup diagnostics and do not enqueue a run.

Pull behavior:

- calls `GET /repos/{owner}/{repo}/issues` with `state=all`, `sort=updated`,
  and `direction=asc`;
- skips entries that are GitHub pull requests;
- stores issue number, title, state, labels, assignees, URLs, and timestamps in
  a normalized payload;
- fetches GitHub issue comments for changed issues and returns them as
  `comment` child records keyed by provider comment id;
- advances a JSON cursor with either `next_url` for paginated responses or
  `updated_since` for the high watermark;
- extracts `<!-- attune:customer_request_id=<uuid> -->` from issue bodies and
  uses it as `ExternalRecord.LocalObjectID`;
- webhook replay metadata can include an issue number and comment id; in that
  case the adapter reads only that issue and its comments, and returns the same
  cursor it received so scheduled pulls keep their high watermark;
- `issue_comment.deleted` webhook replay emits a deleted `comment` child record
  from the webhook `comment_id` when the comment no longer appears in GitHub's
  comment list.

Push behavior accepts `LocalRecord.Payload` JSON. Creating an issue uses:

```json
{
  "title": "Request title",
  "body": "Issue body",
  "labels": ["attune/request"],
  "customer_request_id": "00000000-0000-0000-0000-000000000000"
}
```

Updating an issue uses the issue number as `external_key`. The adapter also
accepts same-repository keys in `owner/repo#number` form and normalizes them to
the issue number before writing; keys for a different repository are rejected.

```json
{
  "external_key": "42",
  "state": "closed",
  "title": "Updated title",
  "body": "Updated body",
  "labels": ["attune/request"]
}
```

For Customer Request mappings, the framework prepares this payload from active,
unmerged, unarchived requests. New requests without an object link become create
payloads; synced requests whose local `updated_at` is newer than the last pushed
version become update payloads with the stored `external_key`. The GitHub
adapter maps local request status `shipped` and `cancelled` to closed GitHub
issues. It does not reopen issues unless `allow_reopen` is enabled.

Create payloads append the Customer Request marker when `customer_request_id` is
present, or when the local record id itself is a UUID. Update payloads are safe
by default: with `linked_existing_write_policy:
read_state_with_backlink`, Attune does not overwrite GitHub issue title, body,
or labels. Setting `linked_existing_write_policy: write_managed_fields` lets the
adapter write the managed request section and perform read-modify-write label
updates that preserve unmanaged labels while replacing labels under
`managed_label_prefix`.

When `sync_comments` is enabled, push also maintains one Attune-managed
request-context issue comment. The comment body includes stable hidden
`attune:comment_id` and `attune:customer_request_id` markers. Before creating a
comment, the adapter lists issue comments and updates the marked comment when it
already exists, so retries and scheduled pushes do not create duplicates.

## Built-in Jira Issues Adapter

The built-in Jira adapter registers provider token `jira` and supports external
object type `issue`.

Connection setup:

```json
{
  "provider": "jira",
  "auth_type": "token",
  "webhook_secret": "replace-with-at-least-16-characters",
  "provider_config": {
    "site_url": "https://acme.atlassian.net",
    "project_key": "ACME",
    "issue_type": "Task",
    "email": "bot@acme.com",
    "request_label_prefix": "attune-customer-request-",
    "status_transitions": {
      "pending": "To Do",
      "synced": "In Progress",
      "failed": "Blocked"
    }
  },
  "scopes": ["issues"]
}
```

`provider_config` also accepts `api_base_url` or a connection `base_url` for
the REST API base URL, and `issue_type_id` instead of `issue_type`. The
credential is a Jira API token, and Attune sends it with the configured email
as Basic auth `email:token`.

Jira webhook setup:

- set `webhook_secret` on the connection;
- configure Jira to POST JSON deliveries to
  `/v1/external-sync/webhooks/jira/{tenant_id}/{connection_id}`;
- send `X-Hub-Signature` with the same `sha256=...` HMAC shape used by GitHub;
- Attune records the delivery event with a compact normalized payload and
  dedupe key.

Pull behavior:

- queries `/rest/api/3/search` for the configured project ordered by updated
  time;
- normalizes issue key, summary, status, labels, assignee, reporter,
  timestamps, resolution, and comment metadata;
- extracts Attune request markers from labels or comments and bridges them
  back to `LocalObjectID`;
- honors the configured `request_label_prefix` when matching request labels,
  so tenants with custom marker prefixes remain idempotent on pull.

Push behavior:

- creates or updates issues with the configured project and issue type;
- preserves the Attune request marker comment when needed;
- transitions status using explicit `status_transitions` when configured, or a
  heuristic fallback when the workflow is obvious.

## Pull Records

`Pull` returns `externalsync.PullResult`:

- `Records` contains provider-neutral `ExternalRecord` values.
- `Children` contains provider-neutral child records such as issue comments.
- `NextCursor` is a JSON object encoded as bytes.
- `StreamKey` is optional; empty means the framework uses `default`.

Each `ExternalRecord` should include:

- `Key`: stable provider object key, required.
- `URL`: operator-facing URL when available.
- `Version`: provider version, ETag, updated timestamp token, or equivalent.
- `LocalObjectID`: Attune object id when the provider can identify it.
- `UpdatedAt`: provider-side update time when available.
- `Deleted`: true when the provider reports an external tombstone.
- `Payload`: redacted normalized JSON object, not raw provider response bytes.

For Customer Request issue sync, `LocalObjectID` must be the customer request
UUID when the external issue is already bound to a request. If it is unknown,
leave it empty; the framework stores a generic external link keyed by
`external:<Key>` and skips the Customer Request issue-link bridge until a local
object is known.

Issue comment child records use `Type: "comment"`, `ParentKey` set to the issue
number, and `Key` set to the provider comment id. The framework stores them in
`external_object_comments` with a unique provider-comment ledger per external
object link, so repeated pulls and webhook deliveries update the same row
instead of duplicating comments.

Customer Request manual GitHub links can join the managed sync identity model
when the tenant has an active GitHub connection and an enabled
Customer Request-to-issue mapping for the same repository. The manual link may
keep its operator-facing key such as `acme/app#42`, while the generic
`external_object_links` row stores provider key `42`. Later pulls, pushes, and
tombstones update the existing issue link through `external_object_link_id`
instead of creating a duplicate `customer_request_issue_links` row. If the same
provider issue is already bound to a different request, the manual link is
rejected as a conflict.

GitHub pull apply projects delivery context into Customer Request issue links:
the issue title and state update `title` and `status`, state is normalized to
`external_status_category` as `open`, `closed`, or `unknown`, and the primary
assignee or assignee list is stored in `external_assignee`.

Record timelines include `comment` entries from `external_object_comments`.
Issue link timeline detail JSON includes external URL, external version, sync
state, tombstone reason, and a provider payload snapshot for normalized labels,
assignees, state reason, close timestamp, and comment count. Comment timeline
detail JSON includes provider comment id, author display, external URL, external
version, external updated timestamp, body digest, truncation flag, and marker.
It does not expose raw comment bodies.

Manual run requests can include provider-neutral input metadata. For push runs,
`local_object_id` limits `PreparePushRecords` to one local object, which enables
single-request GitHub issue creation through the same worker path as scheduled
pushes. For pull runs, `external_key` identifies one provider object when the
adapter can use that key as a fetch hint. GitHub accepts either issue number
`42` or display key `acme/app#42` and fetches that issue without advancing the
scheduled cursor. Run responses expose `input_metadata_json` so operators can
see which selector was used.

Customer Request detail exposes a managed GitHub create action at
`POST /fb/v1/console/customer-requests/{id}/issue-links:create-github`. The
action queues a manual push run with `input_metadata.local_object_id` set to the
request id; the worker then creates the issue through the same provider
contract used by scheduled pushes. The request must not already have a GitHub
issue link, and the tenant must have exactly one active, enabled GitHub mapping
whose direction is `push` or `bidirectional` unless the caller supplies an
explicit `connection_id` or `mapping_id`. Repeated clicks reuse an existing
queued or running create run for the same request and mapping.

Customer Request detail can also link an existing managed GitHub issue either
by full GitHub URL or by connection-scoped issue number. `POST
/fb/v1/console/customer-requests/{id}/issue-links` accepts `connection_id`, an
optional `mapping_id`, and `issue_number` for provider `github`; it also accepts
a plain `external_url` when the URL matches a tenant GitHub connection and a
pull-capable Customer Request issue mapping. Attune resolves or derives the
managed mapping, stores the Customer Request issue link, and binds the same
issue number into `external_object_links` for subsequent pulls, pushes,
tombstones, and delivery timeline entries. A successful managed link queues a
targeted manual pull run with `input_metadata.external_key` set to the issue
number, so Attune refreshes GitHub-owned title, state, assignee, label, and
comment context without waiting for the next scheduled poll. Supplying both a
full `external_url` and `issue_number` is rejected so each link request has one
clear source of truth. If a URL matches more than one active pull-capable
mapping for the same repository, Attune rejects the link instead of choosing a
connection implicitly. If the request is already bound to a different GitHub
issue through the same managed mapping, Attune rejects the new link instead of
downgrading it to a passive manual reference.

Unlinking a managed GitHub issue from a Customer Request marks the generic
external object link as locally tombstoned with reason `local_unlinked` before
removing the operator-facing issue link. Later pull runs skip that local
tombstone and do not recreate the issue link automatically. Scheduled and
ordinary push runs also skip locally tombstoned requests so a background run
does not create a replacement issue just because the active link is gone. An
explicit manual link request for the same GitHub issue can clear the local
tombstone and rebind the external object link to the selected Customer Request;
the explicit managed create action can create a new GitHub issue after unlink.

## Cursor Rules

The framework reads `external_sync_cursors` before `Pull` and writes the
successful `NextCursor` with the run stats. Adapters should follow these rules:

- cursors must be JSON objects;
- a pull with no progress may return the same cursor it received;
- a paginated provider should return the cursor for the next page or next high
  watermark;
- do not advance a provider cursor inside provider-owned durable state; Attune
  is the cursor authority for framework runs.

Operators can reset a pull-capable mapping cursor from Console. Reset clears
stored cursors and high watermarks for the mapping, records the reset requester,
and enqueues a manual pull run. Adapters must therefore tolerate receiving an
empty cursor after previously advancing: the next pull should replay from the
provider's documented beginning or default lookback without relying on hidden
adapter-local checkpoints.

Operators can also enqueue an explicit backfill for a pull-capable mapping. A
backfill uses the same adapter `Pull` contract and durable cursor rules as a
manual run, but its run trigger is `backfill` and the Console can request that
stored cursors are cleared before the run is queued. Do not implement a separate
adapter-local backfill store; use the framework cursor and run ledger so
backfill behavior remains auditable and retryable.

Record-level failures can still produce a `partial` run. This is safe only when
the failed record can be retried independently by stable external key or by a
sanitized replay payload captured in `external_sync_record_failures`.

## Event Ledger And Replay

Provider webhook receivers should verify signatures before recording deliveries
in `external_sync_events`. Store the provider delivery id, event type, dedupe
key, signature status, raw payload digest, and redacted normalized payload
through `service/externalsync.RecordEvent`. Do not store raw provider payloads
or signatures.

Console event detail renders the delivery status, signature status, dedupe key,
external event id, replay linkage, failure reason, payload digest, and
normalized payload. Keep normalized payloads compact and provider-neutral so
operators can diagnose delivery routing without seeing raw webhook bodies.

The built-in GitHub receiver follows this path at
`/v1/external-sync/webhooks/github/{tenant_id}/{connection_id}` and returns
`202 Accepted` only after `X-Hub-Signature-256` verifies against the connection's
encrypted webhook secret.

Operators can replay a verified delivery from Console. Replay creates a
`webhook`-triggered pull run for the event's mapping in the same transaction
that marks the event replayed. A provider-specific receiver should still keep
poll-based reconciliation available so missed or delayed deliveries are healed
by the regular cursor path.

## Failures And Conflicts

Adapters classify provider-call failures with stable, low-cardinality kinds:

- `auth_failed`
- `rate_limited`
- `not_found`
- `validation`
- `provider_unavailable`
- `provider_error`

The framework handles record-level validation failures after `Pull`. A missing
external key becomes a non-retryable record failure. A pending local link that
receives a different external version becomes an open conflict instead of
silently overwriting state.

Console exposes local-wins, external-wins, manual-merge, and ignored conflict
resolutions. Adapters should keep local and external snapshots compact and
provider-neutral so each resolution remains understandable without opening raw
provider payloads.

Console can also resolve multiple open conflicts from the same run in one
operation. Batch resolution uses the same resolution enum as single-conflict
resolution and caps request size so operators can clear repeated, equivalent
conflicts without bypassing the audit ledger.

Adapters should not create rows in `external_sync_record_failures` or
`external_sync_conflicts` directly. Return normalized records and classified
provider errors; the framework owns the durable ledger.

Console run detail renders attempt HTTP status, provider request ids,
`Retry-After`, classified error kinds, payload digests, normalized replay
payloads, and conflict snapshots. Treat those values as operator diagnostics:
they should be stable enough to support provider support tickets and retries,
but must remain redacted and compact.

Console health also rolls recent failed and dead runs into throttled,
unauthorized, provider-unavailable, delayed-retry, and degraded-connection
counts. When the latest three terminal runs for an enabled active connection are
all failed or dead, the worker asks the repository to quarantine that connection.
Quarantine disables the connection, sets status `quarantined`, records a
redacted last error, and prevents queued runs for that connection from being
claimed. Keep `ClassifyError` error kinds stable so these summaries and
quarantine reasons stay actionable across providers.

For retryable provider failures, a future `Retry-After` delays the next worker
claim when it is later than the framework's default backoff. The framework caps
that delay at 24 hours so a malformed provider header cannot quarantine a run
indefinitely.

Console record timeline combines object-link, record-failure, conflict, and run
ledger rows for a local object id or external key. Adapter output should keep
`ExternalRecord.Key`, payload digests, versions, and failure classifications
stable enough for operators to reconstruct what happened to one record across
pulls, pushes, retries, and conflict resolution.

## Observability

Adapters must keep metric labels bounded. Do not add tenant ids, connection ids,
request ids, external keys, URLs, or customer identifiers to Prometheus labels.
The framework emits run, duration, conflict, lag, and dead-run metrics by
provider and object type. Tenant-specific diagnostics belong in Console APIs and
database rows.

Record counters are emitted from completed run stats with operation labels for
pull and push work. Conflict counters use `open` when a run creates a conflict
and the selected resolution when an operator resolves one. Lag and dead-run
gauges are refreshed from the repository snapshot after worker state changes so
the dashboard reflects current durable run state instead of worker-local memory.

## Verification Checklist

For a new adapter:

- add registry tests for provider token and display label;
- add unit tests for `Check`, cursor translation, tombstone normalization, and
  `ClassifyError` redaction;
- add service or repository tests for idempotent links, stale cursor handling,
  record failures, and conflicts;
- add integration tests only under `test/integration/**` when a real service or
  PostgreSQL behavior is required;
- run `go test ./internal/externalsync ./internal/service/externalsync`;
- run `go test -tags=integration ./test/integration/postgres/externalsync` when
  sync ledger behavior changes;
- run `scripts/lint-slog.sh --strict` and `scripts/lint-artifacts.sh --strict`
  before claiming the adapter is ready.
