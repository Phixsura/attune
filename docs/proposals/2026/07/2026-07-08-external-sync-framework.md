<!-- markdownlint-disable MD013 -->

# External Sync Framework

| Field | Value |
|---|---|
| Issue | [#214](https://github.com/Phixsura/attune/issues/214) |
| Status | Implemented |
| Started | 2026-07-08T12:11:39+08:00 |
| Related | [customer requests](./2026-07-07-customer-requests.md), [customer request delivery health rollup](./2026-07-07-customer-request-delivery-health-rollup.md), [customer request decision intelligence](./2026-07-07-customer-request-decision-intelligence.md), [channel-agnostic inbound](../06/2026-06-08-channel-agnostic-inbound.md), [notify dead queue](../06/2026-06-18-notify-dead-queue.md) |

## Problem

Attune can link Customer Requests to external issues and record a provider's
latest issue state, but that path is passive. The current
`RecordCustomerRequestIssueSync` endpoint updates `customer_request_issue_links`
after another actor has already fetched external state. It does not own
provider credentials, object mappings, cursor checkpoints, sync runs, retry
state, record-level failures, conflicts, tombstones, or operator health views.

Issue #214 asks for a reusable external sync framework that can connect Attune
objects to systems such as GitHub, Jira, Linear, and other product or support
tools. This needs to be a platform capability rather than another
Customer Request-specific table, because every provider adds the same hard
requirements: encrypted connection credentials, field and status mappings,
incremental cursor state, durable run history, bounded retries, auditability,
metrics, and Console diagnostics.

Top integration systems converge on this shape. Airbyte, Fivetran, Kafka
Connect, Debezium, Singer/Meltano, Nango, Merge, Hightouch, Unito, and Exalate
all treat connections, mappings, checkpoints, runs, and record-level failures as
first-class operational data. Attune should adopt that operating model while
keeping the first supported object small and aligned with Customer Request issue
links.

## Goals

- Add a generic external sync foundation with tenant-scoped connections, object
  mappings, cursors, sync runs, attempts, record failures, and conflicts.
- Store provider credentials encrypted through Attune's existing Tink secret
  store and key registry path.
- Store provider webhook secrets encrypted through the same Tink path and use
  them for signed provider delivery receivers.
- Support pull, push, and bidirectional sync orchestration through provider
  adapters.
- Reuse the existing background worker discipline: claim fencing, heartbeat,
  stale-claim recovery, bounded attempts, graceful drain, and supervised
  restart.
- Bridge the framework to Customer Request issue links so GitHub, Jira, Linear,
  and `other` issue states can update existing delivery-health rollups.
- Expose generated Console APIs for connection management, mapping management,
  manual sync runs, run detail, failure retry, conflict resolution, and health
  summaries.
- Add mapping preview, operator-triggered backfill, record timeline, and
  automatic connection quarantine paths so recovery and diagnostics are usable
  without direct database access.
- Add operator-triggered connection qualification, explicit quarantine recovery,
  and batch conflict resolution so providers can be assessed and recovered from
  the Console/API without direct database edits.
- Add low-cardinality Prometheus metrics and audit events for security-relevant
  mutations and operator-triggered retries.
- Document the provider plug-in model so later connectors share one contract.

## Non-goals

- Replace the existing Customer Request issue-link model.
- Build a full ETL engine for arbitrary tenant-defined tables.
- Add public roadmap publishing, public voting, or external portal features.
- Support destructive external deletes as a default behavior.
- Add OAuth authorization flows in the same change as the framework skeleton.
- Accept provider webhooks before signature verification, replay protection,
  event deduplication, and idempotent run creation are designed.
- Guarantee exactly-once external writes. The framework targets durable
  at-least-once execution with idempotency keys, provider version checks, and
  conflict records.

## Proposal

Add a new external sync domain with repository, service, handler, worker, and
adapter packages:

- `internal/repo/externalsync`
- `internal/service/externalsync`
- `internal/handlers/console/externalsync`
- `internal/externalsync`
- `internal/externalsync/adapter/<provider>`

The `internal/externalsync` package owns the provider registry, following the
same discipline as `internal/inbound`: adapter packages register provider names
at init time, `cmd/attune` is the only blank-import site, and the registry
validates persisted provider tokens. Service and repository packages do not
import provider adapter packages directly.

### Data model

Create the next migration, currently `105_external_sync_framework.sql`, with
these tables.

`external_connections`

- `id`, `tenant_id`, `provider`, `name`, `enabled`, `status`
- `auth_type`, `base_url`, `provider_config`, `scopes`
- `credential_key_id`, `credential_ciphertext`
- `webhook_secret_key_id`, `webhook_secret_ciphertext`,
  `webhook_secret_set_at`
- `last_tested_at`, `last_test_status`, `last_error`
- `created_by`, `updated_by`, `created_at`, `updated_at`, `deleted_at`

Connections are tenant-scoped and soft-deleted. Credentials are encrypted before
storage. The encryption associated data should bind tenant, connection id, and
provider so ciphertext cannot be moved across rows unnoticed. The service must
generate the connection UUID before encrypting credentials so the associated data
is stable at write time.

Webhook secrets are optional and write-only. They use a separate associated-data
scope from API credentials so a credential ciphertext cannot be replayed as a
webhook secret. API responses expose only `webhook_secret_configured`.

`provider_config` stores non-secret provider settings such as GitHub owner/repo
or installation context, Jira cloud/site/project context, and Linear workspace
or team context. Provider adapters validate this config before a connection can
be enabled. Operator-facing connection fields and validation errors must be
redacted and size-capped.

Core constraints:

- unique active connection name per `(tenant_id, provider)`;
- provider token shape check in SQL and provider registration validation in the
  external sync registry;
- foreign key from `tenant_id` to `tenants`;
- foreign key from `credential_key_id` to `secret_key_registry`;
- partial index on enabled active connections for health summaries.

`external_object_mappings`

- `id`, `tenant_id`, `connection_id`
- `local_object_type`, `external_object_type`
- `direction`
- `field_mapping`, `status_mapping`
- `conflict_policy`, `tombstone_policy`
- `enabled`, `mapping_version`, `created_at`, `updated_at`

The first local object type is `customer_request`; the first external object
type is `issue`. Direction values are `pull`, `push`, and `bidirectional`.
Conflict policies are `manual`, `local_wins`, `external_wins`, and
`latest_update_wins`. Tombstone policies are `ignore`, `mark_stale`, `unlink`,
and `archive_local`. Destructive external delete is excluded.

Core constraints:

- foreign key from `(tenant_id, connection_id)` to the tenant's connection;
- unique active mapping per `(tenant_id, connection_id, local_object_type,
  external_object_type)`;
- JSON object checks for field and status mappings;
- indexes by tenant, connection, enabled state, and object type.

`external_object_links`

- `id`, `tenant_id`, `mapping_id`
- `local_object_type`, `local_object_id`
- `external_object_type`, `external_key`, `external_url`
- `external_version`, `external_updated_at`, `local_updated_at`
- `sync_state`, `sync_error`, `last_synced_at`
- `external_deleted_at`, `local_deleted_at`, `tombstone_reason`
- `created_at`, `updated_at`

This table is the generic object identity bridge. Customer Request issue links
keep their existing columns, and a nullable `external_object_link_id` can attach
them to the generic link when a mapped provider owns the sync.

Core constraints:

- foreign key from `(tenant_id, mapping_id)` to the tenant's mapping;
- unique active link per `(tenant_id, mapping_id, local_object_type,
  local_object_id)`;
- unique active link per `(tenant_id, mapping_id, external_object_type,
  external_key)`;
- index by sync state and last sync time for health summaries.

`external_sync_cursors`

- `tenant_id`, `mapping_id`, `stream_key`
- `cursor`, `high_watermark`
- `last_successful_run_id`
- `reset_requested_at`, `reset_requested_by`
- `updated_at`

Cursors live per mapping and stream, not per connection, because a connection can
sync multiple object types with different checkpoint semantics.

Core constraints:

- primary key or unique index on `(tenant_id, mapping_id, stream_key)`;
- foreign key from `(tenant_id, mapping_id)` to the tenant's mapping;
- cursor JSON object check;
- index on `reset_requested_at` for operator-driven resets.

`external_sync_runs`

- `id`, `tenant_id`, `connection_id`, `mapping_id`
- `direction`, `trigger`, `status`
- `claimed_at`, `claimed_by`
- `attempts`, `next_retry_at`
- `started_at`, `finished_at`
- `cursor_before`, `cursor_after`
- `records_seen`, `records_changed`, `records_failed`, `conflicts_created`
- `error_kind`, `error_message`
- `actor_id`, `created_at`, `updated_at`

Run statuses are `queued`, `running`, `succeeded`, `partial`, `failed`,
`cancelled`, and `dead`. Triggers are `manual`, `schedule`, `retry`, `system`,
and `webhook`. Webhook-triggered runs are created only through signed event
replay so dedupe and event/run linkage stay durable.

Cursor advancement rules are explicit:

- a run that fails before all fetched records are durably handled does not
  advance the cursor;
- a `partial` run can advance the cursor only for records whose retry path is
  independent of the cursor, either because the provider supports refetch by
  stable external key or because Attune stores a sanitized normalized replay
  payload with the record failure;
- cursor writes, run status writes, object-link writes, and Customer Request
  bridge writes happen in one transaction when they represent one logical sync
  step.

Core constraints and indexes:

- foreign keys to tenant, connection, and mapping;
- status, direction, and trigger check constraints;
- claim index on `(status, next_retry_at, claimed_at)`;
- tenant/run listing indexes on `(tenant_id, created_at DESC, id DESC)`,
  `(tenant_id, connection_id, created_at DESC, id DESC)`,
  `(tenant_id, mapping_id, created_at DESC, id DESC)`, and
  `(tenant_id, status, created_at DESC, id DESC)` for filterable keyset
  listing by connection, mapping, status, and prior run id;
- `claimed_by` fencing on every terminal state update.

`external_sync_attempts`

- `id`, `run_id`, `attempt_number`
- `started_at`, `finished_at`, `result`
- `http_status`, `provider_request_id`, `retry_after`
- `error_kind`, `error_message`

Attempts make transient provider behavior inspectable without overwriting the
run summary.

Core constraints:

- foreign key to `external_sync_runs`;
- unique `(run_id, attempt_number)`;
- bounded, redacted `error_message`.

`external_sync_record_failures`

- `id`, `tenant_id`, `run_id`, `mapping_id`
- `operation`, `local_object_id`, `external_key`
- `failure_kind`, `message`, `payload_digest`
- `retry_mode`, `normalized_payload`
- `retryable`, `resolved_at`, `resolved_by`
- `created_at`

Record failures allow a run to finish as `partial` while preserving per-record
retry and diagnostics. `retry_mode` is `refetch` when retry re-reads by stable
external key, and `replay` when retry uses `normalized_payload`.
`normalized_payload` stores only provider-normalized, redacted, size-capped
fields required for deterministic retry. Raw provider payloads are not stored.
Retrying a record failure resolves that failure row and enqueues a retry run for
the same connection, mapping, and direction, so operator action creates durable
worker work rather than only hiding the failure.

Core constraints:

- foreign keys to run and mapping;
- retry-mode check constraint;
- index by `(tenant_id, retryable, resolved_at)`;
- bounded, redacted message and payload fields.

`external_sync_conflicts`

- `id`, `tenant_id`, `mapping_id`
- `local_object_id`, `external_key`
- `conflict_kind`, `status`
- `local_snapshot`, `external_snapshot`
- `resolution`, `resolved_at`, `resolved_by`
- `created_at`, `updated_at`

Conflicts are created when bidirectional sync sees incompatible concurrent
changes or provider version checks fail.

Core constraints:

- foreign key to mapping;
- status and resolution check constraints;
- index by `(tenant_id, status, created_at DESC)`;
- redacted, size-capped local and external snapshots.

`external_sync_events`

- `id`, `tenant_id`, `connection_id`, `mapping_id`
- `provider`, `event_type`, `external_event_id`, `dedupe_key`
- `signature_status`, `status`, `payload_digest`
- `normalized_payload`, `received_at`
- `replayed_at`, `replayed_by`, `run_id`, `failure_reason`
- `created_at`, `updated_at`

The event ledger records provider webhook deliveries after a provider-specific
receiver has verified or rejected the signature. It stores a stable dedupe key,
a digest of the raw delivery payload, and a redacted normalized payload object
for operator troubleshooting. Raw provider payloads are not stored. Replaying a
verified event creates a `webhook`-triggered pull run and links the event to the
queued run in one transaction.

The first concrete receiver is GitHub:

- `POST /v1/external-sync/webhooks/github/{tenant_id}/{connection_id}`;
- validates `X-Hub-Signature-256` with the connection's encrypted
  `webhook_secret`;
- records `X-GitHub-Event` and `X-GitHub-Delivery` in the event ledger;
- stores a compact issue, repository, and sender summary instead of the raw
  GitHub payload;
- returns `202 Accepted` for verified deliveries and records failed signatures
  before returning `401 Unauthorized`.

Core constraints:

- unique `(tenant_id, connection_id, dedupe_key)` for delivery deduplication;
- signature status check constraint for `verified`, `failed`, and
  `not_required`;
- event status check constraint for `received`, `replayed`, `ignored`, and
  `failed`;
- payload digest is a 64-character lowercase SHA-256 hex string;
- normalized payload is a JSON object;
- indexes by tenant, connection, status, run id, and creation time.

### Provider interface

Provider adapters implement a narrow contract:

```go
type Provider interface {
	Provider() string
	Check(ctx context.Context, conn Connection) (CheckResult, error)
	Discover(ctx context.Context, conn Connection) ([]ObjectSchema, error)
	Pull(ctx context.Context, req PullRequest) (PullResult, error)
	Push(ctx context.Context, req PushRequest) (PushResult, error)
	ClassifyError(error) SyncError
}
```

`Pull` receives the current cursor and returns normalized external records plus
the next cursor. `Push` receives normalized local changes and returns per-record
write results, including provider versions or retryable failures. Provider
adapters must use `otelhttp.NewTransport(http.DefaultTransport)` for outbound
HTTP and must not log credentials or raw access tokens.

Providers return redacted error classes, stable retry hints, provider request
ids, and optional retry-after values. Raw request or response bodies stay out of
service logs, audit metadata, and sync-run tables.

### Service behavior

`service/externalsync` owns orchestration:

- validate connection and mapping configuration;
- encrypt and decrypt credentials at the connection boundary;
- create manual sync runs;
- claim due runs and enforce worker ownership;
- call provider adapters;
- apply field/status mappings;
- update generic object links;
- bridge Customer Request issue updates through the existing service/repo path;
- write cursor state only after successful or accepted partial processing;
- classify retryable and terminal failures;
- create conflict records for bidirectional mismatches;
- record audit events inside the same transaction as operator mutations.

Customer Request integration should update existing issue-link sync fields after
a provider record has been normalized. Delivery-health rollups continue to read
from `customer_request_issue_links`, so existing list and detail behavior stays
compatible.

The worker must not call the existing `customerrequest.Service.RecordIssueSync`
method directly, because that method owns its own transaction and writes a
human-facing `customer_request.record_issue_sync` audit event. External sync
needs a tx-safe bridge, such as a Customer Request repository method or a narrow
service port that accepts the external sync transaction. One sync step should
commit these writes atomically:

- generic object-link update;
- Customer Request issue-link sync fields;
- record failure or conflict rows;
- cursor update, when allowed by the cursor advancement rules;
- run status and counters.

Automated background sync should audit the external sync run and operator
actions, not every individual Customer Request issue state write. Manual
operator actions on a Customer Request can continue to use the existing
Customer Request audit action.

### Worker behavior

Add `ExternalSyncWorker` with the same operational properties as the outbox
worker:

- unique `owner` id per process;
- `FOR UPDATE SKIP LOCKED` claim query;
- `claimed_by` fencing on mark-succeeded, mark-failed, and mark-dead;
- heartbeat while processing a batch;
- stale-claim recovery at startup;
- bounded attempts and exponential backoff;
- `workerdrain.Drainer` for shutdown;
- `safego(ctx, "external_sync", ...)` supervision in `cmd/attune`;
- metrics for stale-claim recovery, heartbeat, in-flight work, and drain status
  using the shared worker metric families.

The worker should expose `ProcessOnce` for deterministic tests.

### Console and API

Add generated proto endpoints for:

- list, get, create, update, delete, and test external connections;
- write-only webhook secret input and read-only webhook secret configured state;
- discover provider object schemas for a connection;
- list and update object mappings;
- request a manual sync run;
- list sync runs with connection, mapping, status, and keyset pagination
  filters;
- get sync run detail with attempts, failures, and conflicts;
- retry a run or record failure;
- resolve or ignore a conflict;
- list, inspect, and replay provider event deliveries;
- read external sync health summaries.

Console gets one Integrations page for connection and mapping configuration,
including credential and webhook secret rotation for existing connections and
provider schema visibility plus schema-aware JSON validation beside the mapping
editor. Run detail exposes provider request ids, HTTP status, retry hints,
payload digests, normalized payloads, and conflict snapshots for operator
diagnostics, and lets operators choose local-wins, external-wins, manual-merge,
or ignored conflict resolutions. Event detail exposes delivery status, signature
status, dedupe keys, replay linkage, failure reasons, and normalized payloads.
The Reliability page gets health cards for:

- enabled connections;
- failing or stale connections;
- newest successful run;
- active run count;
- dead or retryable run count;
- open conflict count.

Recent run detail should show run status, duration, trigger, direction, record
counts, cursor movement, attempts, record failures, conflicts, and retry actions.
The Console should refresh active `queued` and `running` runs without requiring
operators to reload the page. Operator action buttons should show busy state
only while their mutation is pending, then re-enable after success or failure so
one completed action does not block follow-up diagnostics.

RBAC should add `settings:external_sync:view` and
`settings:external_sync:edit`. Reads are available to admins and delegated
admins. Mutations that create credentials, edit mappings, retry runs, or resolve
conflicts require delegated-admin-or-higher sessions. Credential deletion and
connection revocation can be admin-only if review finds that delegated admins
should not disable external write paths.

### Audit and metrics

Add audit actions to both Go `validActions` and the database
`chk_audit_action_value` constraint:

- `external_connection.create`
- `external_connection.update`
- `external_connection.delete`
- `external_connection.qualify`
- `external_connection.resume`
- `external_connection.test`
- `external_sync_mapping.update`
- `external_sync_run.request`
- `external_sync_run.retry`
- `external_sync_failure.retry`
- `external_sync_conflict.resolve`
- `external_sync_event.replay`

Audit metadata must identify provider, connection id, mapping id, run id, and
object type. It must not include credentials, access tokens, raw provider
payloads, or unredacted customer URLs.

Add low-cardinality metrics:

- `attune_external_sync_runs_total{provider,object_type,result}`
- `attune_external_sync_records_total{provider,object_type,operation,result}`
- `attune_external_sync_run_duration_seconds{provider,object_type,result}`
- `attune_external_sync_lag_seconds{provider,object_type}`
- `attune_external_sync_conflicts_total{provider,object_type,resolution}`
- `attune_external_sync_dead_runs{provider,object_type}`

Do not label metrics by connection id, request id, external key, or customer
identifier. These metrics intentionally omit tenant labels; tenant-specific
external sync state is exposed through the Console health endpoint backed by the
sync tables. If deployment operators require tenant-labeled Prometheus alerts,
that decision should be made explicitly with a cardinality budget.

### Benchmark baseline

The implemented framework is a durable sync foundation. It should be evaluated
against mature integration products as an operating model, not only as a table
and API addition. The benchmark set includes provider webhook platforms
(GitHub, Stripe, Jira, Zendesk, Linear), data sync runtimes (Airbyte,
Fivetran), integration platforms (Nango, Merge), event streams (Salesforce
Change Data Capture), and enterprise workflow hubs (ServiceNow Integration
Hub). These systems converge on the following maturity traits:

- signed event ingress with a delivery ledger, dedupe key, replay path, and
  operator-facing troubleshooting surface;
- OAuth or app-install flows with refresh, revocation, scope visibility,
  reauthorization, and installation health;
- connector-specific rate-limit handling, `Retry-After` support, backpressure,
  and circuit breakers that protect both Attune and the provider;
- schema discovery, field capability metadata, enum/status mappings, type
  checks, and a previewable mapping experience;
- incremental sync cursors that tolerate replay, overlap, late-arriving
  updates, and manual recovery without losing accepted records;
- run and record timelines that show provider request ids, response classes,
  redacted payload summaries, retry decisions, and conflict diffs;
- connector contract tests, golden provider fixtures, and a qualification path
  before a provider is treated as production-ready;
- governance controls for who can create credentials, enable writes, rotate
  secrets, resolve conflicts, or export audit evidence;
- workflow automation that can use sync state as a trigger after the sync
  substrate has reliable event semantics.

### Product maturity backlog

This proposal intentionally delivers the foundation before the full product
surface. The following items define the remaining gap to a top-tier external
integration product and should be tracked as separately scoped issues.

- **Additional provider-specific signed receivers.** Extend the event ledger
  with Linear, Jira, Zendesk, and GitHub App receivers. The foundation already
  stores signed GitHub deliveries with dedupe keys, normalized payloads, replay
  linkage, and Console detail inspection. Each additional receiver should verify
  signatures, enforce replay windows, derive dedupe keys, normalize payloads,
  and enqueue replayable pull runs while retaining poll-based reconciliation.
- **OAuth and app installation lifecycle.** Add provider-specific OAuth or app
  installation flows for GitHub Apps, Linear OAuth, Jira OAuth, and Zendesk
  OAuth/bearer integrations. Connections should expose granted scopes, token
  expiry, refresh status, revoked state, reauthorization actions, and credential
  rotation audit events.
- **Runtime guardrails.** Provider attempts now capture classified HTTP status,
  provider request ids, and `Retry-After` hints, and health summaries break down
  throttled, unauthorized, provider-unavailable, delayed-retry, degraded, and
  quarantined connections. Worker retries honor future provider `Retry-After`
  values with a bounded cap, and repeated terminal failures automatically
  quarantine active connections so queued runs are no longer claimed until an
  operator resumes the connection. Extend this with per-provider and
  per-connection rate limiters, adaptive retry schedules, explicit half-open
  circuit probes, and per-tenant concurrency budgets.
- **Operator diagnostics.** Run detail now exposes cursor movement, attempts,
  provider request ids, HTTP status, retry hints, failure payload digests,
  normalized payloads, conflict snapshots, and explicit local/external/manual/
  ignored conflict resolution choices, including batch resolution for multiple
  open conflicts on one run. The Console can also request a
  record-scoped timeline that combines object-link, failure, conflict, and run
  ledger entries for a local object id or external key. Extend this with
  redacted request/response summaries, retry decisions, single-record replay
  actions, field-level conflict diffs, provider versions, suggested
  resolutions, and richer before/after previews.
- **Connector qualification.** The Console/API can now qualify a connection by
  running the registered provider check, schema discovery, schema metadata
  inspection, and scope visibility checks, then returning an auditable readiness
  report. Extend this runtime report into a certification harness with provider
  token tests, schema discovery tests, cursor tests, tombstone tests,
  rate-limit tests, redaction tests, contract fixtures, and at-least-once
  idempotency checks. Jira, Linear, Zendesk, ServiceNow, and Salesforce should
  use the same harness before being enabled for production tenants.
- **Schema-aware mapping.** The foundation exposes object names, fields,
  required fields, and writable fields, and Console validates mapping JSON shape
  while warning on discovered-field mismatches. Operators can preview a mapping
  before saving to see schema and JSON diagnostics. Extend `Discover` output so
  Console can drive enum values, status mappings, field transformations, sample
  payload previews, richer validation messages, and dry-run output before a
  mapping is enabled.
- **Cursor recovery and backfill.** The foundation now includes
  operator-safe cursor reset that clears mapping cursors, records the
  requester, and enqueues a recovery pull run. It also supports explicit
  backfill runs for pull-capable mappings, optionally clearing cursors before
  replay. Extend this with bounded windows, overlap scans, late-update
  reconciliation, mapping version migration, and clearer previews of which
  records can be reprocessed, which conflicts may re-open, and which payloads
  are only replayable through stored normalized failure data.
- **Governance and compliance.** Add per-connection RBAC, write-path approval
  controls, audit export, retention settings for attempts and payload
  summaries, PII redaction policy hooks, secret rotation status, and private
  networking guidance for enterprise deployments.
- **Workflow surface.** After signed event ingestion is reliable, allow sync
  events, conflicts, dead runs, and provider state changes to trigger workflow
  actions such as notifications, escalation, approval, or issue-state
  automation.

## Alternatives considered

- **Extend only `customer_request_issue_links`.** Rejected because it would
  duplicate connection, cursor, run, retry, and failure logic for every provider
  and every later object type.
- **Reuse `inbound_sources` as the connection table.** Rejected because inbound
  source state is intentionally small (`last_event_at`, `last_uid`,
  `last_error`) and does not model bidirectional writes, object mappings, run
  history, or record failures.
- **Reuse `notify_outbox` for sync runs.** Rejected because outbox rows model
  one outbound delivery envelope. External sync runs need cursors, mappings,
  provider attempts, per-record failures, and conflicts.
- **Store cursor JSON on the mapping row.** Rejected because cursor reset,
  per-stream sync, backfill, and run-to-cursor auditing need independent rows.
- **Build provider-specific Console pages first.** Rejected because operators
  need one reliability model and one run-detail surface across providers.

## Risks / tradeoffs

- The framework is broader than a single GitHub issue sync. Keeping the first
  implementation centered on Customer Request issue links constrains the object
  surface while preserving the reusable foundation.
- Bidirectional sync can create user-visible conflicts. The default policy
  should be conservative: record conflicts and require explicit resolution when
  provider versions disagree.
- Provider APIs differ in cursor semantics, rate limits, and tombstone
  behavior. The adapter contract must normalize these differences without hiding
  provider-specific diagnostics from run detail.
- Poll-only sync is operationally simpler than signed event ingestion, but
  top-tier integrations expect real-time webhook handling plus polling
  reconciliation. This framework keeps webhook ingestion out of the first
  implementation until signature verification, event dedupe, replay, and
  reconciliation can be specified together.
- Token credentials make the first connector usable, but OAuth and app-install
  lifecycle support is required before non-technical administrators can safely
  operate many tenant connections.
- A provider can pass the common adapter interface while still being weak in
  provider-specific edge cases. Each production connector needs a qualification
  suite, not only compile-time conformance.
- A generic mapping model can become too flexible. The first mapping schema
  should support explicit field and status maps, not arbitrary expressions.
- Credential storage touches security-sensitive paths. The implementation must
  integrate with secret key validation, backup/restore expectations, and
  redaction tests.
- Sync workers can double-write if claim fencing is incomplete. Run status
  transitions must check `claimed_by` and provider writes should use
  idempotency keys or provider version checks where available.

## Implementation plan

1. Add this proposal and mark it `Accepted` when the data model and API surface
   are agreed.
2. Add the next migration, currently 105, for connection, mapping, link, cursor,
   run, attempt, record-failure, conflict, and event tables.
3. Extend proto contracts and run `make proto` so Go, TypeScript, SDK, and
   OpenAPI artifacts stay generated.
4. Add `repo/externalsync` with claim, cursor, object-link, record-failure,
   conflict, event, attempt, and health-summary methods.
5. Add `service/externalsync` with validation, credential encryption,
   provider dispatch, pull/push run state transitions, audit recording, and
   Customer Request issue-link bridging.
6. Add provider registry, a no-op provider for development and tests, the
   GitHub Issues adapter, and an adapter authoring guide for additional
   connectors.
7. Add `ExternalSyncWorker` and wire it from `cmd/attune` with supervised
   startup, stale-claim recovery, metrics, and graceful drain.
8. Add Console handlers, router wiring, RBAC permissions, generated TS query
   helpers, provider schema discovery, Integrations navigation, health summary,
   run-detail UI, event replay diagnostics, and operator-safe cursor reset.
9. Add provider plug-in model documentation.
10. Document the benchmark baseline and product maturity backlog so the
    implemented foundation is reviewed against the larger integration-product
    target.
11. Add GitHub signed webhook delivery handling, encrypted webhook secret
    storage, Console secret configuration state, and receiver tests.
12. Add mapping preview, explicit backfill, record timeline, and automatic
    connection quarantine guardrails.
13. Add connection qualification, explicit quarantine resume, and batch conflict
    resolution across proto, service, repository, Console, and tests.
14. Update changelog when implementation code lands.

## Verification

- `make proto`
- `go test ./internal/repo/externalsync ./internal/service/externalsync ./internal/handlers/console/externalsync ./internal/externalsync ./internal/externalsync/adapter/githubissue ./internal/service/auditlog ./internal/infra/metrics`
- `go test ./internal/handlers/externalsyncwebhook`
- `go test ./internal/handlers/console`
- `go test -tags=integration ./test/integration/postgres/externalsync`
- `go test -race ./internal/externalsync/... ./internal/service/externalsync`
- `pnpm tsc -b --noEmit`
- `pnpm biome check`
- `pnpm vitest run src/features/external-sync/components/external-sync-page.test.tsx`
- `scripts/lint-slog.sh --strict`
- `scripts/lint-errorcode.sh`
- `scripts/lint-artifacts.sh --strict`
- `make ci-check`

## References

- [Issue #214](https://github.com/Phixsura/attune/issues/214)
- [External sync adapter guide](../../../external-sync-adapters.md)
- [GitHub validating webhook deliveries](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries)
- [GitHub redelivering webhooks](https://docs.github.com/en/webhooks/testing-and-troubleshooting-webhooks/redelivering-webhooks)
- [Stripe webhooks](https://docs.stripe.com/webhooks)
- [Jira Cloud webhooks](https://developer.atlassian.com/cloud/jira/platform/webhooks/)
- [Zendesk creating and monitoring webhooks](https://developer.zendesk.com/documentation/webhooks/creating-and-monitoring-webhooks/)
- [Linear OAuth 2.0 authentication](https://linear.app/developers/oauth-2-0-authentication)
- [Linear webhooks](https://linear.app/developers/webhooks)
- [Airbyte sync modes](https://docs.airbyte.com/platform/using-airbyte/core-concepts/sync-modes/)
- [Airbyte incremental sync](https://docs.airbyte.com/platform/using-airbyte/core-concepts/sync-modes/incremental-append-deduped)
- [Fivetran connection schemas](https://fivetran.com/docs/getting-started/fivetran-dashboard/connectors/schema)
- [Kafka Connect REST API status endpoints](https://docs.confluent.io/platform/current/connect/references/restapi.html)
- [Debezium signaling and incremental snapshots](https://debezium.io/documentation/reference/stable/configuration/signalling.html)
- [Singer specification](https://hub.meltano.com/singer/spec/)
- [Nango sync functions](https://nango.dev/docs/guides/functions/syncs/sync-functions)
- [Nango real-time syncs with webhooks](https://nango.dev/docs/guides/functions/syncs/realtime-syncs)
- [Merge syncing data](https://docs.merge.dev/basics/syncing-data/)
- [Merge field mapping](https://docs.merge.dev/merge-unified/supplemental-data/field-mapping/overview)
- [Merge syncing best practices](https://docs.merge.dev/merge-unified/reading-data/syncing-best-practices)
- [Salesforce Pub/Sub event message durability](https://developer.salesforce.com/docs/platform/pub-sub-api/guide/event-message-durability.html)
- [ServiceNow Integration Hub spokes](https://www.servicenow.com/docs/r/integrate-applications/integration-hub/spokes-list.html)
- [Hightouch syncs](https://hightouch.com/docs/syncs/overview)
- [Unito field mappings](https://guide.unito.io/how-to-use-custom-field-mappings-in-unito)
- [Exalate sync queue](https://docs.exalate.com/docs/sync-queue)
