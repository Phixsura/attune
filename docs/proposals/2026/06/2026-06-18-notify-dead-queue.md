# Notify Outbox Dead-Queue: Operator Surface & Manual Retry

| | |
|---|---|
| **Issue** | #33 |
| **Status** | Accepted |
| **Started** | 2026-06-18 |
| **Related** | #39 (audit log — manual retries are audited), #34 (future outbound adapter SDK / generic alerts), #66 (inbound framework / outbound channels the outbox delivers through) |

---

## Problem

The durable retry / dead-letter **engine** already exists, but operators have no
way to see or act on dead deliveries without querying Postgres directly.

**Evidence from codebase (current `main`):**

1. **Storage + worker are complete.** `notify_outbox`
   (migrations/005_notify_outbox.sql:14-45) carries `status ∈
   {pending,delivered,failed,dead}`, `attempts`, `next_retry_at`,
   `last_error`, `dead_reason`, `claimed_at`. `OutboxRepo`
   (repo/outbox/outbox.go) claims rows `FOR UPDATE SKIP LOCKED`
   (ClaimBatch), schedules retries (MarkFailed), and writes terminal rows
   (MarkDead). The worker (service/outbox/outbox_worker.go:182-228) caps at
   `maxAttempts=5` with a `30s / 2m / 10m / 1h → dead` backoff table.

2. **No read surface.** `internal/handlers/console/` has **no** outbox
   handler; `proto/attune/v1/` has **no** outbox contract. The only way to
   inspect dead rows is direct SQL.

3. **No manual retry.** There is no operator-triggered path to re-arm a dead
   row. `PruneStalePending` (repo/outbox/outbox.go:206) is a one-shot ops
   *cleanup*, not a per-row retry.

4. **Failure reason is unstructured.** The worker classifies failures only as
   `terminal` vs `retryable` (outbox_worker.go:144-156). The upstream HTTP
   status is available in the transport (`transport.go:146`,
   `check(ctx, resp.StatusCode, body)`) but is **discarded** into a flat error
   string — operators cannot filter/triage by "all the 5xx" vs "all the DNS
   failures".

**Impact:** dead deliveries are invisible without DB access; recovering a
customer's missed webhook after they fixed their endpoint requires an engineer
with a `psql` prompt; there is no audit trail of who re-sent what.

---

## Goals / Non-goals

### Goals

| Category | Goal |
|---|---|
| **Visibility** | Tenant-scoped list of `dead` + in-flight `failed` deliveries with structured failure reason, HTTP status, attempts, and timestamps |
| **Manual retry** | Operator can re-arm a single dead/failed row (retry-in-place); the worker redelivers on its next poll |
| **Structured reason** | Persist a `failure_kind` enum (`http_4xx/http_5xx/timeout/dns/connection/tls/terminal/other`) + separate `http_status` |
| **Safety** | Tenant isolation (opaque 404); concurrency-safe retry (no double-deliver while a worker holds the row → 409); every retry audited |
| **Observability** | `attune_outbox_dead_rows` gauge for alerting on DLQ depth |
| **Console** | Standalone "Dead deliveries" view: filter by status, per-row retry |

### Non-goals

| Out of scope | Rationale |
|---|---|
| Physical "move-back-to-source" redrive | attune's outbox is already retry-in-place; no separate source queue exists (see Research) |
| Bulk / time-window redrive | v1 is single-row; bulk + throttling is a clean follow-up (Research §bulk) |
| Per-attempt history table (`notify_outbox_attempts`) | Death history is preserved via the audit `before` snapshot; a full attempt table is larger scope than #33 |
| Failure-reason *aggregation* into lifecycle "Issues" (Hookdeck-style) | Strong v2 direction; v1 ships the flat list first |
| Editing the payload before resend | Out of scope; we replay the exact stored envelope |

---

## Industry Research Summary

Surveyed leading DLQ / failed-delivery operator surfaces (primary vendor docs,
adversarially verified). Webhook-delivery platforms are the closest analog to
attune's notify outbox.

| System | Retry model | List filter | Retry endpoint | Notable |
|---|---|---|---|---|
| **GitHub webhooks** | retry-in-place; `POST .../deliveries/{id}/attempts` → **202** (new attempt, original record kept) | `GET .../deliveries?status=failure` (success=2xx-3xx, failure=4xx-5xx) | per-delivery `/attempts` | preserves request+response (headers+body); ~3-day retention |
| **Svix** | "resend" same message to same endpoint (new attempt) | App Portal failed view | single + bulk "Recover Failed Messages" (time window) | message-anchored "replay since" |
| **Hookdeck** | in-place; event ID persists, attempt count `1→2`, trigger `INITIAL→MANUAL` | Issues API `GET /2025-07-01/issues` | single + `POST .../bulk/requests/retry` | auto-aggregates failures into lifecycle "Issues"; ≤50 auto-retries, unlimited manual |
| **Stripe** | consumer re-processes; at-least-once | `GET /v1/events?delivery_success=false` | Dashboard resend | idempotency is the consumer's job (event id) |
| **Azure Service Bus** | move-back-to-source; DLQ is a real sub-queue | Service Bus Explorer | resubmit to source | **no auto-redrive, no auto-cleanup → unbounded growth (anti-pattern)** |

