# Zapier Connector Implementation Plan (#234)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unified webhook-subscription layer + API-key automation surface so a Zapier app (in-repo, `integrations/zapier/`) gets 4 instant triggers and 4 actions.

**Architecture:** New `webhook_subscriptions` table (one row per Zap, event-type array filter); delivery reuses `notify_outbox` + worker with a new `subscription-webhook` destination type resolved by subscription id; events `feedback.created`/`feedback.urgent` fan out from the enricher plan builder, `request.created`/`request.status_changed` enqueue same-tx in `service/customerrequest`. New scopes `hooks:manage`, `requests:read/write` (explicit-only). Proto-first: new `webhook_subscription.proto`, additive rpcs in `customer_request.proto`.

**Tech Stack:** Go 1.x + pgx/Postgres, chi + dispatcher.Bind proto contract, buf, Zapier Platform CLI (Node 20, `integrations/zapier/`, excluded from main build).

## Global Constraints

- Accepted proposal: `docs/proposals/2026/07/2026-07-29-zapier-connector.md`. Benchmark ADOPT-NOW items (§6) may append small tasks; core architecture is fixed.
- CLAUDE.md gates: CCN ≤ 15, NLOC ≤ 100, ptrext (no bare `&x`/`*p`), logext only, changelog entry required, proto → `make proto` + commit generated output, additive proto only (buf breaking is file-level).
- Event vocabulary is **append-only**: `feedback.created`, `feedback.urgent`, `request.created`, `request.status_changed`. Never rename. Envelope stays version `"2"`; performList payload must be schema-identical to live webhook payload (golden test).
- New scopes use `RequireExplicitScope` (no legacy-key implicit grant).
- Audit: new actions go to BOTH `internal/service/auditlog/actions.go` `validActions` AND the `chk_audit_action_value` CHECK in migration 123.
- Migration number: **123** (verify `ls internal/infra/database/migrations | tail -1` still shows 122 before starting).
- Error contract: 401/403/404/409/429 + `Retry-After`; messages ≤ 250 chars; `ErrorResponse.code` from enum (`scripts/lint-errorcode.sh`); never 200-with-error.
- Commits: Conventional Commits, scope `zapier` / `hooks` / `requests` as fits; run relevant lint/test subset before each commit.

---

### Task 1: Migration 123 + `webhooksub` repo

