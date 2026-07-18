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
- `Pull(ctx, req)` returns normalized external records and the next cursor.
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

GitHub webhook setup:

- set the connection `webhook_secret` when creating or updating the connection;
- configure GitHub to deliver JSON webhooks to
  `/v1/external-sync/webhooks/github/{tenant_id}/{connection_id}`;
- enable the `issues` event for issue sync diagnostics and `ping` for setup
  validation;
- Attune verifies `X-Hub-Signature-256`, records `X-GitHub-Delivery` as the
  external event id, and stores only a compact normalized event payload.

Pull behavior:

- calls `GET /repos/{owner}/{repo}/issues` with `state=all`, `sort=updated`,
  and `direction=asc`;
- skips entries that are GitHub pull requests;
- stores issue number, title, state, labels, assignees, URLs, and timestamps in
  a normalized payload;
- advances a JSON cursor with either `next_url` for paginated responses or
  `updated_since` for the high watermark;
- extracts `<!-- attune:customer_request_id=<uuid> -->` from issue bodies and
  uses it as `ExternalRecord.LocalObjectID`.

Push behavior accepts `LocalRecord.Payload` JSON. Creating an issue uses:

```json
{
  "title": "Request title",
  "body": "Issue body",
  "labels": ["attune/request"],
  "customer_request_id": "00000000-0000-0000-0000-000000000000"
}
```

Updating an issue uses the issue number as `external_key`:

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
issues and keeps other statuses open.

Create payloads append the Customer Request marker when `customer_request_id` is
present, or when the local record id itself is a UUID. Update payloads append
the marker only when they explicitly include `body`; state-only updates do not
overwrite the existing GitHub issue body. The marker makes later pulls
idempotently bridge the GitHub issue back to the Customer Request issue-link
ledger.

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
  back to `LocalObjectID`.

Push behavior:

- creates or updates issues with the configured project and issue type;
- preserves the Attune request marker comment when needed;
- transitions status using explicit `status_transitions` when configured, or a
  heuristic fallback when the workflow is obvious.

## Pull Records

`Pull` returns `externalsync.PullResult`:

- `Records` contains provider-neutral `ExternalRecord` values.
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