**Patterns adopted:**

- **Retry-in-place over physical redrive** — strongest cross-system pattern
  (GitHub/Svix/Hookdeck all do it). attune already matches this; we keep it.
- **Preserve the original payload** — it is the replay source-of-truth. attune
  already固化s the envelope at insert (005_notify_outbox.sql:8-10) ✅.
- **Structured failure reason** — separate transport error (DNS/timeout/TLS) from
  HTTP `status_code`; do not collapse "no response" into an HTTP field
  (Service Bus `DeadLetterReason` enum; Hookdeck ~12 error codes).
- **`POST .../retry` returns 202 (async)** — retry re-arms state; actual delivery
  is the worker's next poll. 202 ("accepted") is more honest than 200.
- **Server-side failed-only filter** — GitHub `?status=`.

**Anti-patterns avoided:** losing the payload (we keep it); redrive without
throttling / retry storms (single-row v1; bulk deferred *with* throttling);
no audit trail (every retry → `outbox.retry` audit row); unbounded DLQ growth
(existing `PruneStalePending` retention + a depth metric for alerting).

---

## Proposal

### Data flow

```
Console "Dead deliveries" view
  → GET  /fb/v1/console/outbox/deliveries?status=dead,failed   (proto contract)
  → POST /fb/v1/console/outbox/{id}/retry
      → console/outbox handler        (tenant isolation + audit)
        → OutboxRepo.ListByStatus / RetryOne
Worker failure path (structured reason):
  notify.Transport.Send → *notify.DeliveryError{Kind,HTTPStatus}
    → OutboxWorker classify → OutboxRepo.MarkFailed/MarkDead (persist kind+status)
```

### Schema — `migrations/051_notify_outbox_dead_queue.sql`

Additive, idempotent. The existing `(tenant_id, status)` index serves the list
query's filter; the `ORDER BY id DESC` adds a small in-memory sort step, which is
negligible for a bounded dead queue (alerting keeps depth low) — no new index in
v1, revisit if dead-queue depth grows.

```sql
ALTER TABLE notify_outbox
  ADD COLUMN IF NOT EXISTS failure_kind         TEXT,       -- structured reason enum
  ADD COLUMN IF NOT EXISTS http_status          SMALLINT,   -- upstream HTTP code, NULL if no response
  ADD COLUMN IF NOT EXISTS last_manual_retry_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS retried_by           TEXT,       -- actor user id of last manual retry
  ADD COLUMN IF NOT EXISTS manual_retry_count   SMALLINT NOT NULL DEFAULT 0;

ALTER TABLE notify_outbox
  ADD CONSTRAINT notify_outbox_failure_kind_chk
  CHECK (failure_kind IS NULL OR failure_kind IN
    ('http_4xx','http_5xx','timeout','dns','connection','tls','terminal','other'));
```

### notify layer — typed delivery error (the structured-reason root)

`notify` owns transport, so it owns classification. It declares the error type;
the `service` worker maps it to repo calls (layering preserved — `notify` never
imports `service`/`repo`).

```go
// internal/notify/delivery_error.go
type FailureKind string
const (
  KindHTTP4xx FailureKind = "http_4xx"   // terminal client error
  KindHTTP5xx FailureKind = "http_5xx"
  KindTimeout FailureKind = "timeout"
  KindDNS     FailureKind = "dns"
  KindConn    FailureKind = "connection" // refused / reset
  KindTLS     FailureKind = "tls"
  KindTerminal FailureKind = "terminal"  // ErrTerminal not tied to a status (bad payload, disabled, no channel)
  KindOther   FailureKind = "other"
)
type DeliveryError struct { Kind FailureKind; HTTPStatus int; Err error }
func (e *DeliveryError) Error() string { return e.Err.Error() }
func (e *DeliveryError) Unwrap() error { return e.Err }      // preserves errors.Is(.., ErrTerminal)
func Classify(err error, status int) *DeliveryError          // net/url/tls inspection
```

- `Transport.attempt` carries `resp.StatusCode`; `Transport.Send` wraps the
  `httpClient.Do` net error (and the checker result) through `Classify`.
- `OutboxWorker` reads it via `errors.As(err, &de)` and threads
  `de.Kind` / `de.HTTPStatus` into `MarkFailed` / `MarkDead`.
