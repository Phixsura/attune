# Unified Audit Log for Sensitive Console Actions

| Field | Value |
|---|---|
| Issue | [#39](https://github.com/Phixsura/attune/issues/39) |
| Status | Implemented |
| Started | 2026-06-16 |
| Related | [#38](https://github.com/Phixsura/attune/issues/38) (RBAC), [#40](https://github.com/Phixsura/attune/issues/40) (OIDC SSO), [#29](https://github.com/Phixsura/attune/issues/29) (workflow audit), [#41](https://github.com/Phixsura/attune/issues/41) (API key scopes), [#43](https://github.com/Phixsura/attune/issues/43) (GDPR export/delete), [#66](https://github.com/Phixsura/attune/issues/66) (channel-agnostic inbound) |

---

## Problem

Attune now has real control-plane actions in the Console:

- RBAC member invite / role change / removal
- OIDC-backed user access
- Inbound source create / rotate secret / pause / resume / delete
- Guard policy edits
- LLM channel, ability, and route management
- API key mint / revoke
- Notify target CRUD
- Feedback delete and batch delete

These actions are security-sensitive, tenant-impacting, and often compliance-relevant. Today there is no unified, immutable audit trail for them.

### Current state

1. **There is no generic console audit table or handler.**
   The Console router exposes many privileged mutation endpoints, but no `/fb/v1/console/audit-log` surface exists yet.

2. **There is already a narrow workflow audit log, not a control-plane audit log.**
   `feedback_audit_log` records workflow field changes on feedback rows and powers the per-feedback audit timeline. It is useful, but it is intentionally scoped to feedback workflow history, not “who changed system settings / secrets / roles”.

3. **Recent features increased the need sharply.**
   `main` now includes OIDC and RBAC, including member invites and role changes. That makes “who granted access / who changed privilege” a first-class operational question, not a future nice-to-have.

4. **Sensitive mutations currently disappear into normal application logs.**
   Structured logs are helpful for debugging, but they are not sufficient for compliance handoff:
   - they are not tenant-friendly to browse,
   - they are not guaranteed immutable,
   - they are not normalized across features,
   - and they should not be the only source for security review.

### Why existing mechanisms are insufficient

| Existing mechanism | Why it is insufficient |
|---|---|
| `feedback_audit_log` | Per-feedback workflow history only; wrong abstraction for member / secret / config changes |
| Structured app logs | Operational debugging, not a durable compliance surface |
| Metrics | Great for counts and alerts, not for per-action evidence |
| Current DB tables | Hold current state, not who changed it, from what, and when |

### Impact

- **Enterprise blocker** — auditability is explicitly part of the v1.0 “Enterprise-ready” milestone.
- **Security gap** — privileged changes cannot be reviewed cleanly after the fact.
- **Support friction** — debugging “who changed this route / policy / role?” is slower than it should be.
- **Compliance gap** — SOC2-style access review and PIPL-style processing accountability need a durable operator-visible trail.

---

## Goals

1. Provide a **tenant-scoped, append-only audit log** for sensitive Console actions.
2. Capture **who**, **when**, **what action**, **what target**, and a **sanitized before/after diff**.
3. Expose an **admin-only read API** with pagination and filters for date range, actor, and action.
4. Add a **read-only Console page** for browsing and filtering audit rows.
5. Support **CSV export** for compliance / incident-response handoff.
6. Enforce immutability at the **database layer**, not only in Go.
7. Support **retention pruning** with a safe default of 365 days.
8. Keep **secrets and raw sensitive values out of audit payloads**.
9. Integrate with the current architecture:
   - proto-first JSON APIs,
   - feature-based Console code,
   - repo/service/handler layering,
   - existing RBAC.

---

## Non-goals

1. Replace `feedback_audit_log` with a universal history system.
   Keep it for high-volume per-feedback workflow history.

2. Build a generic database row-versioning framework for every table.
   This issue is about sensitive Console actions, not universal CDC.

3. Audit every read operation.
   This proposal covers sensitive mutations plus a small number of security-relevant operational actions.

4. Store raw secrets, API keys, webhook secrets, or full prompt bodies in audit rows.
   Audit payloads must use sanitized metadata only.

5. Introduce a separate event bus or external SIEM dependency for v1.
   The first implementation should remain self-hosted and local to Postgres.

6. Backfill historical actions from old logs.
   The audit log starts being authoritative from the migration forward.

---

## Proposal

### 1. Add a dedicated `audit_log` table

Introduce a new table for control-plane audit rows:

```sql
CREATE TABLE audit_log (
    id                BIGSERIAL PRIMARY KEY,
    tenant_id         TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,

    actor_user_id     TEXT        NOT NULL,
    actor_type        TEXT        NOT NULL CHECK (actor_type IN ('admin', 'oidc_user', 'tenant_user', 'system')),
    actor_role        TEXT        NOT NULL,
    actor_display     TEXT        NOT NULL,
    actor_ip          INET,
    actor_user_agent  TEXT        NOT NULL DEFAULT '',

    action            TEXT        NOT NULL,
    target_type       TEXT        NOT NULL,
    target_id         TEXT        NOT NULL,
    target_display    TEXT        NOT NULL DEFAULT '',

    before            JSONB,
    after             JSONB,
    meta              JSONB       NOT NULL DEFAULT '{}'::jsonb,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

#### Why a dedicated table

- The data shape is different from `feedback_audit_log`.
- Query patterns are different: control-plane list by actor/action/date, not “timeline for one feedback row”.
- Retention and CSV export requirements are different.
- The semantics are different: immutable evidence of privileged operations.

### 2. Keep `feedback_audit_log` separate

This proposal intentionally does **not** fold the workflow audit table into the new one.

#### Boundary

| Table | Purpose |
|---|---|
| `feedback_audit_log` | High-volume object history for one feedback row |
| `audit_log` | Sensitive Console action trail across members, secrets, config, and destructive operations |

#### Exception

Some operations may write to both:

- `feedback.transition` continues using `feedback_audit_log`
- `feedback.delete` also emits a row to `audit_log` because a control-plane deletion needs to remain visible even after the feedback row is gone

This keeps both UX surfaces correct:

- feedback detail timeline stays focused and fast,
- compliance/security review sees one control-plane stream.

### 3. Enforce append-only semantics at the DB layer

Add a trigger that:

- always rejects `UPDATE`,
- rejects `DELETE` unless the transaction explicitly marks itself as the retention pruner.

#### Mechanism

Use a guarded `SET LOCAL` flag inside the retention transaction:

```sql
CREATE FUNCTION audit_log_reject_mutation() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'audit_log is append-only';
    END IF;

    IF TG_OP = 'DELETE' THEN
        IF current_setting('app.audit_prune', true) <> 'on' THEN
            RAISE EXCEPTION 'audit_log rows may only be deleted by retention prune';
        END IF;
        RETURN OLD;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
```

This gives us both:

- **immutability for normal app paths**, and
- **controlled retention deletion** without needing a separate DB role.

### 4. Use action taxonomy, not ad hoc free-form strings

Store `action` as `TEXT` plus a Go-side constant set and a DB `CHECK` constraint.
Prefer `TEXT + CHECK` over PostgreSQL enum because it is easier to evolve in incremental migrations.

#### Initial action set

```text
member.invite
member.update_role
member.remove

api_key.create
api_key.revoke

notify_target.create
notify_target.update
notify_target.delete
notify_target.test

digest_subscription.upsert
digest_subscription.delete

tag.create
tag.update
tag.archive

inbound_source.create
inbound_source.rotate_secret
inbound_source.pause
inbound_source.resume
inbound_source.delete
inbound_source.test_connection

feedback_job.cancel

guard_policy.create
guard_policy.update
guard_policy.delete

enrich_config.update

llm_channel.create
llm_channel.update
llm_channel.delete
llm_channel.test
llm_ability.upsert
llm_ability.delete
llm_route.upsert
llm_route.delete

workflow_state.create
workflow_state.update
workflow_state.archive
workflow_transition.replace
workflow_seed_defaults.run

feedback.delete
feedback.batch_delete
```

#### Out of scope for v1

- every feedback tag add/remove,
- every workflow transition,
- every read/search/list call.

Those are operationally useful, but they would blur the line between “sensitive control-plane trail” and “general activity stream”.

### 5. Sanitize `before` / `after` aggressively

The most important design rule is:

> `before` and `after` must capture the business-relevant diff without ever persisting raw secrets or secret-equivalent values.

#### Rules

1. **Never store plaintext secrets**
   - API keys
   - inbound webhook secrets
   - IMAP passwords
   - LLM provider keys
   - session material

2. **Use sanitized projections**
   - safe identifiers,
   - booleans,
   - counts,
   - roles,
   - non-secret URLs,
   - key fingerprints / last4 / version markers where useful.

3. **Prefer omission over risky redaction**
   If a field is security-sensitive and not essential for review, do not include it.

#### Examples

| Action | `before` | `after` |
|---|---|---|
| `member.invite` | `null` | `{"email":"a@b.com","role":"member","member_type":"invite"}` |
| `member.role_change` | `{"role":"viewer"}` | `{"role":"member"}` |
| `inbound_source.rotate_secret` | `{"secret_version":2}` | `{"secret_version":3,"next_eligible_at":"..."}` |
| `apikey.create` | `null` | `{"key_id":"...","label":"main","prefix":"atu_...","created_for_tenant":"..."}` |
| `llm.channel.update` | `{"name":"openai","auth_mode":"bearer","credential_present":true}` | `{"name":"openai-prod","auth_mode":"bearer","credential_present":true}` |
| `feedback.delete` | `{"feedback_id":"123","mode":"soft","title":"Checkout crashes"}` | `{"deleted":true}` |

#### Implementation shape

Do not ask repos to serialize whole rows blindly. Instead add small per-feature “projector” helpers that produce a safe snapshot struct for audit.

This keeps secret handling explicit and reviewable.

### 6. Record actor metadata from the request context

Each audit row should snapshot:

- `actor_user_id`
- `actor_type`
- `actor_role`
- `actor_display`
- `actor_ip`
- `actor_user_agent`

#### Why snapshot more than user ID

- users can be renamed or removed later,
- current role may differ from the role at action time,
- CSV export should remain legible without extra joins,
- incident review should not depend on reconstructing current identity state.

#### Actor source

For Console requests, the audit writer reads from `session.AuthCtx` plus RBAC middleware context.

For non-human system actions, use:

- `actor_type = 'system'`
- `actor_user_id = 'system'`
- `actor_display = 'system'`

This proposal does not require broad system-action coverage in v1, but the schema should support it.

### 7. Add a small `auditlog` repo + service

Create:

- `internal/repo/auditlog/`
- `internal/service/auditlog/`

#### Repo responsibilities

- append row
- filtered list query
- CSV export query / stream support
- prune expired rows

#### Service responsibilities

- central `Record(...)` API
- request-context actor extraction
- safe snapshot assembly
- consistent action constants
- light validation (`tenant_id`, action, target present)

#### Why a service layer

Without a service, every handler will hand-roll:

- actor extraction,
- request metadata capture,
- JSON shape decisions,
- and secret sanitization.

That is too risky for a security-sensitive feature.

### 8. Wire recording at the service or orchestration boundary

Prefer to emit audit rows from the same place that owns the business mutation, ideally in the same transaction when the mutation is transactional.

#### Pattern

1. load current state
2. validate authorization / invariants
3. compute safe `before`
4. perform mutation
5. compute safe `after`
6. write audit row in the same transaction

#### Why not handler-only logging

Handler-only logging is weaker because:

- it cannot always see authoritative `before` state,
- it risks logging actions that later roll back,
- and it spreads audit knowledge into HTTP glue instead of business code.

### 9. JSON list API: proto-first

Add `proto/attune/v1/audit.proto` with an `AuditLogService`.

#### JSON endpoint

`GET /fb/v1/console/audit-log`

#### Request fields

- `cursor`
- `limit`
- `date_from`
- `date_to`
- `actor_user_id`
- `actions[]`

#### Response fields

- `entries[]`
- `next_cursor`

#### Entry fields

- `id`
- `tenant_id`
- `actor_user_id`
- `actor_type`
- `actor_role`
- `actor_display`
- `actor_ip`
- `actor_user_agent`
- `action`
- `target_type`
- `target_id`
- `target_display`
- `before_json`
- `after_json`
- `meta_json`
- `created_at`

#### Authorization

Admin-only via RBAC middleware.

### 10. CSV export: raw file endpoint sharing the same query path

Use a dedicated raw endpoint for export:

`GET /fb/v1/console/audit-log/export.csv`

#### Why not squeeze CSV through proto

The repo is proto-first for JSON HTTP contracts, but CSV is a file download.
The cleanest shape is:

- proto-backed JSON list endpoint for app usage,
- raw `text/csv` endpoint for export.

This is already consistent with the Console router having a few non-dispatch endpoints where the response is not a normal proto JSON envelope.

#### Requirements

- same filters as JSON list endpoint
- admin-only
- streamed writer, not full in-memory buffer
- fixed column order
- ISO-8601 timestamps in UTC
- JSON columns serialized compactly

#### Suggested CSV columns

```text
created_at,tenant_id,actor_user_id,actor_type,actor_role,actor_display,
actor_ip,action,target_type,target_id,target_display,before,after,meta
```

### 11. Console UI: read-only audit page under Settings

Add a new admin-only page under Settings:

- route: `/settings/audit-log`
- feature folder: `console/src/features/audit-log/`

#### UX

- filter bar:
  - date range
  - actor
  - action multi-select
- table:
  - time
  - actor
  - action
  - target
  - summary
- detail sheet:
  - request metadata
  - pretty-printed before/after JSON
- export button:
  - downloads current filter as CSV

#### Deliberate constraints

- read-only
- no inline mutation actions from this page
- no free-text full-text search in v1

### 12. Add process config for retention

Extend runtime YAML:

```yaml
audit:
  retention_days: 365
  prune_interval: "24h"
```

#### Behavior

- `retention_days <= 0` disables pruning
- default is 365 days
- worker wakes on `prune_interval`
- each run deletes rows older than `NOW() - retention_days`

#### Why process config, not tenant config

Retention is a deployment/compliance decision, not an end-user self-service setting.

### 13. Retention worker and metrics

Add a small background worker started from `cmd/attune/server.go`.

#### Metrics

- `attune_audit_rows_written_total{action}`
- `attune_audit_rows_pruned_total`
- `attune_audit_prune_duration_seconds`

These should stay bounded and low-cardinality.

### 14. Compliance documentation

Document:

- what is recorded,
- what is intentionally omitted,
- retention behavior,
- operator responsibilities for protecting the endpoint/export,
- how this supports access review and incident response.

Suggested docs:

- `docs/compliance-audit-log.md`
- `docs/private-deploy.md` section on audit retention/export

#### Positioning

This is not “SOC2 compliant by itself”. Instead:

- it provides evidence collection and operator accountability,
- aligns with least-privilege review under SOC2-style controls,
- and supports PIPL-style processing traceability without logging raw secrets.

---

## Alternatives Considered

### A. Reuse `feedback_audit_log` as the generic audit table

**Rejected.**

Why:

- wrong query model,
- wrong payload model,
- no actor IP / user-agent,
- too coupled to feedback workflow semantics,
- would make both surfaces worse.

### B. Only rely on structured application logs

**Rejected.**

Why:

- not normalized,
- not easily tenant-filterable,
- not a durable product surface,
- and not strong enough for compliance export.

### C. Generic trigger-based table history for every row change

**Rejected for v1.**

Why:

- would capture too much noise,
- difficult to sanitize secrets safely,
- weak semantic action names,
- high implementation scope relative to #39.

### D. Client-side CSV export from paged JSON data only

**Rejected.**

Why:

- would only export what the browser loaded,
- forces the UI to page through all data,
- makes compliance handoff brittle on large datasets.

### E. External SIEM / Kafka / webhook sink

**Rejected for v1.**

Why:

- adds operational complexity,
- conflicts with self-hosted simplicity,
- can be layered later after the local audit table exists.

---

## Risks / Tradeoffs

### 1. Payload creep

Risk:

- teams may try to dump entire mutated objects into `before` / `after`.

Mitigation:

- require explicit per-action projector helpers,
- code review rule: no raw secret-bearing structs serialized directly.

### 2. Transaction coupling

Risk:

- adding audit writes to many mutation paths increases implementation breadth.

Mitigation:

- start with the highest-value actions,
- centralize common writer logic,
- keep audit writes in the same transaction only where the mutation already has one.

### 3. Storage growth

Risk:

- audit rows accumulate quickly in busy deployments.

Mitigation:

- retention default 365 days,
- narrow scope to sensitive actions only,
- indexes tuned for list filters, not analytical fan-out.

### 4. False sense of completeness

Risk:

- operators may assume every meaningful action is audited on day one.

Mitigation:

- document the audited action set explicitly,
- treat new sensitive mutations as requiring audit coverage in future PRs.

### 5. Immutability vs retention

Risk:

- “append-only” sounds incompatible with pruning.

Mitigation:

- define immutability as “no ordinary update/delete path”,
- retention is a controlled administrative exception enforced through guarded DB logic.

---

## Implementation Plan

### Phase 1 — Schema and plumbing

1. Add `audit` config block with defaults and validation.
2. Add migration for:
   - `audit_log`
   - indexes
   - append-only trigger/function
3. Add `internal/repo/auditlog/`.
4. Add `internal/service/auditlog/`.
5. Add metrics.

### Phase 2 — API surface

1. Add `proto/attune/v1/audit.proto`.
2. Run `make proto`.
3. Add Console handler:
   - list endpoint
   - export CSV endpoint
4. Mount admin-only routes in Console router.

### Phase 3 — High-value emitters

Add audit emission for:

1. members:
   - invite
   - role change
   - remove
2. inbound sources:
   - create
   - rotate secret
   - pause
   - resume
   - delete
3. API keys:
   - create
   - revoke
4. feedback:
   - delete
   - batch delete
5. guard policies:
   - create
   - update
   - delete
6. digest subscriptions:
   - upsert
   - delete
7. tags:
   - create
   - update
   - archive
8. enrich config:
   - update
9. feedback jobs:
   - cancel
10. LLM config:
   - channel create/update/delete/test
   - ability upsert/delete
   - route upsert/delete
11. add a route-inventory test that requires every mutating Console endpoint to
    declare an explicit audit decision (`audited` or `exempt with reason`)

### Phase 4 — Console UI

1. Add `features/audit-log`.
2. Add page under Settings.
3. Add filters, table, detail panel, export button.
4. Add RBAC hide/show wiring in navigation.

### Phase 5 — Retention and docs

1. Add prune worker.
2. Add prune tests and metrics.
3. Add compliance / deploy docs.
4. Add changelog entries under:
   - `### Added`
   - `### Security`

---

## Verification

### Unit tests

1. action validation rejects unknown values
2. projector helpers never include secret-bearing fields
3. actor extraction captures role / type / display correctly
4. CSV row formatting is stable and quoted correctly

### Repo / integration tests

1. **Append-only**
   - `UPDATE audit_log` fails
   - ordinary `DELETE audit_log` fails
   - prune transaction with `SET LOCAL app.audit_prune = 'on'` succeeds

2. **Filters**
   - actor filter works
   - action filter works
   - date range filter works
   - cursor pagination is stable and descending by id

3. **Sanitized diff**
   - `rotate-secret` row never contains plaintext secret
   - API key create row contains prefix/metadata only

4. **Transactional correctness**
   - failed mutation does not leave a phantom audit row
   - successful mutation writes exactly one audit row

5. **Coverage guard**
   - every `POST` / `PUT` / `PATCH` / `DELETE` Console route appears in the
     audit-coverage inventory test
   - new mutating routes fail CI until they are explicitly marked as audited or
     intentionally exempt

### Handler tests

1. viewer/member forbidden from audit-log endpoints
2. admin can list and export
3. bad filter input returns structured `BAD_REQUEST`
4. CSV endpoint returns `text/csv` with expected headers

### Console tests

1. page renders rows from API
2. filters round-trip to query params / requests
3. export button hits CSV endpoint with active filters
4. non-admin navigation does not show the page

### End-to-end scenarios

1. invite member → audit row appears
2. change member role → before/after role diff appears
3. rotate inbound secret → sanitized metadata row appears
4. delete feedback → control-plane audit row remains visible
5. prune old rows → rows older than retention disappear, recent rows remain

### Implemented verification notes

1. Added service-side unknown-action rejection and a DB `CHECK` constraint so
   new audit action strings must be registered in both code and schema.
2. Added PostgreSQL integration coverage for append-only enforcement, action /
   actor / date-range / multi-action filters, cursor pagination, retention
   pruning, and `member.invite` end-to-end audit emission.
3. Added a router inventory guard so every mutating Console route must declare
   an explicit `audited` or `exempt` decision during tests.
4. Added Console component coverage for multi-action filters, request-metadata
   drill-down, and cursor pagination.
5. Re-ran the audit page in a local browser-backed environment on June 16, 2026
   against a seeded Postgres dataset, verifying:
   - multi-action selection state renders in the filter trigger
   - date-range filtering narrows the visible row set
   - row details expose actor email / IP / user-agent metadata
   - cursor pagination still exposes “加载更多” for result sets larger than 50

---

## References

- Issue [#39](https://github.com/Phixsura/attune/issues/39)
- RBAC proposal: [2026-06-16-rbac-admin-member-viewer.md](./2026-06-16-rbac-admin-member-viewer.md)
- OIDC proposal: [2026-06-15-oidc-sso.md](./2026-06-15-oidc-sso.md)
- Workflow audit proposal: [2026-06-14-feedback-workflow-status.md](./2026-06-14-feedback-workflow-status.md)
- Inbound proposal note that lifecycle audit folds into #39: [2026-06-08-channel-agnostic-inbound.md](./2026-06-08-channel-agnostic-inbound.md)
- Roadmap: [README.md](../../../../README.md)