**Files:**
- Create: `internal/infra/database/migrations/123_webhook_subscriptions.sql`
- Create: `internal/repo/webhooksub/webhooksub.go`
- Test: `internal/repo/webhooksub/webhooksub_integration_test.go` (build tag `integration`, layout per `scripts/lint-integration-layout.sh` — mirror `internal/repo/notifytarget`'s integration test placement)

**Interfaces:**
- Produces: `type Subscription struct { ID uuid.UUID; TenantID string; TargetURL string; Secret string; EventTypes []string; Status string; DisabledReason string; Consumer string; CreatedByKeyID *uuid.UUID; CreatedAt, UpdatedAt time.Time }`
- Produces: `func New(pool *pgxpool.Pool) *Repo`, methods `Insert(ctx, Subscription) (Subscription, error)`, `GetByID(ctx, tenantID string, id uuid.UUID) (*Subscription, error)`, `ListByTenant(ctx, tenantID string) ([]Subscription, error)`, `ListActiveByTenantEvent(ctx, tenantID, eventType string) ([]Subscription, error)`, `ListActiveByTenantEventTx(ctx, tx pgx.Tx, tenantID, eventType string) ([]Subscription, error)`, `Delete(ctx, tenantID string, id uuid.UUID) (bool, error)`, `Disable(ctx, id uuid.UUID, reason string) error`, `CountByTenant(ctx, tenantID string) (int, error)`
- Produces: consts `StatusActive = "active"`, `StatusDisabled = "disabled"`, `ReasonGone = "gone"`, `ReasonManual = "manual"`, `ConsumerZapier = "zapier"`, `ConsumerGeneric = "generic"`; `var ErrSubscriptionNotFound = errors.New("webhook subscription not found")`
- Produces (same migration): `notify_outbox.feedback_id` nullable + `'subscription-webhook'` reachable as `destination_type` (005 has no CHECK on destination_type — verify; if one exists, extend it); audit CHECK gains `webhook_subscription.create`, `webhook_subscription.delete`.

- [ ] **Step 1: Write the migration**

```sql
-- 123_webhook_subscriptions.sql
-- Unified webhook subscription layer for the automation surface (#234).
-- One row per consumer hook (e.g. one Zapier Zap); event_types filters the
-- append-only event vocabulary (feedback.created, feedback.urgent,
-- request.created, request.status_changed).
CREATE TABLE webhook_subscriptions (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id         TEXT        NOT NULL,
  target_url        TEXT        NOT NULL,
  secret            TEXT        NOT NULL,
  event_types       TEXT[]      NOT NULL CHECK (array_length(event_types, 1) >= 1),
  status            TEXT        NOT NULL DEFAULT 'active'
                    CONSTRAINT chk_webhook_sub_status CHECK (status IN ('active', 'disabled')),
  disabled_reason   TEXT,
  consumer          TEXT        NOT NULL DEFAULT 'generic'
                    CONSTRAINT chk_webhook_sub_consumer CHECK (consumer IN ('zapier', 'generic')),
  created_by_key_id UUID,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_webhook_subs_tenant_status ON webhook_subscriptions (tenant_id, status);

-- request.* events carry no feedback row.
ALTER TABLE notify_outbox ALTER COLUMN feedback_id DROP NOT NULL;

-- Two-layer audit allow-list (see attune-audit-action-two-layer-allowlist).
ALTER TABLE tenant_audit_log DROP CONSTRAINT chk_audit_action_value;
ALTER TABLE tenant_audit_log ADD CONSTRAINT chk_audit_action_value CHECK (action IN (
  -- copy the full list from migration 118, then append:
  'webhook_subscription.create',
  'webhook_subscription.delete'
));
```

Copy the existing action list verbatim from `118_cohort_sync_audit_actions.sql` (open it; do not retype). Check `005_notify_outbox.sql` for a `destination_type` CHECK — none is expected, but if present, extend it with `'subscription-webhook'` here.

- [ ] **Step 2: Write failing integration test**

```go
//go:build integration

package webhooksub_test // mirror notifytarget's integration test pattern incl. testdb setup

func TestInsertListDisable(t *testing.T) {
	repo := webhooksub.New(testdb.Pool(t))
	sub, err := repo.Insert(ctx, webhooksub.Subscription{
		TenantID: "t1", TargetURL: "https://hooks.zapier.com/x", Secret: "s3cr3t-16chars-min",
		EventTypes: []string{"feedback.created"}, Consumer: webhooksub.ConsumerZapier,
	})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, sub.ID)
	require.Equal(t, webhooksub.StatusActive, sub.Status)

	active, err := repo.ListActiveByTenantEvent(ctx, "t1", "feedback.created")
	require.NoError(t, err)
	require.Len(t, active, 1)

	// event filter excludes non-subscribed types
	none, err := repo.ListActiveByTenantEvent(ctx, "t1", "request.created")
	require.NoError(t, err)
	require.Empty(t, none)

	require.NoError(t, repo.Disable(ctx, sub.ID, webhooksub.ReasonGone))
	got, err := repo.GetByID(ctx, "t1", sub.ID)
	require.NoError(t, err)
	require.Equal(t, webhooksub.StatusDisabled, got.Status)
	require.Equal(t, "gone", got.DisabledReason)

	// disabled rows drop out of the active list
	active, err = repo.ListActiveByTenantEvent(ctx, "t1", "feedback.created")
	require.NoError(t, err)
	require.Empty(t, active)

	ok, err := repo.Delete(ctx, "t1", sub.ID)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestGetByIDWrongTenant(t *testing.T) { /* insert as t1, GetByID with t2 → ErrSubscriptionNotFound */ }
```

- [ ] **Step 3: Run test, verify it fails** — `make test-integration GOTEST_RUN=TestInsertListDisable` (or the repo's equivalent; check Makefile target syntax first). Expected: compile error (package missing).

- [ ] **Step 4: Implement the repo**

`ListActiveByTenantEvent` filter: `WHERE tenant_id = $1 AND status = 'active' AND $2 = ANY(event_types)`. All queries tenant-scoped except `Disable` (worker path has the id only; id is the PK and unguessable). Follow `notifytarget` package style: `ptrext.Of` construction, `logext` on errors, `pgxutil` helpers where the package uses them.

- [ ] **Step 5: Run tests, verify pass** — integration test green; also `TestSourceVocabulary`-style sanity: run full `go test ./internal/repo/webhooksub/...`.

- [ ] **Step 6: Commit** — `feat(hooks): add webhook_subscriptions table and repo`

---

### Task 2: Domain scopes

**Files:**
- Modify: `internal/domain/scope.go`
- Test: `internal/domain/scope_test.go` (extend existing)

**Interfaces:**
- Produces: `ScopeHooksManage Scope = "hooks:manage"`, `ScopeRequestsRead Scope = "requests:read"`, `ScopeRequestsWrite Scope = "requests:write"`; all three in `AllScopes`; hierarchy `ScopeRequestsWrite → ScopeRequestsRead`. Update the "28 total" comment to 31.

- [ ] **Step 1: Failing test**

```go
func TestNewAutomationScopes(t *testing.T) {
	for _, s := range []domain.Scope{"hooks:manage", "requests:read", "requests:write"} {
		if !s.IsValid() { t.Fatalf("%s should be valid", s) }
	}
	if !domain.HasExplicitScope([]domain.Scope{domain.ScopeRequestsWrite}, domain.ScopeRequestsRead) {
		t.Fatal("requests:write must imply requests:read")
	}
	if domain.HasExplicitScope([]domain.Scope{}, domain.ScopeHooksManage) {
		t.Fatal("legacy empty-scope keys must NOT get hooks:manage")
	}
}
```

- [ ] **Step 2: Run, verify fails** — `go test ./internal/domain/ -run TestNewAutomationScopes` → invalid scope.
- [ ] **Step 3: Implement** (constants + AllScopes + hierarchy entry + comment count).
- [ ] **Step 4: Run, verify passes.** Also run the whole package: existing scope-count assertions may need the 31 update.
- [ ] **Step 5: Commit** — `feat(domain): add hooks:manage and requests:read/write scopes`

---

### Task 3: Proto contract

**Files:**
- Create: `proto/attune/v1/webhook_subscription.proto`
- Modify: `proto/attune/v1/customer_request.proto` (additive only)
- Modify: `proto/attune/v1/api_key.proto` (additive `tenant_display_name` on the auth-verify response)
- Generated: `make proto` output (`internal/proto/**`, `console/src/proto/**`, `docs/openapi/**`)

**Interfaces:**
- Produces: `WebhookSubscriptionService` — `CreateWebhookSubscription (POST /v1/hooks)`, `ListWebhookSubscriptions (GET /v1/hooks)`, `DeleteWebhookSubscription (DELETE /v1/hooks/{id})`, `ListWebhookSamples (GET /v1/hooks/samples/{event_type})`.
- Produces: `CustomerRequestAutomationService` (new service block in customer_request.proto, reusing existing messages where shapes match) — `ListRequestsAutomation (GET /v1/requests)`, `CreateRequestAutomation (POST /v1/requests)`, `UpdateRequestAutomation (PATCH /v1/requests/{id})`, `AddRequestNoteAutomation (POST /v1/requests/{id}/notes)`.

- [ ] **Step 1: Write webhook_subscription.proto**

```proto
syntax = "proto3";
package attune.v1;
import "google/api/annotations.proto";
import "google/api/field_behavior.proto";

// WebhookSubscriptionService manages automation webhook subscriptions
// (e.g. Zapier REST hooks) over API-key auth, scope hooks:manage.
service WebhookSubscriptionService {
  // POST /v1/hooks (201)
  rpc CreateWebhookSubscription(CreateWebhookSubscriptionRequest) returns (WebhookSubscription) {
    option (google.api.http) = { post: "/v1/hooks" body: "*" };
  }
  // GET /v1/hooks
  rpc ListWebhookSubscriptions(ListWebhookSubscriptionsRequest) returns (ListWebhookSubscriptionsResponse) {
    option (google.api.http) = { get: "/v1/hooks" };
  }
  // DELETE /v1/hooks/{id} (204)
  rpc DeleteWebhookSubscription(DeleteWebhookSubscriptionRequest) returns (DeleteWebhookSubscriptionResponse) {
    option (google.api.http) = { delete: "/v1/hooks/{id}" };
  }
  // GET /v1/hooks/samples/{event_type} — Zapier performList
  rpc ListWebhookSamples(ListWebhookSamplesRequest) returns (ListWebhookSamplesResponse) {
    option (google.api.http) = { get: "/v1/hooks/samples/{event_type}" };
  }
}

// Secret is write-only (accepted on create, never returned) — same rule as NotifyTarget.
message WebhookSubscription {
  string id = 1;
  string target_url = 2;
  repeated string event_types = 3; // append-only vocabulary
  string status = 4;               // active | disabled
  string disabled_reason = 5;
  string consumer = 6;             // zapier | generic
  string created_at = 7;           // RFC3339
}
message CreateWebhookSubscriptionRequest {
  string target_url = 1 [(google.api.field_behavior) = REQUIRED];
  repeated string event_types = 2 [(google.api.field_behavior) = REQUIRED];
  string secret = 3;   // optional; server generates when empty
  string consumer = 4; // default "generic"
}
message ListWebhookSubscriptionsRequest {}
message ListWebhookSubscriptionsResponse { repeated WebhookSubscription subscriptions = 1; }
message DeleteWebhookSubscriptionRequest { string id = 1; }
message DeleteWebhookSubscriptionResponse {}
message ListWebhookSamplesRequest { string event_type = 1; }
// Samples are envelope objects, schema-identical to live webhook payloads.
// Returned as raw JSON (google.protobuf.Struct) so the envelope stays the
// single source of truth.
message ListWebhookSamplesResponse { repeated google.protobuf.Struct samples = 1; }
```

(Add `import "google/protobuf/struct.proto";`.)

- [ ] **Step 2: Add CustomerRequestAutomationService to customer_request.proto** — new service block only; reuse existing `CustomerRequest`/`CreateCustomerRequestRequest`-family messages if their field shapes fit the /v1 surface; where console-specific fields (e.g. member ids) don't fit, define new `*Automation*` messages. Add `visibility` (internal|public) field on the automation note request. NEVER touch existing rpcs/messages.
- [ ] **Step 3: api_key.proto** — add `string tenant_display_name = N;` (next free tag) to the auth-verify response message.
- [ ] **Step 4: Generate + gates** — `make proto`, then `buf lint && buf breaking` (CI mirrors this), commit generated Go/TS/OpenAPI. Expected: clean, no drift after commit.
- [ ] **Step 5: Commit** — `feat(proto): webhook subscription service + automation request surface`

---

### Task 4: Envelope: event-type parameter + request envelope + golden test

**Files:**
- Modify: `internal/service/enrich/enricher_outbox.go` (`buildOutboxEnvelope`)
- Create: `internal/domain/automationevent.go` (event-type constants — domain because both enrich and customerrequest services need them)
- Create: `internal/service/customerrequest/request_envelope.go`
- Test: `internal/service/enrich/enricher_outbox_test.go` (extend), `internal/service/customerrequest/request_envelope_test.go`, golden files under each package's `testdata/`

**Interfaces:**
- Produces: `domain.EventFeedbackCreated = "feedback.created"`, `domain.EventFeedbackUrgent = "feedback.urgent"`, `domain.EventRequestCreated = "request.created"`, `domain.EventRequestStatusChanged = "request.status_changed"`, `domain.EventFeedbackEnriched = "feedback.enriched"` (legacy), and `func IsAutomationEvent(s string) bool` (the four new ones only).
- Produces: `buildOutboxEnvelope(s domain.Snapshot, traceID, sourceDisplay, eventType string) ([]byte, error)` — existing call site passes `domain.EventFeedbackEnriched`; behavior for legacy targets is byte-identical.
- Produces: `func BuildRequestEnvelope(d Detail, eventType, previousStatus, traceID string) ([]byte, error)` returning:

```json
{"version":"2","event_type":"request.status_changed","delivered_at":"…RFC3339…","trace_id":"…",
 "request":{"id":"uuid","display_id":"REQ-42","title":"…","description":"…","status":"in_progress",
            "previous_status":"planned","priority":"high","created_at":"…","updated_at":"…"}}
```

`previous_status` omitted (`omitempty`) for `request.created`.

- [ ] **Step 1: Failing tests** — (a) extend the existing envelope test to call with explicit `domain.EventFeedbackEnriched` and assert against the CURRENT golden bytes (proves refactor is behavior-preserving); add a case with `domain.EventFeedbackUrgent` asserting only `event_type` differs. (b) request envelope golden test for both request events. Golden files = the public contract pin (proposal "Risks").
- [ ] **Step 2: Run, verify fail** (signature mismatch compile error, then golden mismatch).
- [ ] **Step 3: Implement** (thread the parameter; struct-tag `previous_status,omitempty`).
- [ ] **Step 4: Run package tests** — `go test ./internal/service/enrich/... ./internal/service/customerrequest/... ./internal/domain/...` green.
- [ ] **Step 5: Commit** — `feat(hooks): event vocabulary + parameterized outbox envelopes`

---

### Task 5: Outbox worker — `subscription-webhook` send path + 410 auto-disable

**Files:**
- Modify: `internal/repo/outbox/outbox.go` (`Insert`: `NULLIF($1, 0)` for feedback_id; `OutboxRow.FeedbackID` stays `int64`, 0 = none; scan with COALESCE)
- Modify: `internal/service/outbox/outbox_worker.go` + `outbox_worker_send.go`
- Create: `internal/service/outbox/subscription_target.go`
- Test: `internal/service/outbox/outbox_worker_test.go` (extend, injected-interface style per attune-coverage-gates), integration test alongside existing outbox worker integration tests

**Interfaces:**
- Consumes: `webhooksub.Repo.GetByID` (Task 1 — worker needs a tenant-free variant: add `GetByIDAny(ctx, id uuid.UUID)` to the repo for the worker's trusted path), `webhooksub.Repo.Disable`.
- Produces: const `DestSubscriptionWebhook = "subscription-webhook"` (in `internal/repo/notifytarget` alongside the other Dest consts, so `outboxDestTypes`-style maps can reference one vocabulary; do NOT add it to the tenant_notify_targets CHECK — it never appears in that table).
- Produces: worker resolution branch — when `row.DestinationType == DestSubscriptionWebhook`, `destination_target` is the subscription id: load subscription; if missing/disabled → MarkDead(`dead_reason='subscription-disabled'`) without an HTTP attempt; else build `outbound.Target{URL: sub.TargetURL, Secret: sub.Secret, SignatureVersion: "v2-content-hash", TimeoutSeconds: 10}` and send via the existing generic (raw-webhook) adapter render path.
- Produces: 410 hook — where the worker classifies a failed attempt (the MarkFailed/MarkDead decision point that already records `http_status`), add: `if destType == DestSubscriptionWebhook && httpStatus == http.StatusGone { subs.Disable(ctx, subID, webhooksub.ReasonGone); MarkDead(row, "gone") }`.

- [ ] **Step 1: Failing unit tests** (injected fake subscription store + fake transport):

```go
// 1. delivers via generic adapter with subscription URL/secret
// 2. disabled subscription → row dead, no HTTP attempt (fake transport asserts zero calls)
// 3. transport returns 410 → subscription Disable("gone") called AND row dead with dead_reason "gone"
// 4. transport returns 500 → normal retry path, Disable NOT called
```

- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement** — wire `*webhooksub.Repo` into `NewOutboxWorker` (follow how `targets *notifytarget.NotifyTargetRepo` is injected; `cmd/attune` construction site updates in the same commit). Keep the resolution branch in `subscription_target.go` to respect NLOC ≤ 100 per function.
- [ ] **Step 4: Run** `go test ./internal/service/outbox/...` + outbox integration tests green.
- [ ] **Step 5: Commit** — `feat(hooks): subscription-webhook outbox send path with 410 auto-disable`

---

### Task 6: Feedback event fan-out (enricher)

**Files:**
- Modify: `internal/service/enrich/enricher_outbox.go` (`buildOutboxPlan` / `insertOutboxRows`)
- Modify: `internal/service/enrich/enricher.go` (inject `*webhooksub.Repo`; `cmd/attune` wiring)
- Test: extend the enricher outbox tests (same injected-fake style they already use)

**Interfaces:**
- Consumes: `webhooksub.ListActiveByTenantEvent` (called once per event type in plan-build, OUTSIDE the tx, mirroring `notifytarget.ListActiveByTenant` usage), `buildOutboxEnvelope(..., eventType)` (Task 4), `notifytarget.DestSubscriptionWebhook` (Task 5).
- Produces: plan rows — for each enriched snapshot: subscriptions matching `feedback.created` each get one outbox row (payload = envelope with `event_type=feedback.created`, `destination_target` = sub id); if `s.IsUrgent`, subscriptions matching `feedback.urgent` additionally get rows with that event type. A subscription subscribed to both receives BOTH (distinct events by design — Zapier Zaps are per-trigger; document in code comment).
- Soft cap: `CountByTenant` ≥ 25 rejected at CREATE time (Task 7 handler), not here — fan-out trusts stored rows.

- [ ] **Step 1: Failing test** — fake subscription store returns 2 subs (one on `feedback.created`, one on both types); non-urgent snapshot → 1 sub-row (+ existing legacy target rows unchanged); urgent snapshot → 3 sub-rows (created×2? no: sub1 created, sub2 created + urgent ⇒ 3). Assert each row's `destination_type`, `destination_target`, envelope `event_type`.
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement** — extend `buildOutboxPlan`; keep function CCN ≤ 15 by extracting `planSubscriptionRows(subs []webhooksub.Subscription, s domain.Snapshot, ...) []outboxrepo.OutboxRow`.
- [ ] **Step 4: Run** enrich package tests + `make test-integration` subset for the enricher outbox path.
- [ ] **Step 5: Commit** — `feat(hooks): fan out feedback.created/feedback.urgent to webhook subscriptions`

---

### Task 7: Request event emission (same-tx)

**Files:**
- Modify: `internal/service/customerrequest/customerrequest.go` (`Create` ~line 381, `Update` ~line 397)
- Test: extend `internal/service/customerrequest` tests (the package already fakes its repo/tx)

**Interfaces:**
- Consumes: `BuildRequestEnvelope` (Task 4), `webhooksub.ListActiveByTenantEventTx` (Task 1), `outboxrepo.Insert(ctx, tx, row)` (existing — the service gains an injected `outboxInserter` interface `{ Insert(ctx, tx, OutboxRow) (int64, error) }` + `SetAutomationSink(subs subscriptionLister, outbox outboxInserter)` following the `SetNotificationSink` precedent at line 137).
- Produces: in `Create`, after the insert inside the tx: list subs for `request.created`, insert one outbox row each (`FeedbackID: 0` → NULL, `destination_type=subscription-webhook`). In `Update`, at the existing `before.Status != after.Status` branch (~line 434, next to `RecordStatusChangeTx`): same for `request.status_changed` with `previousStatus=before.Status`.
- Nil-sink = no-op (same rule as notificationSink), so console-only deployments are untouched.

- [ ] **Step 1: Failing tests** — (a) Create with sink: outbox insert called in-tx with `request.created` envelope; (b) Update status planned→in_progress: row with `previous_status:"planned"`; (c) Update WITHOUT status change: no insert; (d) sink unset: no panic, no insert; (e) tx rollback (repo returns error after insert): no visible row — cover in the PG integration test.
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement** (+ `cmd/attune` wiring of the sink).
- [ ] **Step 4: Run package + integration tests.**
- [ ] **Step 5: Commit** — `feat(requests): emit request.created and request.status_changed events`

---

### Task 8: `/v1/hooks` handlers (CRUD + samples) + audit

**Files:**
- Create: `internal/handlers/console/webhooksub/handler.go`, `audit.go`, `samples.go`
- Modify: `internal/handlers/console/apikey_admin.go` (mount, following the workflow-states mount pattern at :230) + `cmd/attune/router_v1.go`
- Test: `internal/handlers/console/webhooksub/handler_test.go` (injected service/repo interfaces — unit-testable without PG per attune-coverage-gates)

**Interfaces:**
- Consumes: Task 1 repo, Task 2 scopes, Task 3 proto types, `apikey.RequireExplicitScope(domain.ScopeHooksManage)`.
- Produces routes (all under the existing `/v1` API-key group):
  - `POST /v1/hooks` → validate: `target_url` HTTPS (loopback exemption per config, reuse the notify-target URL validator), `event_types` all pass `domain.IsAutomationEvent`, secret ≥ 16 chars when provided else generate 32-hex; `CountByTenant` ≥ 25 → 409 `subscription limit reached (25)`; success → 201 + audit `webhook_subscription.create`.
  - `GET /v1/hooks` → list (no secrets).
  - `DELETE /v1/hooks/{id}` → 204; missing → 404; audit `webhook_subscription.delete`.
  - `GET /v1/hooks/samples/{event_type}` → performList: unknown event type → 404. For `feedback.*`: query the N=10 most recent enriched feedback (add `ListRecentEnriched(ctx, tenantID string, urgentOnly bool, limit int) ([]domain.Snapshot, error)` to `internal/repo/feedback` — reverse-chron), render via `buildOutboxEnvelope` (export a thin `enrich.RenderSampleEnvelope` wrapper — samples.go must not reach into enrich internals). For `request.*`: `ListRecent` on the customerrequest repo → `BuildRequestEnvelope` (status_changed samples fabricate `previous_status:"open"`). Empty tenant → one canned static sample per event type (fixtures in `samples_static.go`) so Zapier's editor test always gets an item. Response = reverse-chron array.
- Error codes via `ErrorResponse.code` enum — check `scripts/lint-errorcode.sh` conventions; add new codes to the enum if none fit.

- [ ] **Step 1: Failing handler tests** — table-driven: create happy path (201, secret not echoed), bad URL (400), bad event type (400 message lists valid types), cap (409), missing scope (403 via middleware test), delete 204/404, samples for all 4 event types + static fallback + schema-equality assertion: `require.JSONEq` between a live-built envelope and the sample for the same fixture (the D012 guarantee).
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement** handler + mounts + audit wiring (`SetAuditLogger` pattern).
- [ ] **Step 4: Run** handler package tests + `go vet ./...`.
- [ ] **Step 5: Commit** — `feat(hooks): /v1/hooks subscription CRUD and performList samples`

---

### Task 9: `/v1/requests` surface + tag assignment + auth/verify label

**Files:**
- Create: `internal/handlers/console/customerrequest/automation.go` (thin API-key bindings delegating to the existing handler/service methods)
- Modify: `cmd/attune/router_v1.go` (mount under `/v1` with `RequireExplicitScope(requests:*)`), tag-assignment binding `POST /v1/feedback/{id}/tags` with `RequireScope(domain.ScopeTagsWrite)` delegating to the existing assignment handler, auth-verify handler gains `tenant_display_name` (Task 3 proto field; resolve via the existing tenant repo lookup)
- Test: extend customerrequest handler tests + a router-level scope test

**Interfaces:**
- Consumes: existing `service/customerrequest` `Create/Update/AddNote`, existing tag-assignment handler func, Task 2/3 outputs.
- Produces: `GET /v1/requests` (requests:read), `POST /v1/requests`, `PATCH /v1/requests/{id}` (status validated against the flat enum `open|planned|in_progress|shipped|cancelled`), `POST /v1/requests/{id}/notes` (`visibility:"internal"` → `AddNote`; `"public"` → the portal-comment pipeline with moderation, actor = API key). Audit: existing `customer_request.*` actions recorded with key-id actor (verify existing actions cover create/update/add_note — they do per survey; add none).

- [ ] **Step 1: Failing tests** — status transition happy path via PATCH; invalid status 400; note visibility internal/public dispatch; public note lands in moderation queue not live (assert via fake); 403 without `requests:write`; legacy unscoped key → 403 (explicit scope); auth/verify returns tenant name.
- [ ] **Step 2: Run, verify fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** package tests + `scripts/lint-errorcode.sh`.
- [ ] **Step 5: Commit** — `feat(requests): API-key automation surface for customer requests`

---

### Task 10: SDK wrappers (Go + Node)

**Files:**
- Create: `sdk/go/hooks.go`, `sdk/go/requests.go`; Test: `sdk/go/hooks_test.go`, `sdk/go/requests_test.go` (httptest, mirroring `sdk/go/tags_test.go`)
- Modify: `sdk/node/src/client.ts` (+ its test file, mirroring the tags methods)

**Interfaces:**
- Consumes: wire shapes from Task 3 (Go SDK hand-writes wire types per attune-go-sdk memory — zero-dep, NOT proto-generated).
- Produces (Go): `CreateWebhookSubscription`, `ListWebhookSubscriptions`, `DeleteWebhookSubscription`, `ListWebhookSamples`; `ListRequests`, `CreateRequest`, `UpdateRequest`, `AddRequestNote`. Node: same set, camelCase.

- [ ] **Step 1: Failing tests** (httptest asserting method/path/header/body round-trip, error mapping for 403/409).
- [ ] **Step 2-4: Implement, run** `go test ./sdk/go/...` and `pnpm -C sdk/node test`.
- [ ] **Step 5: Commit** — `feat(sdk): webhook subscription and request automation methods`

---

### Task 11: Zapier CLI project (`integrations/zapier/`)

**Files:**
- Create: `integrations/zapier/package.json` (zapier-platform-core ^17, node ≥ 20, own lockfile; NOT referenced by root build/pnpm workspace), `.gitignore`, `README.md`
- Create: `index.js`, `authentication.js`, `triggers/{new_feedback,urgent_feedback,new_request,request_status_changed}.js`, `creates/{create_feedback,update_request,add_tag,add_note}.js`, `samples/*.json`
- Test: `integrations/zapier/test/*.test.js` (zapier-platform-core test harness + nock mocks; `ATTUNE_LIVE_BASE_URL` env switches to a real local stack)

**Interfaces:**
- Consumes: every endpoint from Tasks 8-9 + `/v1/feedback/ingest` + `/v1/auth/verify`.
- Produces per trigger (all four identical in shape, differing in `event_type`):

```js
// triggers/new_feedback.js
const subscribeHook = (z, bundle) => z.request({
  url: `${bundle.authData.base_url}/v1/hooks`, method: 'POST',
  body: { target_url: bundle.targetUrl, event_types: ['feedback.created'], consumer: 'zapier' },
}).then(r => r.data); // { id } stored as bundle.subscribeData
const unsubscribeHook = (z, bundle) => z.request({
  url: `${bundle.authData.base_url}/v1/hooks/${bundle.subscribeData.id}`, method: 'DELETE',
});
// perform: reshape envelope → add Zapier dedup id (delivery id header is not in body,
// so id = `${feedback.id}-${event_type}`; requests use request.id)
const perform = (z, bundle) => {
  const e = bundle.cleanedRequest;
  return [{ id: `${e.feedback.id}-${e.event_type}`, ...e }];
};
const performList = (z, bundle) => z.request({
  url: `${bundle.authData.base_url}/v1/hooks/samples/feedback.created`,
}).then(r => r.data.samples.map(e => ({ id: `${e.feedback.id}-${e.event_type}`, ...e })));
```

- Auth: `custom` type, fields `api_key` + `base_url` (self-hosted OSS → base URL is a connection field); test = `GET /v1/auth/verify`; connection label from `tenant_display_name`.
- Actions call ingest / PATCH request / tag / note endpoints; map 409/400 bodies to Zapier-friendly messages.

- [ ] **Step 1: Scaffold + failing tests** — `zapier init` equivalent by hand (no network scaffold), write nock-based tests: subscribe stores id, unsubscribe hits DELETE, perform reshapes live payload, performList output schema-equals perform output (Zapier T004 mirror), each create posts the right body, auth test 401 propagates.
- [ ] **Step 2: Run, verify fail** — `npm -C integrations/zapier test`.
- [ ] **Step 3: Implement all modules; `zapier validate` passes locally** (no push).
- [ ] **Step 4: Run tests green**; ensure `scripts/lint-artifacts.sh --strict` ignores or passes on the new tree (English-only content, no roadmap markers).
- [ ] **Step 5: Commit** — `feat(zapier): in-repo Zapier Platform CLI integration with mock-API tests`

---

### Task 12: Docs, CHANGELOG, full verification

**Files:**
- Create: `docs/integrations/zapier.md` (connector auth guide: create an API key with `hooks:manage`+`requests:write`+`ingest:write`+`tags:write`; four sample recipes: "Zendesk ticket → attune feedback", "urgent feedback → Slack DM via Zap", "Typeform → create request", "request shipped → email customer")
- Modify: `CHANGELOG.md` `[Unreleased] → ### Added`, `README.md` (one line in the integrations section), proposal doc `Status: Accepted → Implemented`
- Modify (if benchmark §6 landed ADOPT-NOW items): fold each into its owning task's pattern — small ones (e.g. ping/test-delivery endpoint, `GET /v1/hooks/events` vocabulary listing) append here as explicit sub-steps before the final gate.

- [ ] **Step 1: Write docs + changelog.**
- [ ] **Step 2: Full gate** — `make ci-check` (or the documented full subset: `go vet ./...`, `go build ./...`, `go test -race ./...`, `golangci-lint run`, `lizard`, `scripts/lint-slog.sh --strict`, `scripts/lint-rawptr.sh`, `scripts/lint-errorcode.sh`, `scripts/lint-integration-layout.sh`, `scripts/lint-artifacts.sh --strict`, `make test-integration`, `buf lint/breaking` + generated-drift, console checks untouched-but-run). Cite output.
- [ ] **Step 3: Real-LLM e2e** (attune-local-e2e-verification + attune-local-e2e-setup memories): local stack + loopback reverse proxy, `allow_loopback_egress`; create subscription via curl pointing at a local catcher; ingest → enrich → assert catcher received `feedback.created` (+ `feedback.urgent` for an urgent fixture) with valid `X-Attune-Signature`; create + status-change a request via `/v1/requests` → assert both request events; kill catcher with 410 → assert subscription auto-disabled. Then `npm -C integrations/zapier test` in live mode against the same stack.
- [ ] **Step 4: Commit** — `docs(zapier): connector guide, recipes, changelog` and mark proposal Implemented.

---

## Self-review notes

- Spec coverage: proposal §1→Task 1/5, §2→Tasks 4/6/7, §3→Tasks 3/8/9, §4→Tasks 8/9 (+scopes Task 2), §5→Task 11, verification→Task 12. Benchmark §6 pending → explicit fold-in point in Task 12.
- Type consistency: `webhooksub.Subscription`/repo method names used in Tasks 5-8 match Task 1's Produces block; envelope builders defined Task 4, consumed 6/7/8; `DestSubscriptionWebhook` defined Task 5, consumed 6/7.
- Known judgment calls surfaced to implementers: Zapier dedup id is `feedback.id + event_type` (delivery-id header unavailable in body); samples endpoint fabricates `previous_status` for status_changed; subscription cap enforced at create-time only.