- `NotifyFailuresTotal`'s `reason` label is intentionally left as
  `terminal|retryable` (not refined to the kind) to avoid label-cardinality
  churn and breaking existing dashboard queries; the structured `kind` is
  persisted in `failure_kind` and logged. No new notify metric is added here.

### repo — `internal/repo/outbox/outbox.go`

```go
// MarkFailed/MarkDead gain (kind notify-agnostic string, httpStatus int) and persist them.
func (r *OutboxRepo) MarkFailed(ctx, id int64, errMsg, failureKind string, httpStatus int, nextDelay time.Duration) error
func (r *OutboxRepo) MarkDead(ctx, id int64, reason, failureKind string, httpStatus int) error

// New read + retry surface (all tenant-scoped):
func (r *OutboxRepo) ListByStatus(ctx, tenantID string, statuses []string, limit int, beforeID int64) ([]OutboxRow, error)
func (r *OutboxRepo) RetryOne(ctx, tenantID string, id int64, actor string) (RetryOutcome, error)
func (r *OutboxRepo) DeadCount(ctx) (int64, error)   // feeds the gauge
```

`OutboxRow` grows display fields: `DeadReason, FailureKind, HTTPStatus,
NextRetryAt, CreatedAt, DeliveredAt, LastManualRetryAt, ManualRetryCount`.

**RetryOne — concurrency-safe, retry-in-place:**

```sql
UPDATE notify_outbox
   SET status='pending', attempts=0, next_retry_at=NOW(), claimed_at=NULL,
       last_error=NULL, dead_reason=NULL, failure_kind=NULL, http_status=NULL,
       last_manual_retry_at=NOW(), retried_by=$3, manual_retry_count=manual_retry_count+1
 WHERE id=$1 AND tenant_id=$2 AND status IN ('dead','failed') AND claimed_at IS NULL
 RETURNING id;
```

`RetryOutcome` distinguishes the 0-row cases for the handler: a follow-up
`SELECT status, claimed_at` decides **404** (no row / wrong tenant) vs **409**
(row exists but `delivered`/`pending`, or `claimed_at` set = worker in-flight).
The death snapshot (`attempts/dead_reason/last_error/failure_kind/http_status`)
is read *before* the reset and returned so the handler can write it as the
audit `before`.

### proto — `proto/attune/v1/outbox.proto`

```proto
service NotifyOutboxService {
  rpc ListDeliveries(ListDeliveriesRequest) returns (ListDeliveriesResponse) {
    option (google.api.http) = {get: "/fb/v1/console/outbox/deliveries"};
  }
  rpc RetryDelivery(RetryDeliveryRequest) returns (RetryDeliveryResponse) {
    option (google.api.http) = {post: "/fb/v1/console/outbox/{id}/retry" body: "*"};
  }
}
enum OutboxFailureKind { OUTBOX_FAILURE_KIND_UNSPECIFIED = 0; OUTBOX_FAILURE_KIND_HTTP_4XX = 1; /* … */ }
message OutboxDelivery { /* id, feedback_id, destination_type/target, audience, status,
  attempts, failure_kind, http_status, last_error, dead_reason, *_at timestamps, retried_by */ }
message ListDeliveriesRequest  { repeated string status = 1; int32 limit = 2; int64 before_id = 3; }
message ListDeliveriesResponse { repeated OutboxDelivery deliveries = 1; int64 next_before_id = 2; }
message RetryDeliveryRequest   { int64 id = 1; }
message RetryDeliveryResponse  { OutboxDelivery delivery = 1; }
```

`make proto` regenerates Go / TS / OpenAPI; committed alongside.

### handlers — `internal/handlers/console/outbox/`

Mirrors `console/tag` (handler + audit recorder). Registered in
`console/router.go` via `dispatcher.Bind` (`dispatcher.Query` for list,
`dispatcher.JSON` + `{id}` for retry), `dispatcher.WithAuth(session.FromContext)`.

- `List`: `auth.TenantID` → `repo.ListByStatus`; default `status=[dead]` when
  unspecified; cap `limit` (e.g. ≤200).
