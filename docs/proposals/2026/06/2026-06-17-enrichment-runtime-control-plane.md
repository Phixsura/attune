# Enrichment runtime control plane for live queue and rate-limit tuning

| Field | Value |
| --- | --- |
| **Issue** | [#80](https://github.com/Phixsura/attune/issues/80) |
| **Status** | Implemented |
| **Started** | 2026-06-17 23:55 CST |
| **Related** | [#10](https://github.com/Phixsura/attune/issues/10), [#25](https://github.com/Phixsura/attune/issues/25), [#26](https://github.com/Phixsura/attune/issues/26), [#109](https://github.com/Phixsura/attune/issues/109) |

## Problem

The bounded enrichment runner added for #80 is configurable only from process
YAML and only at startup. That is not enough for an operator-facing system that
must react to burst load, provider incidents, cost pressure, and backlog
recovery in real time.

The first proposal version established the right direction, but review exposed
four boundary problems that must be designed explicitly before implementation:

1. **Control-plane ownership is unclear.**
   Existing Console auth, RBAC, and audit are tenant-scoped, while the new
   knobs affect deployment runtime behavior.
2. **Desired policy and live runtime state are mixed together.**
   A single `effective` snapshot cannot describe partial apply, multi-instance
   drift, or component-specific failures.
3. **Hot reconfiguration semantics are under-specified.**
   Queue resize, limiter swap, rollback, and boot recovery all need a real
   state machine, not just “save then apply.”
4. **The persistence model is too vague for long-term operation.**
   We need explicit source-of-truth rules between YAML bootstrap defaults, DB
   overrides, per-instance runtime status, and operator rollback.

This proposal replaces the MVP-shaped idea with a full runtime control-plane
design intended for production use, not just a quick operator form.

## Goals / Non-goals

### Goals

- Provide a **durable deployment policy** for enrichment runtime behavior.
- Provide **live per-instance runtime status** for operators.
- Allow **restart-free reconfiguration** on live nodes.
- Define a **safe multi-instance convergence model**.
- Separate **spec**, **metadata**, and **status** in the API and storage model.
- Make conflict handling, rollback, validation, and audit first-class concerns.
- Fit the existing repository architecture without hand-waving around current
  tenant-scoped constraints.

### Non-goals

- Do not build a generic dynamic-config platform for every subsystem.
- Do not introduce Redis, etcd, or another distributed control-plane
  dependency in v1.
- Do not make runtime queue depth or in-flight counts operator-editable.
- Do not promise strict cluster-wide atomic reconfiguration.
- Do not change enrichment semantics, prompt rendering, or dimension logic.

## Current state

### What is already configurable

The backend YAML config already includes:

- `enricher.queue_len`
- `enricher.workers`
- `enricher.batch`
- `enricher.batch_window`
- `enricher.interval`
- `enricher.llm_max_qps`
- `enricher.llm_burst`

These are parsed at startup and wired into:

- `enrich.NewRunner(...)`
- `llmclient.NewRateLimitedClient(...)`

### What the Console exposes today

The current Console settings UI and `enrich-config` API expose only tenant
authored enrichment behavior:

- `prompt_template`
- `default_prompt_template`
- `dimensions`
- preview sample / rendered prompt

### What is missing

- no deployment-scoped operator surface
- no runtime config persistence model
- no live `Configure(...)` for runner
- no live `Configure(...)` for rate limiter
- no per-instance applied status
- no conflict/rollback protocol
- no deployment-scoped audit story

## Core design principles

### 1. Separate policy from runtime

This system must model three different things explicitly:

1. **Bootstrap defaults** — process YAML at boot
2. **Desired deployment policy** — operator-managed, persisted in Postgres
3. **Per-instance runtime state** — what each live replica actually applied

They are related, but they are not the same object.

### 2. Separate spec, revision metadata, and status

The editable knobs are one thing. Who changed them and at what version is a
different thing. Whether each instance applied them is a third thing.

### 3. Separate deployment policy from instance-local mechanics

`workers`, `queue_len`, and the local LLM limiter are implemented as instance
local mechanisms. The control plane can carry one desired policy, but every
replica must report its own apply result.

### 4. Prefer typed feature-specific models over generic config platforms

Attune’s repo structure favors typed feature packages, typed repos, and explicit
data models. This proposal follows that style instead of introducing a generic
KV config subsystem.

## Scope and ownership

### Tenant enrichment authoring config

Unchanged and still tenant-scoped:

- prompt template override
- dimensions
- preview prompt rendering

This remains under the existing classification settings flow.

### Enrichment runtime policy

New deployment-scoped operator concern:

- queue length
- worker concurrency
- sweep batch size
- batch window
- sweep interval
- LLM local rate-limit enabled
- LLM max QPS
- LLM burst

### Operator identity model

This proposal introduces a new concept:

- **deployment operator**

That is intentionally different from tenant admin/member/viewer.

Because the current Console stack is tenant-bound, the proposal makes this
constraint explicit:

- **v1 support target:** private deploy / single-tenant operational ownership,
  with deployment policy managed by an authenticated operator surface
- **not supported in v1:** pretending that ordinary tenant admins can safely
  manage deployment-global runtime policy in a shared multi-tenant control plane

If Attune later wants true shared SaaS tenant-facing operation here, it will
need a deployment-scoped identity and audit model beyond this feature.

## Source of truth and precedence

### Bootstrap default

YAML remains the bootstrap source for default runtime values.

### Desired policy authority

Once a DB policy revision exists, it becomes the authoritative desired policy
for the deployment.

### Default provenance

“Reset to default” must be data-driven, not request-node-driven.

For v1, each persisted desired revision stores:

- a full resolved spec snapshot
- the `bootstrap_snapshot_version` used when that revision was created

This keeps reset and rollback deterministic even if process YAML later changes.

### Effective runtime authority

Each instance reports its own applied runtime state. There is no single global
effective runtime object.

### Reset semantics

Operators need explicit reset behavior. This proposal supports:

1. **Reset field to bootstrap default**
2. **Clear all DB overrides and return to bootstrap defaults**
3. **Rollback to prior stored revision**

This avoids “shadowed YAML forever” behavior.

### YAML consistency expectation

Bootstrap YAML defaults are expected to be operationally identical across all
replicas in one deployment. The control plane should surface the bootstrap
snapshot version in the read model so operators can detect mismatched rollouts.

## Data model

Use typed feature-specific tables.

### `enrichment_runtime_policy`

One row per deployment policy keyspace.

Suggested columns:

```sql
create table enrichment_runtime_policy (
  policy_key text primary key,
  queue_len integer not null,
  workers integer not null,
  batch_size integer not null,
  batch_window_ms bigint not null,
  sweep_interval_ms bigint not null,
  llm_rate_limit_enabled boolean not null,
  llm_max_qps double precision not null,
  llm_burst integer not null,
  bootstrap_snapshot_version text not null,
  spec_version integer not null,
  last_known_good_version bigint not null default 0,
  version bigint not null,
  updated_at timestamptz not null default now(),
  updated_by text not null,
  update_reason text not null default '',
  check (policy_key = 'default'),
  check (queue_len > 0),
  check (workers > 0),
  check (batch_size > 0),
  check (batch_window_ms > 0),
  check (sweep_interval_ms > 0),
  check (llm_max_qps >= 0),
  check (llm_burst >= 0),
  check (spec_version > 0)
);
```

This table stores the **desired policy** only.

### `enrichment_runtime_policy_history`

Immutable revision history for rollback and audit support.

Suggested columns:

- policy fields snapshot
- `version`
- `bootstrap_snapshot_version`
- `spec_version`
- `operation_type`
- `changed_at`
- `changed_by`
- `change_reason`

History is append-only. `rollback` always creates a new version rather than
rewinding the current row in place. Current-row update and history append must
happen in one DB transaction with serialized version allocation.

### `enrichment_runtime_instance_status`

Per-instance live apply state.

Suggested columns:

```sql
create table enrichment_runtime_instance_status (
  instance_id text primary key,
  boot_id text not null,
  desired_version bigint not null,
  observed_desired_version bigint not null,
  runner_effective_version bigint not null,
  limiter_effective_version bigint not null,
  attempted_runner_version bigint not null default 0,
  attempted_limiter_version bigint not null default 0,
  runner_apply_status text not null,
  limiter_apply_status text not null,
  runner_last_apply_error text not null default '',
  limiter_last_apply_error text not null default '',
  queue_depth integer not null default 0,
  queue_capacity_target integer not null default 0,
  queue_capacity_effective integer not null default 0,
  queue_resize_pending boolean not null default false,
  in_flight integer not null default 0,
  degraded_reason text not null default '',
  applied_spec_json jsonb not null,
  heartbeat_interval_ms bigint not null,
  stale_after_ms bigint not null,
  expire_after_ms bigint not null,
  last_applied_at timestamptz,
  last_reconciled_at timestamptz not null,
  last_seen_at timestamptz not null
);
```

This table stores **instance runtime status**, not desired policy.

Instance-row ownership rules:

- `instance_id` identifies the logical replica
- `boot_id` identifies one concrete process lifetime
- a restarted process must generate a new `boot_id`

Staleness rules:

- a row becomes `stale` when `now - last_seen_at > stale_after`
- a row becomes `expired` when `now - last_seen_at > expire_after`
- expired rows must not count toward convergence summaries

## API contract

The control plane needs distinct API types.

### `EnrichmentRuntimeSpec`

Editable policy knobs only.

```proto
message EnrichmentRuntimeSpec {
  int32 queue_len = 1;
  int32 workers = 2;
  int32 batch_size = 3;
  google.protobuf.Duration batch_window = 4;
  google.protobuf.Duration sweep_interval = 5;
  bool llm_rate_limit_enabled = 6;
  double llm_max_qps = 7;
  int32 llm_burst = 8;
}
```

### `EnrichmentRuntimeRevision`

Metadata about the desired policy revision.

```proto
message EnrichmentRuntimeRevision {
  uint64 version = 1;
  google.protobuf.Timestamp updated_at = 2;
  string updated_by = 3;
  string update_reason = 4;
  string bootstrap_snapshot_version = 5;
  uint32 spec_version = 6;
  uint64 last_known_good_version = 7;
}
```

### `EnrichmentRuntimeInstanceStatus`

Per-instance live state.

```proto
message EnrichmentRuntimeInstanceStatus {
  string instance_id = 1;
  string boot_id = 2;
  uint64 desired_version = 3;
  uint64 observed_desired_version = 4;
  uint64 runner_effective_version = 5;
  uint64 limiter_effective_version = 6;
  uint64 attempted_runner_version = 7;
  uint64 attempted_limiter_version = 8;
  RuntimeApplyStatus runner_apply_status = 9;
  RuntimeApplyStatus limiter_apply_status = 10;
  string runner_last_apply_error = 11;
  string limiter_last_apply_error = 12;
  int32 queue_depth = 13;
  int32 queue_capacity_target = 14;
  int32 queue_capacity_effective = 15;
  bool queue_resize_pending = 16;
  int32 in_flight = 17;
  string degraded_reason = 18;
  google.protobuf.Timestamp last_applied_at = 19;
  google.protobuf.Timestamp last_reconciled_at = 20;
  google.protobuf.Timestamp last_seen_at = 21;
  EnrichmentRuntimeSpec applied_spec = 22;
}
```

```proto
enum RuntimeApplyStatus {
  RUNTIME_APPLY_STATUS_UNSPECIFIED = 0;
  RUNTIME_APPLY_STATUS_PENDING = 1;
  RUNTIME_APPLY_STATUS_APPLYING = 2;
  RUNTIME_APPLY_STATUS_APPLIED = 3;
  RUNTIME_APPLY_STATUS_FAILED = 4;
  RUNTIME_APPLY_STATUS_STALE = 5;
  RUNTIME_APPLY_STATUS_DEGRADED = 6;
}
```

### `EnrichmentRuntimeReadModel`

Combined operator view:

```proto
message EnrichmentRuntimeReadModel {
  EnrichmentRuntimeSpec bootstrap_default = 1;
  EnrichmentRuntimeSpec desired_spec = 2;
  EnrichmentRuntimeRevision desired_revision = 3;
  EnrichmentRuntimeSummary summary = 4;
  repeated EnrichmentRuntimeInstanceStatus instances = 5;
}
```

```proto
message EnrichmentRuntimeSummary {
  uint64 desired_version = 1;
  uint32 live_instances = 2;
  uint32 stale_instances = 3;
  uint32 expired_instances = 4;
  uint32 degraded_instances = 5;
  uint32 fully_applied_instances = 6;
  bool fully_converged = 7;
}
```

### Service methods

```proto
service EnrichmentRuntimeService {
  rpc GetEnrichmentRuntime(GetEnrichmentRuntimeRequest)
      returns (GetEnrichmentRuntimeResponse);

  rpc UpdateEnrichmentRuntime(UpdateEnrichmentRuntimeRequest)
      returns (UpdateEnrichmentRuntimeResponse);

  rpc ResetEnrichmentRuntime(ResetEnrichmentRuntimeRequest)
      returns (ResetEnrichmentRuntimeResponse);

  rpc RollbackEnrichmentRuntime(RollbackEnrichmentRuntimeRequest)
      returns (RollbackEnrichmentRuntimeResponse);
}
```

### Update contract

`UpdateEnrichmentRuntimeRequest` must include:

- `expected_version`
- `spec`
- `update_reason`

Conflict behavior:

- stale version returns `409 CONFLICT`
- response includes latest desired revision and spec

Reset contract:

- reset one field to bootstrap default, or all fields
- reset requires `expected_version`
- reset creates a new revision

Rollback contract:

- rollback to a known prior version
- rollback requires `expected_version`
- rollback creates a new revision
- prefer restricting rollback targets to previously validated revisions

First-write contract:

- when no DB policy exists yet, `expected_version = 0`
- first successful write creates version `1`

## Validation rules

Validation must exist at service level and partly at DB level.

### Numeric and duration checks

- `queue_len > 0`
- `workers > 0`
- `batch_size > 0`
- `batch_window > 0`
- `sweep_interval > 0`
- `llm_max_qps` must be finite and `>= 0`
- `llm_burst >= 0`

### Cross-field rules

- `workers <= queue_len`
- `batch_size <= queue_len`
- if rate limit enabled, `llm_max_qps > 0`
- if rate limit enabled, `llm_burst >= 1`
- if rate limit disabled, `llm_max_qps` may be `0`

### Safety caps

Initial caps:

- `queue_len <= 10000`
- `workers <= 512`
- `batch_size <= 1000`
- `batch_window <= 10m`
- `sweep_interval <= 1h`
- `llm_max_qps <= 10000`
- `llm_burst <= 10000`

### High-risk change policy

The backend should identify risky changes for operator confirmation and audit:

- reducing `llm_max_qps` by more than 80%
- increasing `workers` above a threshold
- shrinking `queue_len` below current observed depth

These are not validation failures by themselves, but they should be marked as
high-risk operations in the read/write model.

### Compatibility rules

The stored desired policy must carry a `spec_version`.

Rules:

- binaries may only apply supported `spec_version` values
- unsupported spec versions must surface explicit degraded status
- reconcilers must not silently reinterpret unsupported policy shapes

## Runtime apply architecture

### Components

The control plane manages two independent runtime components:

1. **Runner runtime**
2. **Limiter runtime**

They must report status separately.

### Apply state machine

For one instance:

1. load desired revision
2. validate desired revision against current binary rules
3. attempt limiter apply
4. record limiter result
5. attempt runner apply
6. record runner result
7. publish full instance status

Important:

- there is **no fake single effective state**
- partial success is visible and persisted
- retries operate against desired version until both components converge
- stale apply completions must not overwrite state for newer attempted versions

### Rollback policy

This proposal does **not** do automatic rollback on partial component failure.

Reason:

- rollback itself can fail
- automatic rollback can oscillate under persistent runtime errors
- operator visibility is more important than hidden churn

Instead:

- desired policy remains stored
- failed component status is marked explicitly
- background reconcile keeps retrying with backoff
- operator may rollback revision explicitly
- the system records `last_known_good_version` whenever a revision fully
  converges on all live instances

### Quarantine policy

If a desired revision is invalid for the current binary or repeatedly fails
reconcile, the system should surface a quarantined/degraded state and preserve
`last_known_good_version` for operator recovery.

## Multi-instance convergence

### Desired policy

One desired policy revision is shared through Postgres.

### Local convergence

Every instance runs a reconciler loop:

1. read latest desired revision
2. compare with local applied component versions
3. apply missing/outdated components
4. publish heartbeat status row

Reconcile rules:

- instances converge directly to the latest desired version
- intermediate versions may be skipped
- each component records the attempted version it is applying
- a slow completion for version `N` must be ignored once version `N+1` has been
  attempted or applied

### Propagation

Phase design:

1. **v1:** short polling, for example every 2-5 seconds
2. **v2:** Postgres `LISTEN/NOTIFY` to accelerate propagation
3. poller remains as the recovery path

### What the Console shows

The Console must show:

- desired revision
- cluster summary
- per-instance rows
- stale instances
- expired instances if requested
- last seen times
- component-specific apply failures

No single cluster-level `effective` value is exposed as if it were exact truth.

## Queue reconfiguration design

This area needs an explicit algorithm.

### Constraint

The current runner consumes from a channel returned once by `Consume()`. A naive
channel swap is unsafe.

### Proposed design

Replace direct consumer access to the storage channel with a stable broker.

#### Broker model

- one stable ingress API for `Submit`
- one stable egress channel for worker consumption
- internal queue storage owned by a broker goroutine or broker object
- resize changes broker capacity rules, not worker-facing channel identity

#### Resize behavior

- **grow:** increase accepted capacity immediately
- **shrink above current depth:** enforce immediately
- **shrink below current depth:** accept the new configured target, but defer
  enforcement until depth drains below target

This avoids job loss and avoids unsafe consumer channel swaps.

#### Operator-visible semantics

When shrink is deferred:

- desired spec shows new `queue_len`
- instance status shows:
  - `queue_capacity_target`
  - `queue_capacity_effective`
  - `queue_resize_pending = true`

### Why not reject shrink-below-depth

Rejecting it forces operators to wait for a quiet moment. Deferred enforcement is
more operationally useful and still safe if status is explicit.

## Rate-limiter reconfiguration design

### Mutable limiter wrapper

The limiter runtime is represented as an atomically swapped generation:

- enabled/disabled
- qps
- burst
- generation id
- limiter pointer

### Behavior

- new requests observe the new generation immediately after apply
- already-waiting requests may complete under the old generation

### Important wording

“Immediate effect” means **new work** observes the new config immediately. It
does not mean already-waiting requests are forcibly preempted.

### Disable behavior

Disabling the limiter stops gating new requests immediately. Existing waiters are
allowed to complete naturally under the prior generation.

This is simpler, safer, and easier to reason about than canceling old waiters.

## Boot behavior and bad-config recovery

### Boot sequence

1. load YAML bootstrap defaults
2. load latest desired policy from DB if present
3. validate desired policy against current binary
4. if valid, start runtime with desired policy and publish status
5. if invalid, start with bootstrap defaults, mark instance degraded, and
   publish the validation failure against desired version

### Why not fail startup hard

Refusing startup because one desired runtime policy is invalid creates a larger
availability blast radius than running degraded with visible status.

### Recovery from bad desired policy

- reconcile loop keeps reporting failure
- Console highlights invalid desired policy
- operator may reset offending fields or rollback revision
- operator may promote `last_known_good_version`

## Repository architecture fit

To keep the implementation aligned with repo style:

- use a typed repo package, for example
  `internal/repo/enrichmentruntime`
- use a typed service package, for example
  `internal/service/enrichruntime`
- keep Console handlers separate from tenant `enrich-config`
- keep audit integration explicit

This proposal does **not** introduce a generic “runtime config” package.

## Authorization and audit

### Authorization

Because current backend enforcement is role-based and tenant-scoped, this
proposal requires an explicit operator gate.

Recommended v1 path:

- expose this surface only to deployment-scoped console-admin sessions
- do not expose this API to ordinary tenant-admin sessions
- document clearly that this is a deployment-wide operator action

This is intentionally stricter than ordinary settings edits because the blast
radius is deployment-wide.

If Attune later introduces deployment-scoped identities, this surface should
move there.

### Step-up requirements

Backend-enforced recent-auth step-up is required for `update`, `reset`, and
`rollback`. High-risk mutations should require explicit reconfirmation of the
current desired version and a non-empty reason field.

### Audit

Audit must capture:

- action name
- actor identity
- actor tenant context if applicable
- expected version
- new desired version
- reason/comment
- before/after spec
- component apply summary if mutation handler performed local apply

Suggested action names:

- `enrichment_runtime.update`
- `enrichment_runtime.reset`
- `enrichment_runtime.rollback`

Failed validation attempts should also be audit-visible.

Audit and history integrity rules:

- current-row mutation
- immutable history append
- audit-event persistence

must happen in one DB transaction. No post-commit best-effort audit is
acceptable for deployment runtime mutations.

Recommended audit fields:

- actor type/id
- tenant context if any
- session/request id
- source IP
- user agent
- previous version
- resulting version
- mutation type
- risk classification
- rollback lineage if applicable

## Console UX

This is a high-blast-radius operator surface. The UI must behave like one.

### IA

Do not merge this into the current tenant classification form.

Add a distinct Settings section or operator panel labeled as:

- `Deployment Runtime`
  or
- `Enrichment Runtime`

with clear deployment-wide copy.

### Layout

1. **Desired policy**
   - editable form
   - bootstrap defaults reference
   - reset controls
2. **Cluster status**
   - desired version
   - stale instance count
   - degraded instance count
3. **Instances**
   - instance id
   - last seen
   - runner status
   - limiter status
   - queue depth / target / effective capacity
   - in-flight
4. **History**
   - recent revisions
   - rollback action

### Guardrails

- inline validation
- recommended ranges
- confirmation dialog for high-risk changes
- explicit “deployment-wide impact” copy
- structured reason field required on update
- recent-auth requirement surfaced in UX, with backend enforcement

### Conflict UX

On `409 CONFLICT`:

- show banner that config changed elsewhere
- refresh to latest spec/revision
- preserve unsaved local edits separately
- let operator review and resubmit

### Partial-apply UX

When some instances or components lag:

- show warning banner
- show per-instance row details
- allow manual refresh
- allow rollback from history
- allow promotion of `last_known_good_version`

## Industry benchmarking

Reviewed 12 mature systems with official documentation, focusing on runtime
configuration, operator control planes, backpressure, and multi-instance state
convergence.

| System | Verified pattern | Takeaway for Attune |
| --- | --- | --- |
| Envoy Runtime | layered runtime config and runtime overrides | separate defaults, desired overrides, and effective runtime |
| Kong Rate Limiting | local and shared policy modes are explicit | distinguish local enforcement from shared policy intent |
| Apache APISIX `limit-req` | live-editable traffic controls with clear knobs | expose rate and burst as first-class typed fields |
| Cloudflare Rate Limiting Rules | UI + API + reusable policy abstractions | operator surfaces need both policy editing and status clarity |
| NGINX Plus | live runtime plus shared-state evolution | keep local apply and cluster convergence as distinct concerns |
| AWS AppConfig | validation, staged runtime config, rollback discipline | runtime config is a product surface, not just a storage trick |
| LaunchDarkly | versioned desired state, audit, environment scope | conflict handling and provenance matter |
| Unleash | lifecycle and stale-state discipline | configuration needs ownership and cleanup paths |
| Temporal Worker Tuners | concurrency is a control-plane concern | runtime slot policy and task truth are distinct |
| Celery Remote Control | live worker mutation patterns | hot reconfiguration should be explicit and observable |
| RabbitMQ Runtime Parameters | runtime state belongs outside static files in some cases | DB-backed desired policy is a valid pattern |
| RabbitMQ Consumer Prefetch | backpressure before execution is essential | queue capacity and worker concurrency are separate knobs |

### Repeating patterns

1. desired state and live state are separate
2. local enforcement and cluster policy are separate
3. operator mutation requires validation, versioning, and audit
4. conflict and rollback are part of the feature, not polish
5. throughput and concurrency controls stay independent

## Alternatives considered

### Keep YAML-only and require restart

Rejected. Too slow operationally and fails the user requirement.

### Store runtime policy under tenant `enrich-config`

Rejected. Wrong ownership and wrong scope.

### Build a generic config platform first

Rejected. Too much abstraction and poor fit for repo style.

### Use a generic `runtime_config(key, jsonb)` table

Rejected for v1. It creates a quasi-platform and muddies typed ownership.

### Automatic rollback on partial apply failure

Rejected. More complex failure semantics with limited operator benefit.

## Risks / tradeoffs

### Current auth model remains a compromise

Attune does not yet have a true deployment-scoped identity plane.

Mitigation:

- restrict to admin-owned private deployments in v1
- call the compromise out explicitly

### Queue broker refactor is non-trivial

It is the safest path, but it is more work than a mutable buffered channel.

Mitigation:

- design it as a focused internal abstraction
- add race-heavy tests

### Multi-instance convergence is eventual, not atomic

This is acceptable if visibility is strong.

Mitigation:

- per-instance status
- short poll interval
- future `LISTEN/NOTIFY`

## Implementation plan

1. Finalize proposal scope, ownership, and state model.
2. Add proto contract with distinct spec/revision/status types.
3. Add typed DB tables for desired policy, history, and instance status.
4. Add repo/service layer for policy load, CAS update, reset, rollback, and
   reconcile.
5. Refactor runner to a stable broker model with resize semantics.
6. Refactor limiter to generation-based live reconfiguration.
7. Add instance reconciler loop and status heartbeats.
8. Add Console handlers and admin-only authorization path.
9. Add Console operator page with conflict handling, history, and drift UI.
10. Add metrics, audit, and dashboards.

## Verification

### Backend unit tests

- validation including `NaN` / `Inf`
- CAS conflict behavior
- reset and rollback semantics
- boot fallback on invalid desired policy

### Concurrency and race tests

- concurrent submit during queue grow
- concurrent submit during queue shrink
- shrink below depth with deferred enforcement
- shutdown racing with resize
- limiter generation swap during active traffic
- reconcile retry after partial component failure

### Handler tests

- admin-only authorization
- stale-write `409`
- reset-one-field
- reset-all-fields
- rollback-to-prior-version
- audit payload contents

### Frontend tests

- deployment-wide warning copy
- high-risk confirmation
- conflict banner + refresh flow
- degraded instance rendering
- per-instance status table
- rollback flow

### Integration tests

- save policy, observe same-instance live apply
- observe peer convergence via poller
- lower QPS, confirm new requests use new limiter generation
- shrink queue under load, confirm deferred enforcement without job loss
- boot with invalid desired policy, confirm degraded status and operator recovery

## References

- [Envoy runtime documentation](https://www.envoyproxy.io/docs/envoy/latest/configuration/operations/runtime)
- [Kong rate limiting plugin docs](https://developer.konghq.com/plugins/rate-limiting/)
- [Apache APISIX `limit-req` docs](https://apisix.apache.org/docs/apisix/plugins/limit-req/)
- [Cloudflare rate limiting rules docs](https://developers.cloudflare.com/waf/rate-limiting-rules/)
- [NGINX runtime state sharing docs](https://docs.nginx.com/nginx/admin-guide/high-availability/zone_sync/)
- [AWS AppConfig overview](https://docs.aws.amazon.com/appconfig/latest/userguide/what-is-appconfig.html)
- [LaunchDarkly targeting docs](https://launchdarkly.com/docs/home/flags/target)
- [Unleash feature flag concepts](https://docs.getunleash.io/concepts/feature-flags)
- [Temporal worker performance docs](https://docs.temporal.io/develop/worker-performance)
- [Celery workers guide](https://docs.celeryq.dev/en/stable/userguide/workers.html)
- [RabbitMQ runtime parameters docs](https://www.rabbitmq.com/docs/parameters)
- [RabbitMQ consumer prefetch docs](https://www.rabbitmq.com/docs/consumer-prefetch)
