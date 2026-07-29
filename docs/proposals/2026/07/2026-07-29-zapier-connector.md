# Zapier connector: unified webhook subscriptions + automation API surface

| | |
|---|---|
| **Issue** | [#234](https://github.com/Phixsura/attune/issues/234) |
| **Status** | Implemented |
| **Started** | 2026-07-29 |
| **Related** | #202 (platform pillar), #251/#253 (external sync), close-the-loop request notifications (2026-07-16) |

## Problem

Productboard, Canny, and most feedback tools lean on Zapier for long-tail
integrations. attune has no automation surface a Zapier app could be built on:

1. **No subscription API.** Outbound webhooks exist (`tenant_notify_targets` +
   `notify_outbox`), but targets are console-session-only, capped at one per
   `(tenant, destination_type, audience)`, and carry no event-type filter. A
   Zapier REST-hook trigger needs *N* independent subscriptions (one per Zap),
   created and deleted over API-key auth.
2. **Fragmented events.** The outbox emits a single implicit event
   (`feedback.enriched`); request status changes flow through a separate
   `customer_request_notification_events` pipeline (console-configured,
   end-user-focused); "new customer request" is not evented at all.
3. **No API-key surface for customer requests.** Create/update/status/notes are
   console-session routes only — no `requests:*` scope exists. Zapier actions
   ("update request workflow", "add note") have nothing to call.

## Goals

- Zapier can trigger on: new feedback, urgent feedback, new customer request,
  request status change — **instant** (REST hooks), with the polling fallback
  Zapier requires for public apps.
- Zapier actions: create feedback, update request workflow/status, add tag,
  add note — all scoped by API key and audited.
- Stable error contract (401 / 403 / 429 + `Retry-After`; no 200-with-error).
- Connector definition validated against a local/mock API.

## Non-goals

- Publishing/listing the app on the Zapier platform (needs a Zapier account,
  review cycle — follow-up).
- OAuth2 authorization-code flow (API-key auth is publishable on Zapier as
  long as keys are self-serve; see benchmark).
- Migrating existing `tenant_notify_targets` consumers onto the new
  subscription layer (the old surface stays; convergence is a follow-up).
- Make/n8n connector definitions (the API surface this adds is generic enough
  to serve them later).

## Proposal

### 1. Unified webhook subscription layer

New table `webhook_subscriptions` (migration 123):

```sql
CREATE TABLE webhook_subscriptions (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id      TEXT        NOT NULL,
  target_url     TEXT        NOT NULL,
  secret         TEXT        NOT NULL,           -- per-subscription HMAC key
  event_types    TEXT[]      NOT NULL,           -- subset of the event vocabulary
  status         TEXT        NOT NULL DEFAULT 'active',  -- active | disabled
  disabled_reason TEXT,                          -- e.g. 'gone' (HTTP 410), 'manual'
  consumer       TEXT        NOT NULL DEFAULT 'generic', -- zapier | generic
  created_by_key_id UUID,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

One subscription per Zap — no uniqueness constraint on `(tenant, url)` beyond
the primary key. REST-hook lifecycle: subscribe stores the Zapier `targetUrl`
and returns `{id}`; unsubscribe deletes by id; a delivery answered with HTTP
**410 Gone** auto-disables the subscription (`disabled_reason='gone'`) and
stops all sends, per Zapier's contract.

### 2. Event vocabulary (append-only)

Dot-namespaced `entity.verb` tokens, same append-only discipline as
`user_feedback.source` (CLAUDE.md §5):

| Event | Emission point |
|---|---|
| `feedback.created` | enrich completion — same transaction that flips `enrichment_status='done'` (existing `enricher_outbox.go` plan builder, extended to fan out over matching subscriptions) |
| `feedback.urgent` | same point, additionally matched when `Snapshot.IsUrgent` (the existing `radar` predicate) |
| `request.created` | `service/customerrequest.Create` — same transaction |
| `request.status_changed` | `service/customerrequest.Update` where `before.Status != after.Status` — same site that feeds the close-the-loop sink, same transaction |

Delivery reuses `notify_outbox` + the existing worker: new
`destination_type='subscription-webhook'` with `destination_target` = the
subscription id; envelope v2 with `event_type` set per event; HMAC-SHA256
signature (`X-Attune-Signature`, v2-content-hash) with the **subscription's**
secret; `X-Attune-Delivery-Id` for consumer dedup; existing backoff/dead-queue
semantics. The worker resolves the subscription at send time and skips
disabled rows (a queued envelope may outlive its subscription — same
"validate at write, opaque at delivery" rule the source vocabulary follows).

### 3. API surface (proto-first, `X-API-Key`)

| Route | Scope | Purpose |
|---|---|---|
| `POST /v1/hooks` → 201 `{id}` | `hooks:manage` (explicit) | Zapier subscribe |
| `GET /v1/hooks` | `hooks:manage` | list subscriptions |
| `DELETE /v1/hooks/{id}` | `hooks:manage` | Zapier unsubscribe |
| `GET /v1/hooks/samples/{event_type}` | `hooks:manage` | performList: reverse-chron array, schema-identical to the live webhook payload (Zapier checks D012/T004-T006) |
| `GET /v1/requests`, `POST /v1/requests` | `requests:read` / `requests:write` (explicit) | list/create customer requests |
| `PATCH /v1/requests/{id}` | `requests:write` | update incl. `status` (`open\|planned\|in_progress\|shipped\|cancelled`) |
| `POST /v1/requests/{id}/notes` | `requests:write` | add note; `visibility: internal\|public` — internal → `customer_request_notes`, public → portal comment pipeline (existing moderation applies) |
| `POST /v1/feedback/{id}/tags` | `tags:write` | tag assignment (today console-only) |
| `POST /v1/feedback/ingest` (existing) | `ingest:write` | create feedback |
| `GET /v1/auth/verify` (existing, + tenant display name in response) | any key | Zapier auth test + connection label |

New scopes `hooks:manage`, `requests:read`, `requests:write` join
`domain/scope.go` with the write→read hierarchy; all three use
`RequireExplicitScope` (no legacy-key implicit grant). Request-surface
handlers delegate to the existing `service/customerrequest` functions —
no new business logic, only an API-key-authenticated binding.

Proto: new `webhook_subscription.proto` (WebhookSubscriptionService); additive
HTTP bindings for the request surface (existing `customer_request.proto` rpcs
stay untouched; new rpcs only where the console shape doesn't fit — additive
per the proto policy).

### 4. Error contract & audit

- 401 invalid/revoked key → Zapier prompts reconnect; 403 `missing scope: X`;
  429 + `Retry-After` from the existing per-key limiter; 4xx messages specific
  and ≤ 250 chars; `ErrorResponse.code` from the enum. Never 200-with-error.
- New audit actions — added to **both** `validActions` and the
  `chk_audit_action_value` migration (two-layer allow-list):
  `webhook_subscription.create`, `webhook_subscription.delete`,
  plus the existing `customer_request.*` / `tag.*` actions now reachable via
  API key (recorded with the key id as actor).

### 5. Zapier integration definition (in-repo)

`integrations/zapier/` — Zapier Platform CLI project (own `package.json`,
excluded from the main build): 4 triggers (REST hook + performList), 4 actions,
API-key auth with `/v1/auth/verify` test + connection label, static sample
data matching the envelope schema, and integration tests running against a
local attune (or mock) API. Publishing is out of scope.

### 6. Benchmark validation (72-product survey)

`zapier-connector-benchmark.md` (+ raw corpus `.json`) surveyed 72 products
across 9 categories: 64 ship Zapier apps; the instant-REST-hook +
`{url, events[]}` subscription + HMAC-signature + dot-namespaced-event
pattern this design implements is the corpus-dominant shape (57/72 filter
by event array, 60/72 sign, all feedback-category leaders are all-instant).
**No ADOPT-NOW gaps** — attune's per-subscription secrets exceed the
shared-secret norm. Two DEFERs tracked as follow-ups:

- `POST /v1/hooks/{id}/ping` test-delivery endpoint (GitHub/Intercom/ClickUp
  pattern; the samples endpoint covers the Zap-editor need today).
- Timestamp folded into the webhook signature for replay resistance
  (Canny/Zendesk/Linear pattern) — a cross-cutting change to the shared
  `v2-content-hash` signing scheme, out of scope for this issue.

## Alternatives considered

1. **Zapier-only hooks table** (`zapier_hooks`) — same mechanics, non-general
   subscription model; a second automation platform (Make/n8n) would force a
   rebuild or a rename. Rejected: cost delta to the unified layer is small.
2. **Polling-only triggers** — cheapest (no subscription API), but 1-15 min
   latency, weaker than every feedback-category competitor (Canny is
   all-instant), and the polling endpoints are required anyway as performList.
   Rejected as the primary mechanism; shipped as the fallback.
3. **Extend `tenant_notify_targets`** with event masks + multi-row support —
   touches the enricher fan-out, the console CRUD, and the uniqueness
   constraint consumed by existing tenants; higher migration risk than a new
   table, and conflates operator-configured ops channels with
   consumer-created automation hooks. Rejected.
4. **Reuse `customer_notification_webhook_targets`** (close-the-loop) — has
   `event_mask`, but is console-configured, end-user-notification-scoped, and
   coupled to public-update policy gating. Wrong ownership model for
   API-key-created hooks. Rejected; its `event_mask` design is the precedent
   the new table follows.

## Risks / tradeoffs

- **Event fan-out volume**: each enriched feedback row now also fans out over
  subscriptions. Bounded by a per-tenant cap of 25 active subscriptions
  (compile-time constant) and the existing outbox backpressure.
- **Envelope schema is a public contract**: performList and live payloads must
  stay schema-identical (Zapier T004-T006). A golden test pins the envelope;
  changes are additive-only.
- **Two webhook systems in flight** (`tenant_notify_targets` vs
  `webhook_subscriptions`): documented split — ops channels vs automation
  consumers; convergence tracked as a follow-up issue.
- **Public notes via automation**: moderated by the existing pipeline, but a
  misconfigured Zap could flood the portal queue; per-key rate limits apply.

## Implementation plan

1. Migration 123 (`webhook_subscriptions` + audit CHECK update) + repo layer.
2. Domain scopes + `webhook_subscription.proto` + `make proto`.
3. Subscription CRUD handlers (`/v1/hooks*`) + performList endpoint.
4. Event emission: `request.created`, `request.status_changed` (same-tx
   enqueue), subscription fan-out in `enricher_outbox` plan builder,
   `subscription-webhook` send path in the outbox worker + 410 handling.
5. Request surface over API key (`/v1/requests*`, notes, tag assignment).
6. SDK (Go/TS) wrappers; audit wiring.
7. `integrations/zapier/` CLI project + integration tests.
8. Docs: connector auth guide + sample recipes; CHANGELOG.

## Verification

- Unit + PG integration tests: subscription CRUD, fan-out matching,
  410 auto-disable, same-tx emission (rollback drops the event), envelope ↔
  performList golden.
- `make ci-check` green; coverage non-regression.
- Real-LLM e2e: local stack, ingest → enrich → subscription receives
  `feedback.created` and `feedback.urgent` deliveries with valid signatures;
  Zapier CLI integration tests against the local API.

## References

- Benchmark: `zapier-connector-benchmark.md` + `.json` (this directory)
- Zapier REST hooks / publishing checks: docs.zapier.com (D012, D017, T004-T006)
- Close-the-loop events: `2026-07-16-close-the-loop-request-notifications.md`