- `Retry`: `repo.RetryOne` → map `RetryOutcome` to **202** (re-armed) / **404**
  (opaque) / **409** (in-flight or not-retryable). The `outbox.retry` audit
  (`before`=death snapshot, `after`=re-armed state) is written **inside
  `RetryOne`'s transaction** via `auditlog.Service.RecordTx` — the re-arm and its
  audit commit or roll back together, so a retry can never land without an audit
  trail (and an audit failure can't leave a half-applied 500). Migration 052
  registers the `outbox.retry` action in the audit allow-list + `chk_audit_action_value`.
  Error codes come from the `ErrorCode` enum (lint-errorcode).

### metric

`attune_outbox_dead_rows` gauge, sampled in the existing collector at
`cmd/attune/setup.go:143-148` (next to `OutboxLagSeconds`) via
`OutboxRepo.DeadCount`. Global (no label), matching `OutboxLagSeconds`.

### console — `console/src/features/outbox-dead/`

Mirrors `notify-targets` layout: `api/{list-deliveries,retry-delivery}.ts`
(generated TS client), `components/{dead-deliveries-page,table}.tsx`. Standalone
tenant-wide view; columns: destination, audience, attempts, `failure_kind`,
`http_status`, `dead_reason`, timestamps; status filter; per-row **Retry** →
202 → refetch + toast. Added to the console route table + nav.

---

## Alternatives considered

| Alternative | Why rejected |
|---|---|
| Pure `attempts=0` reset (issue's literal text) | Erases death history; a retried-then-failed row is indistinguishable from a fresh failure. Audit `before` snapshot solves this for ~free. |
| Full `notify_outbox_attempts` history table | Faithful to GitHub/Hookdeck but larger than #33; audit snapshot covers the need. Re-evaluate with v2 aggregation. |
| Classify failures by parsing the error string in the worker | Brittle; status code already structurally available in transport. Typed `DeliveryError` is the clean carrier. |
| Dead-only list | User chose to also surface in-flight `failed`; handled with `claimed_at IS NULL` guard + 409. |
| Per-tenant gauge label | Cardinality risk; matches `OutboxLagSeconds` (global) for v1. |
| Bulk redrive now | Needs throttling to avoid retry storms (Research); ship single-row first. |

## Risks / tradeoffs

| Risk | Severity | Mitigation |
|---|---|---|
| Double-delivery if retry races the worker | High | `RetryOne` UPDATE guarded by `claimed_at IS NULL`; in-flight → 409 |
| Touching the well-tested `notify.Transport` | Medium | `DeliveryError.Unwrap` keeps `errors.Is(.., ErrTerminal)`; extend transport tests for each kind |
| `MarkFailed/MarkDead` signature change ripples to callers/tests | Medium | Single worker call-site each; update tests in same change |
| Retry storm via repeated manual retries | Low | v1 single-row + manual; `manual_retry_count` is visible; bulk deferred |
| DLQ unbounded growth | Low | `PruneStalePending` retention + `attune_outbox_dead_rows` alerting |

## Implementation plan

| Phase | Work | Location |
|---|---|---|
| 1 | Migration 051 (columns + CHECK) | `migrations/051_*.sql` |
| 2 | `DeliveryError` + `Classify` + transport wiring (+tests) | `internal/notify/` |
| 3 | Worker classify → `MarkFailed/MarkDead` new args (+tests) | `internal/service/outbox/` |
| 4 | Repo: `ListByStatus`/`RetryOne`/`DeadCount`, row fields, persist kind (+tests) | `internal/repo/outbox/` |
| 5 | `outbox.proto` + `make proto` (commit generated) | `proto/`, `internal/proto`, `console/src/proto`, `docs/openapi` |
| 6 | Handler List/Retry + audit + router wiring (+tests) | `internal/handlers/console/outbox/`, `router.go` |
| 7 | Metric + sampler wiring | `internal/infra/metrics`, `cmd/attune/setup.go` |
| 8 | Console feature + tests | `console/src/features/outbox-dead/` |
| 9 | CHANGELOG `### Added`; sync #33 scope | `CHANGELOG.md`, issue #33 |

## Verification

| Check | Method |
|---|---|
| Repo retry reset / in-flight guard / tenant isolation | `go test ./internal/repo/outbox/...` + integration (`test/integration/postgres/`) |
| Worker error → kind/http_status classification | `go test ./internal/service/outbox/...`, `./internal/notify/...` |
| Handler 202 / 404 / 409 / audit write | `go test ./internal/handlers/console/outbox/...` |
| Proto in sync | `buf generate && git diff --exit-code` |
| Console render + retry mutation | `pnpm vitest run` |
| Gates | `go vet`, `golangci-lint`, `lizard -C 15 -T nloc=100`, lint-rawptr/slog/errorcode, `pnpm biome check`, `pnpm arch` |

## References

- Issue #33; CLAUDE.md §5 (layering), §7 (observability), §11 (proto contract)
- Current code: `repo/outbox/outbox.go`, `service/outbox/outbox_worker.go`, `notify/transport.go:94-147`, `cmd/attune/setup.go:143`
- House patterns mirrored: `handlers/console/tag/` (handler+audit), `service/auditlog/`, `notify-targets` console feature
- Industry: GitHub REST webhooks (deliveries/attempts, `?status=`), Svix replay, Hookdeck Issues/retries, Stripe undelivered events, Azure Service Bus DLQ
