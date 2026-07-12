# Slack Channel Ingest Adapter

| | |
|---|---|
| **Issue** | [#217](https://github.com/Phixsura/attune/issues/217) |
| **Status** | Implemented |
| **Started** | 2026-07-12T12:50:21+08:00 |
| **Related** | [#202](https://github.com/Phixsura/attune/issues/202), [#66](../06/2026-06-08-channel-agnostic-inbound.md), [#95](../06/2026-06-23-registry-driven-source-set.md), [#31](../06/2026-06-20-slack-adapter-wiring.md), [#220](2026-07-11-end-user-feedback-submission-portal.md) |

## Problem

Slack is one of the highest-value inbound sources for a product-feedback
platform. In most teams, the first serious signal does not arrive as a neatly
structured form submission. It shows up in a Slack channel, a thread, or a
reply buried under a day of conversation.

Attune already has the right pieces to support a world-class Slack connector:

- a channel-agnostic inbound framework,
- a registry-driven source vocabulary,
- a normalized feedback ingest path,
- an outbox envelope that already carries `source_display`,
- and a Console inbound-sources surface.

What is missing is the Slack source itself. Today, operators must bridge Slack
manually, or re-enter information into another channel. That loses author
identity, thread context, permalinkability, and dedupe safety. It also makes
Slack feel like an afterthought rather than a first-class source.

This issue should not be treated as "add another webhook." Slack is a
stateful source connector with:

- a discoverable workspace/channel universe,
- explicit auth and scope checks,
- a message and thread hierarchy,
- a stable message identity,
- cursor or watermark state,
- and retry-safe replay behavior.

## Goals

- Add Slack as a first-class inbound source in Console and API.
- Preserve channel, thread, author, permalink, and timestamp metadata.
- Deduplicate retries and replays so the same Slack message never creates a
  duplicate feedback row.
- Support operator test/verify flows before a source is enabled.
- Keep the Slack ingestion core transport-agnostic so a future Events API mode
  can reuse the same normalization and dedupe logic.
- Reuse the existing `source_display` envelope field and the current
  `inbound_sources` state model as much as possible.

## Acceptance Criteria

- A selected Slack channel can feed messages and thread replies into Attune
  feedback.
- Slack metadata is preserved without leaking tokens, signing material, or raw
  replay secrets.
- Replays and retries do not create duplicate feedback rows.
- Operators can test auth, discover readable channels, and see sync health
  before enabling the source.
- The first release uses polling, but the normalized ingest shape stays
  transport-agnostic for a later Events API transport.
- The first release is explicit about the supported token model and fails
  closed for unsupported scopes or inaccessible channels.
- Freshly created Slack sources start from `now`; any historical backfill is a
  separate operator action and is out of scope for this issue.

## Non-goals

- Do not build a Slack outbound target.
- Do not change the outbox rendering pipeline for Slack.
- Do not turn Slack into a full chat-sync product.
- Do not add a separate feedback schema for Slack.
- Do not require a new core source vocabulary change.
- Do not bulk backfill historical Slack history at source creation.

## World-Class Benchmarks

The best systems in this problem space do not hard-code integrations into the
core. They converge on a few reliable patterns:

| Project | What it does well | Lesson for Attune |
|---|---|---|
| OpenTelemetry Collector | Components are assembled from factories, with metadata co-located in component descriptors and distributions built from a registry snapshot. | Build one immutable source set at the composition root, and keep display metadata next to registration. |
| HashiCorp Vault | Built-in plugins are preregistered, external plugins use a catalog, and the core uses an injected registry seam to avoid import cycles. | Keep the pure validation layer dependent on an injected snapshot, never on live adapter packages. |
| Kubernetes admission plugins | Compiled-in plugins register into a central plugin set; enable/disable decisions are made from that registered list. | Fail fast on duplicate or reserved identifiers, and validate from a startup snapshot. |
| CoreDNS | Plugins declare Setup, Registration, and Handler; registration is compile-time, configuration is runtime, and order is only special when semantics require it. | Add codegen or ordering only if it is load-bearing; otherwise prefer a runtime registry. |
| Caddy | Every module has a unique namespaced ID, self-registers, and publishes docs from the registry. | Co-locate the Slack channel identity and human label with the registration site. |
| Telegraf | Inputs/outputs/processors/aggregators register as plugins; repeated instances are normal and can have independent aliases and intervals. | Treat each Slack source as an independent instance, not as a singleton integration. |
| gRPC-Go | Resolver and balancer builders register by scheme/name, with strict naming discipline and init-time registration. | Keep Slack identifiers immutable, normalized, and collision-checked. |
| Airbyte | Source connectors implement `spec`, `check`, `discover`, and `read`; connectors are versioned, stateful, and testable. | Slack should have a discover/check/read-shaped lifecycle, not a one-off handler. |
| n8n | Trigger/action nodes separate execution from credentials; community nodes are packaged and loaded independently; webhook triggers have a complete lifecycle. | Separate Slack credentials from sync configuration and keep the lifecycle explicit. |
| Vector | Inventory-driven component registration and generated docs keep component metadata and validation in sync. | Eliminate hand-maintained parallel lists for Slack channel labels or states. |

### Shared patterns from the benchmark

1. The core never imports the concrete plugin implementation.
2. Valid sets are assembled once and consumed as immutable data.
3. Identifiers are append-only and fail fast on collision.
4. Registration metadata lives beside the thing being registered.
5. Stateful connectors expose explicit check/discover/read semantics.
6. Credentials are separate from runtime behavior.
7. Read paths gracefully handle retired or stale identifiers.

Taken together, these systems converge on the same operating model:
register once at the composition root, freeze the registry into an immutable
snapshot, keep credentials separate from runtime logic, and expose lifecycle
hooks that make connectors testable before they are enabled. Slack should follow
that model instead of becoming an ad hoc handler.

## Proposal

### 1. Treat Slack as a source connector, not a handler

Add `internal/inbound/adapter/slack` as a new inbound adapter package with a
single registered channel name, `slack`.

The adapter should expose one normalization core and one transport boundary.
The first release should ship the poll transport, but the adapter design must
leave room for an Events API transport without changing the normalized ingest
shape.

That means:

- one adapter package,
- one registry entry,
- one Console create flow,
- one normalized feedback shape,
- and one dedupe strategy shared by all Slack transports.

### 2. Make Slack look like Airbyte-style source semantics

Slack source creation should follow a discover/check/read pattern:

- `check` verifies auth and workspace access.
- `discover` lists channels the app can actually read.
- `read` syncs messages and replies into normalized feedback.

The Console create flow should not ask operators to hand-type opaque IDs if the
API can discover them safely. The preferred flow is:

1. Paste or configure a Slack app token.
2. Run a connection check.
3. Discover accessible channels.
4. Pick the target channel.
5. Save the source.

Implementation-wise, that maps cleanly to Slack's own API surface:

- `auth.test` for workspace identity and token validity,
- `conversations.list` for discovery,
- `conversations.history` and `conversations.replies` for message sync,
- and `chat.getPermalink` for canonical message URLs.

The proposal intentionally keeps v1 narrow:

- supported auth mode: workspace bot token for a single workspace-installable
  app;
- supported objects: public and private channels that the token can actually
  read;
- unsupported in v1: DMs, RTM/Socket Mode, and user-token-based scraping.

If the token lacks the scopes or channel membership required by Slack's own
API, `check` and `discover` fail closed with a clear operator-facing error.

Slack discovery should be an explicit RPC, not an implied side effect of create.
The Console should call a dedicated `DiscoverSlackChannels` endpoint that returns
the readable channel list, then pass the selected `channel_id` into create.
That keeps create idempotent, keeps discovery testable, and avoids baking a UI
only flow into the core contract.

Proposed wire shape:

- `DiscoverSlackChannelsRequest` carries the Slack token and any workspace hint.
- `DiscoverSlackChannelsResponse` returns readable channels with `channel_id`,
  `name`, `is_private`, `is_member`, and a cached display label.
- `SlackCreateConfig` carries the selected `channel_id`, the write-only token,
  and a display label snapshot for persistence.
- `SlackConnConfig` is the narrower test-only shape used by `TestConnection`.

Token-bearing request data is never written verbatim into audit events, traces,
or logs. Operators may see the workspace/team id, selected channel id, readable
channel count, and outcome, but not the raw Slack token or signing material.

### 3. Normalize Slack data into Attune feedback with immutable identity

Slack messages should normalize to Attune like this:

- `content` = canonical plain text extracted from the message payload.
- `source` = `slack`.
- `source_user` = stable Slack user ID when available.
- `page_url` = Slack permalink.
- `source_meta.slack` = workspace, channel, thread, author, and delivery metadata.
- `IdempotencyKey` = a deterministic Slack message key.

Recommended stable key:

`slack:<team_id>:<channel_id>:<message_ts>`

That key should be used for both root messages and thread replies. Replays and
duplicate deliveries then become a storage concern the existing ingest layer can
already handle.

Do not force a semantic `type` at ingest. Let the classifier own the product
taxonomy. Slack is a source of evidence, not a source of truth about the
feedback category.

### 4. Use the existing source-state model, with Slack-specific meaning

The current `inbound_sources` schema already has the right operational fields:

- `enabled`
- `last_event_at`
- `last_uid`
- `last_error`

For Slack, this proposal uses those fields like this:

- `last_event_at` = last successful sync heartbeat.
- `last_uid` = monotonic sync watermark derived from Slack message timestamps.
- `last_error` = last sync or auth failure.

That keeps the first release schema-stable. If the adapter ever needs an opaque
cursor in addition to a watermark, add it later only if operational evidence says
the watermark is not enough.

Slack credentials and per-source sync settings should stay inside the existing
encrypted `inbound_sources.config` blob. The adapter only needs to persist the
minimum viable state there:

- Slack workspace / team identifier,
- selected channel identifier,
- a human-readable channel snapshot,
- the auth token or equivalent write-only credential,
- the poll transport mode for v1,
- and any lookback / thread-hydration tuning needed to absorb late thread
  activity.

For the watermark itself, treat Slack's message `ts` as a Unix microsecond
counter: parse `seconds.microseconds` into `seconds * 1_000_000 + micros`, store
the maximum seen value in `last_uid`, and keep the deterministic
`slack:<team_id>:<channel_id>:<message_ts>` key as the replay fence.

Do not treat `last_uid` as a full thread cursor. It is the channel root-message
watermark only. Thread replies need a small, bounded thread cache in the
encrypted config blob so the poller can remember which roots have already been
hydrated and which reply timestamps were last seen.

The thread cache is a compact LRU/TTL map keyed by root `ts` and storing:

- `root_ts`
- `last_reply_ts`
- `reply_count`
- `last_hydrated_at`
- `last_seen_at`

Suggested bounds for v1:

- capacity: 500 active roots per source,
- TTL: 30 days since `last_seen_at`,
- eviction: least-recently-seen entries first.

On restart, the adapter reloads the persisted cache and continues from the last
saved state. Roots outside the cache are intentionally treated as inactive until
they reappear in the channel lookback window.

### 5. Prefer a poll-first transport, but keep the adapter transport-agnostic

The first release should use polling as the shipping transport because it:

- fits the current inbound framework cleanly,
- supports private/self-hosted deployments without requiring public ingress,
- and can be made replay-safe with a watermark plus idempotency key.

The adapter should still be structured so an Events API transport can be added
later behind the same normalization core.

Recommended poll behavior:

- use `conversations.history` for the selected channel,
- follow cursor pagination for history and thread-reply fetches until Slack
  returns an empty `next_cursor`, with a hard cap to avoid infinite loops,
- apply a bounded sliding lookback window to catch late arrivals and thread
  activity, not to bulk-import history,
- hydrate replies only for roots that are new, recently active, or whose
  `reply_count` / latest reply timestamp has advanced since the last sync,
- cap the number of thread hydrations per poll cycle so rate limits stay
  predictable,
- and dedupe every row on the way into Attune.

This mirrors what the strongest connector platforms do: one logical connector,
multiple capture strategies, one state model.

### 6. Support thread replies without inventing a second feedback pipeline

Slack threads should not become a separate data model. Root messages and thread
replies both land as feedback rows, with `source_meta.slack.thread_ts` and a
deterministic idempotency key making them replay-safe.

The adapter should preserve:

- root message content,
- reply content,
- author identity,
- thread root timestamp,
- reply timestamp,
- permalink,
- and channel identity.

If the operator wants to review a thread as a single story later, that is a
presentation concern. The ingest layer should continue to store atomic evidence.

### 7. Keep Console a discover/check/select flow

Console should gain a Slack branch in the create dialog with:

- Slack-specific auth/config inputs,
- a connection test button,
- a channel discovery step,
- and a clear status display after creation.

On the wire, that means adding a Slack-specific config message to
`CreateInboundSourceRequest` and `TestInboundConnectionRequest`. The auth token
stays write-only inside the encrypted `inbound_sources.config` blob; the API
returns only the discovered channel identity and sync status.

The list view should show Slack as a distinct channel badge instead of falling
back to a generic or raw label.

The health model should remain familiar:

- healthy,
- paused,
- error,
- last sync time.

### 8. Preserve downstream rendering behavior

No outbox renderer changes are required for this issue.

Attune already stamps `source_display` onto the outbound envelope, and the
GitHub issue / notify renderers already fall back safely when the field is
missing. Slack ingress should therefore show up naturally in downstream
delivery and reporting.

## Alternatives considered

### A. Events API only

Rejected for v1.

It is operationally elegant, but it requires public ingress, signature
verification, timestamp replay windows, and additional deployment guidance.
That is a lot of surface area for the first pass.

### B. Poll only with no transport abstraction

Rejected.

It would be easy to ship, but it would lock the adapter into one capture
strategy and make a future Events API mode awkward. Slack deserves a connector
shape that can evolve.

### C. New Slack-only state table or opaque cursor column

Rejected for the first release.

The current inbound source state model is already enough for a watermark-based
poller. Add a cursor field later only if the operating evidence says the
watermark cannot cover the workload.

### D. Treat Slack as a generic webhook

Rejected.

Slack is not a generic webhook source. It has channel discovery, thread
hierarchy, message identity, and replay semantics. A generic webhook would
throw away too much structure.

## Risks / tradeoffs

- Slack reply hydration can hit rate limits if it is naive. The adapter needs a
  bounded replay window, bounded thread hydration, backoff, and dedupe.
- Polling alone can miss very old thread updates unless the lookback window is
  chosen carefully.
- A public Events API mode is more real-time, but it increases deployment
  complexity.
- Workspace access differs for public channels, private channels, and org-wide
  installs. The create flow must fail closed when the selected channel is not
  readable.
- Message normalization is intentionally lossy. The feedback row should retain
  only structured Slack metadata, not the full raw payload. If we need deeper
  diagnostics, they should be emitted as redacted logs or bounded internal
  traces, not copied into user-visible feedback.

## Implementation plan

1. Extend the proto contract with Slack config messages plus an explicit
   `DiscoverSlackChannels` RPC.
2. Extend the Console create flow with a Slack branch, connection test, and
   channel discovery.
3. Add `internal/inbound/adapter/slack` with a normalization core and poll
   transport.
4. Normalize Slack messages and replies into `domain.IngestInput` using a
   deterministic `IdempotencyKey`.
5. Reuse `inbound_sources.last_uid` as a Slack root-message watermark and
   `last_event_at` as the sync heartbeat.
6. Keep the adapter transport-agnostic so an Events API mode can be added
   later without changing the stored feedback shape.
7. Add adapter conformance tests, fixture-based Slack payload tests, and
   Console/API tests for create/test/discover flows.
8. Add a configurable Slack API base URL so browser and CI smoke tests can
   point at a local mock Slack API without changing production defaults.
9. Update docs with the exact Slack auth, scope, and operational model.

## Verification

- `go build ./...`
- `go test ./...`
- adapter conformance tests for the Slack package
- fixture tests for:
  - message normalization,
  - thread reply normalization,
  - permalink extraction,
  - duplicate replay dedupe,
  - and watermark advancement
- Console build and typecheck for the inbound-sources flow
- discovery-contract tests for channel listing and auth failure cases
- bounded thread-hydration tests for new roots, updated replies, and
  rate-limit-aware retry paths
- mock-API browser smoke tests that exercise auth, discovery, test-connection,
  and create against a loopback Slack origin

If Events API mode is added later, add tests for:

- request verification,
- timestamp replay rejection,
- and endpoint handshake / challenge handling.

## References

### Attune docs and code

- [Channel-agnostic inbound framework](../06/2026-06-08-channel-agnostic-inbound.md)
- [Registry-driven source set](../06/2026-06-23-registry-driven-source-set.md)
- [Slack outbound adapter wiring](../06/2026-06-20-slack-adapter-wiring.md)
- [End-user feedback submission portal](2026-07-11-end-user-feedback-submission-portal.md)

### Slack docs

- [The Events API](https://docs.slack.dev/apis/events-api/)
- [Verifying requests from Slack](https://docs.slack.dev/authentication/verifying-requests-from-slack)
- [Conversations API overview](https://docs.slack.dev/apis/web-api/using-the-conversations-api)
- [conversations.history](https://docs.slack.dev/reference/methods/conversations.history)
- [conversations.replies](https://docs.slack.dev/reference/methods/conversations.replies)
- [conversations.list](https://docs.slack.dev/reference/methods/conversations.list)
- [auth.test](https://docs.slack.dev/reference/methods/auth.test)
- [chat.getPermalink](https://docs.slack.dev/reference/methods/chat.getPermalink)
- [Rate limit changes for non-Marketplace apps](https://docs.slack.dev/changelog/2025/05/29/rate-limit-changes-for-non-marketplace-apps)

### Benchmark projects

- [OpenTelemetry Collector components](https://opentelemetry.io/docs/collector/components/)
- [OpenTelemetry Collector custom components](https://opentelemetry.io/docs/collector/extend/custom-component/)
- [Vault plugin ecosystem](https://developer.hashicorp.com/vault/docs/plugins)
- [Vault plugin architecture](https://developer.hashicorp.com/vault/docs/plugins/plugin-architecture)
- [Kubernetes admission controllers](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/)
- [Kubernetes dynamic admission control](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
- [CoreDNS plugin development](https://coredns.io/manual/plugins-dev/)
- [Caddy modules](https://caddyserver.com/docs/modules)
- [Extending Caddy](https://caddyserver.com/docs/extending-caddy)
- [Telegraf INPUTS.md](https://github.com/influxdata/telegraf/blob/master/docs/INPUTS.md)
- [Telegraf configuration](https://github.com/influxdata/telegraf/blob/master/docs/CONFIGURATION.md)
- [gRPC-Go resolver registry](https://github.com/grpc/grpc-go/blob/master/resolver/resolver.go)
- [gRPC-Go balancer registry](https://github.com/grpc/grpc-go/blob/master/balancer/balancer.go)
- [Airbyte connector protocol](https://github.com/airbytehq/airbyte/blob/master/docs/platform/understanding-airbyte/airbyte-protocol.md)
- [Airbyte connector development](https://docs.airbyte.com/platform/connector-development/)
- [n8n node types](https://docs.n8n.io/integrations/builtin/node-types)
- [n8n credentials](https://docs.n8n.io/integrations/builtin/credentials)
- [n8n custom nodes starter](https://github.com/n8n-io/n8n-nodes-starter)
- [Vector inventory registration](https://rust-doc.vector.dev/src/vector_config/component/mod.rs.html?search=)
- [Vector contribution guide](https://github.com/vectordotdev/vector/blob/master/CONTRIBUTING.md)
