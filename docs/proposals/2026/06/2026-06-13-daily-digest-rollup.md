# Daily digest roll-up with LLM-labeled top themes per tenant

| | |
|---|---|
| **Issue** | #27 |
| **Status** | Implemented |
| **Started** | 2026-06-13 CST |
| **Related** | #114 (embedding semantic clustering — themes reuse its cluster + `cluster_label` path/purpose), #26 (reply-draft worker/outbox paradigm reused), #25 (embedding outbox/worker paradigm), #66 (inbound refactor — removed the old weekly digest scheduler in Plan T17, pointed a channel-agnostic digest at #34), #34 (outbound adapter SDK — future Lark/Slack/email channels), #109 (managed LLM routing — theme naming reuses existing purposes) |

> **Review-hardened 2026-06-13.** This revision incorporates a code-verified
> adversarial review that refuted several load-bearing assumptions in the first
> draft (a new LLM purpose would fail unconfigured; the outbox resolves targets
> by `(tenant, dest_type, audience)` not by id; the `≤5 rows` threshold skips
> the *LLM*, not the *send*; unclustered rows are silently dropped). See
> [Review hardening](#review-hardening) for the full list and where each was
> verified.

## Problem

Per-row notifications fire as each feedback finishes enrichment — right for a P0
"checkout is down" alert, but noisy at daily volume. A tenant taking 47 feedbacks
a day gets 47 webhook POSTs and no synthesis. Operators want the inverse: **one
message each morning** that rolls up yesterday and surfaces the few themes that
matter:

> *Yesterday: 47 feedbacks. Top themes: (1) checkout broken on Safari — 12
> reports (#1024, #1031); (2) export timeout — 8 reports (#1066, #1071);
> (3) feature request: dark mode — 5 reports (#1090, #1092).*

attune already has the two halves this needs but has never joined them:

- **Aggregation** — `internal/repo/feedback/feedback_console_stats.go` already
  groups enriched feedback by tenant + time window (`UsageByDay`,
  `TopValuesByDim`, `UrgentCount`).
- **Themes** — #114 (commit `6dd0ea8`) shipped semantic clustering. Each
  feedback carries a `cluster_id` + `cluster_label`
  (`025_embedding_clustering.sql:22-23`); the label is produced by
  `maybeGenerateClusterLabel` (`internal/service/embedding/worker.go:213-263`)
  with a prompt that already does *exactly* this issue's "feed it titles, ask
  for the theme" step, and cluster counts already come from SQL `COUNT(*)`
  (`internal/repo/embedding/task.go:286-299`).

What is missing is the **time-triggered glue**: a per-tenant, timezone-aware
scheduler that fires once per local morning, rolls up yesterday, turns the
result into a themed message, and delivers it — exactly once, even across
restarts and replicas.

## Code reconciliation (issue text vs. verified code)

| Issue says | Verified reality | Decision |
|---|---|---|
| "New `tenants.timezone` (default `UTC`)" | Exists since migration `008` (default `Asia/Shanghai`); `tenant.Tenant.Timezone` scanned (`tenants.go:107-125`); **never validated as IANA** anywhere | Reuse the column; **add IANA validation** on write + graceful `LoadLocation` failure in the worker. |
| "render as Lark / Slack message" | Lark hard-deleted (migration `015`); Slack `NOT_IMPLEMENTED`; only `raw-webhook` + `github-issue` deliver | Deliver a rendered JSON+markdown payload via the **`raw-webhook` outbox**; the customer's Lark/Slack endpoint renders it. Dedicated adapters are #34. |
| "Step 2: LLM call, top-3 themes" | #114 already clusters + LLM-labels; counts are SQL `COUNT(*)` | **Cluster-then-label**: themes = top-N clusters for the window; counts + example IDs from SQL; LLM only names, **reusing the existing `cluster_label` purpose** (a new purpose would fail unconfigured — see C1). Naive single-call (`enrich` purpose) is the fallback for clustering-off tenants. |
| "skip if ≤ 5 rows" | — | This skips the **LLM**, not the digest: 0 → no send (unless opt-in); **1–5 → themeless digest** listing the rows; ≥6 → themed digest. (C3) |
| "New audience: `digest`" | `audience` CHECK is `('pool','radar','all')` (`004:13-14`); `selectOutboxTargets` has **no** digest case (`enricher_outbox.go:311-329`) | Add `audience='digest'` **only as a routing filter** (extend the CHECK + a skip case so a digest target gets no per-event traffic). The schedule lives in `digest_subscriptions`, not on the audience. |
| "`digest_runs(tenant_id, run_date)` dedup" | No such table; `last_digest_sent_at` (`010`) is vestigial | Adopt `digest_runs` keyed `UNIQUE(tenant_id, run_date)` as the exactly-once ledger. Leave `last_digest_sent_at` unused. |
| "At most one digest per tenant per local day" | A full per-tenant *entity* could allow N subscriptions ⇒ N digests/day | **v1: one `digest_subscriptions` row per tenant** (`UNIQUE(tenant_id)`); the entity stays fully configurable (frequency/weekday/hour). Multiple subscriptions/targets per tenant = future. (M4) |

## Industry benchmarking

Benchmarked the comparable subsystem in seven top-tier OSS projects across four
axes, plus the Go-ecosystem idioms for timezone scheduling and transactional
outbox.

### Scheduling & local-time delivery

| System | Trigger | Local-time model | Dedup |
|---|---|---|---|
| **Discourse** (`Jobs::EnqueueDigestEmails`) | poll `every 30.minutes` + "elapsed since last attempt" | none — interval-since-last | per-user `digest_attempted_at` watermark, bumped **even on empty** |
| **Zulip** (`zerver/lib/digest.py`) | nightly cron + per-realm `digest_weekday` | per-realm weekday on server clock | `RealmAuditLog` ledger, 12h look-back |
| **Sentry** (`tasks/summaries/weekly_reports.py`) | celery-beat `0 0 * * saturday` → fan-out one task per org | localize **label only**, deliver globally | Redis `(date, org, user)` incr-ledger + resume checkpoint |
| **GitLab** (`config/schedule.yml`) | sidekiq-cron, per-entry `cron_timezone` | tz in the cron string (caused outages, GitLab #556779) | sidekiq-cron Redis sorted-set + `CronjobQueue` |

**Conclusions.** None precompute per-tz send times; the closest analog to "tick
and check local hour" is Discourse's poll+filter — *structurally identical to
attune's existing `time.Ticker` workers*. tz-in-cron is a documented fragility;
store tz as a **per-row column and compute due-ness in the loop**. Make the tick
*cheap* and isolate per-tenant work (Sentry's fan-out).

### Exactly-once across restarts

All four use a durable dedup keyed by *(date-bucket, tenant[, recipient])*. The
canonical Postgres upgrade — **the insert is the claim**:

```sql
INSERT INTO digest_runs (tenant_id, run_date, status)
VALUES ($1, $2, 'pending')
ON CONFLICT (tenant_id, run_date) DO NOTHING
RETURNING id;     -- zero rows ⇒ already claimed this local day ⇒ skip
```

Stronger than Zulip's soft 12h window, no Redis (unlike Sentry). `river` reaches
"once per period" the same way — a unique index, not timing
([River](https://riverqueue.com/docs/periodic-jobs)). Exactly-once delivery is
impossible (two-generals): the ledger guarantees **at most one enqueue**, the
existing outbox guarantees **at least one delivery**, and an idempotency key
`digest:{tenant}:{run_date}` in the payload lets the receiver dedup
([outbox](https://microservices.io/patterns/data/transactional-outbox.html)).

### Skip-empty & opt-in

Zulip gates on `enough_traffic()`, Discourse on `if @popular_topics.present?`;
both **mark the attempt regardless** so an empty day doesn't re-fire. → skip the
LLM+webhook below threshold but still write the `digest_runs` row
(`skipped_empty`). Do **not** copy the "inactive recipients only" targeting — a
re-engagement nudge, not a proactive ops roll-up.

### Batch summarization → themes

The four strategies are *stuff*, *map-reduce*, *refine*, and **cluster-then-label**.
Production feedback tools converge on cluster-then-label (Enterpret hierarchical
clustering + auto-label; Dovetail "Magic Cluster"); academia splits the roles
deterministically ([TnT-LLM](https://dl.acm.org/doi/pdf/10.1145/3637528.3671647);
[k-LLMmeans](https://arxiv.org/pdf/2502.09667)): **code does grouping + counting
+ example selection; the LLM only names.** Decisive, and exactly what #114
already does (SQL `COUNT(*)`, LLM label only). Sentry independently validates
the shape — cheap deterministic grouping first, embeddings only on the residual.

### Config-entity shape (the `audience` vs. entity question)

| System | Entity | Schedule | LLM-summary config | Timezone |
|---|---|---|---|---|
| **Metabase** | `pulse` / `pulse_channel` | `schedule_type`/`hour`/`day` → Quartz cron | — | instance-local (leak) |
| **PostHog** | `Subscription` | rrule + materialized `next_delivery_date` | **`summary_enabled` + `summary_prompt_guide`** | wall-clock (leak) |
| **Grafana** | `Report` + `Schedule` | `frequency`/`dayOfMonth`/`workdaysOnly` | — | **explicit IANA `timeZone`** (correct) |

**All three model the scheduled report as a separate first-class entity** — never
a value on a generic notification-target row. `pool`/`radar`/`all` are stateless
per-event *routing predicates*; a digest is a *schedule + aggregation job with a
cursor*. → build `digest_subscriptions`; borrow PostHog's **materialized
`next_run_at`** cursor and Grafana's **explicit per-row IANA timezone**.

### Go idiom: ticker vs. cron dependency

`robfig/cron/v3` buys almost nothing: the only correctness-sensitive part is pure
stdlib (`time.Date(y,m,d,9,0,0,0,loc)`), it is in-memory/single-process (fires on
every replica — the ledger is needed regardless), and its DST behaviour is a
liability ("jobs scheduled during daylight-savings leap-ahead transitions will
not be run!", [godoc](https://pkg.go.dev/github.com/robfig/cron/v3)). → **stay
ticker-based** (matches every existing worker; satisfies CLAUDE.md §8 by adding
no dependency). Compute the next fire from civil fields (never `+24h`); 09:00
avoids the ambiguous DST window; `time.Date(y, Feb, 29, …)` handles leap day.

## Goals / Non-goals

**Goals**

1. Once per tenant per local day, deliver a single themed roll-up of yesterday —
   **at most one** even across restarts / replicas.
2. Themes are **LLM-named clusters with SQL-derived counts and real example
   IDs** (reuse #114); a naive single-call fallback covers clustering-off tenants.
3. Tiered by volume: 0 → no send (unless opt-in); 1–5 → themeless list; ≥6 →
   themed.
4. Tenant-configurable via Console + API: enable, frequency (daily/weekly), local
   send hour, weekday, LLM threshold, opt-in-on-empty.
5. Delivery reuses the existing at-least-once `raw-webhook` outbox.

**Non-goals**

- Dedicated Lark/Slack/email adapters or channel-specific cards (#34). One
  `raw-webhook` payload today.
- A general cron engine / arbitrary cron expressions. One rule: "daily, or weekly
  on weekday W, at local hour H."
- **Multiple digest subscriptions or multiple digest targets per tenant** (v1 is
  one per tenant); monthly / `bysetpos` cadences — schema leaves room, not built.
- Re-engagement / recipient-activity targeting; backfilling missed days.

## Proposal

### Architecture

```
                         digest worker (time.Ticker, 60s)  ── new ──
                                  │
        ┌── select due ──────────┤  SELECT … WHERE enabled AND next_run_at <= now()
        │   (digest_subscriptions)│
        │                         ├── load tz (LoadLocation; fail ⇒ warn+advance+skip)
        │                         ├── queue-lag check (QueueDepthByTenant) ⇒ defer if backlogged
        │                         │
        │                         ├── claim + advance (ONE tx, idempotent):
        │                         │     INSERT digest_runs(tenant_id, run_date) ON CONFLICT DO NOTHING
        │                         │     UPDATE next_run_at = next occurrence strictly after now()
        │                         │
        │                         ├── aggregate window [from,to) by created_at in tenant tz
        │                         │     totals: COUNT(*) all rows (clustered + not)
        │                         │     0          → skipped_empty (unless send_on_empty)
        │                         │     1..llm_min → themeless list (no LLM)
        │                         │     >= llm_min → themes:
        │                         │        clustering on  → top-N clusters + label (reuse #114)
        │                         │        clustering off → naive single LLM call (enrich purpose)
        │                         │     counts + example_ids ← SQL / code (never the LLM)
        │                         │
        │                         └── render → same tx: digest_runs 'sent'
        │                                      + outboxrepo.Insert(audience='digest')
        ▼
   notify_outbox ──→ OutboxWorker (existing) ──→ GetByTenantAudience(tenant,'raw-webhook','digest')
                                              ──→ rawwebhook adapter (HMAC POST, at-least-once)
```

New package `internal/service/digest` (worker + aggregator + renderer), new
`internal/repo/digestsubscription` + `internal/repo/digestrun`, mirroring the
reply-draft/embedding worker shape (`replydraft/worker.go:22-74`,
`Configure(pollInterval, maxAttempts)`). Wired in `cmd/attune/server.go` next to
`startReplyDraftWorker` (`server.go:239-247`).

### Data model

**`027_tenant_digest_subscriptions.sql`** — the first-class entity (+ extend the
audience enum). **No `target_id`**: delivery resolves the tenant's
`audience='digest'` raw-webhook target the same way the outbox already resolves
every target — by `(tenant_id, destination_type, audience)`
(`outbox_worker.go:131`, `GetByTenantAudience`, backed by
`UNIQUE(tenant_id, destination_type, audience)`).

```sql
CREATE TABLE IF NOT EXISTS digest_subscriptions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    enabled         BOOLEAN     NOT NULL DEFAULT TRUE,
    frequency       TEXT        NOT NULL DEFAULT 'daily' CHECK (frequency IN ('daily','weekly')),
    send_hour       SMALLINT    NOT NULL DEFAULT 9 CHECK (send_hour BETWEEN 0 AND 23),
    byweekday       SMALLINT    CHECK (byweekday BETWEEN 0 AND 6),   -- Go time.Weekday (Sun=0); weekly only
    timezone        TEXT,                       -- IANA override (validated on write); NULL ⇒ tenants.timezone
    llm_min_feedback INT        NOT NULL DEFAULT 6 CHECK (llm_min_feedback >= 1),  -- ≥ this ⇒ LLM themes; below ⇒ themeless
    send_on_empty   BOOLEAN     NOT NULL DEFAULT FALSE,
    theme_prompt    TEXT,                       -- optional LLM-naming override
    next_run_at     TIMESTAMPTZ,                -- materialized UTC instant of next fire
    last_run_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id)                          -- v1: one subscription per tenant
);
CREATE INDEX IF NOT EXISTS idx_digest_subscriptions_due
    ON digest_subscriptions (next_run_at) WHERE enabled;

-- digest-only ROUTING FILTER on the delivery target (NOT a schedule).
ALTER TABLE tenant_notify_targets DROP CONSTRAINT IF EXISTS tenant_notify_targets_audience_check;
ALTER TABLE tenant_notify_targets ADD  CONSTRAINT tenant_notify_targets_audience_check
    CHECK (audience IN ('pool', 'radar', 'all', 'digest'));
```

`audience='digest'` is added to `selectOutboxTargets`
(`enricher_outbox.go:311-329`) as a skip case so a digest target never receives
per-event traffic, and to the `notifytarget` constants
(`notify_targets.go:45-50`, add `AudienceDigest`). The operator configures one
`raw-webhook` target with `audience='digest'` in the existing notify-targets UI;
the digest subscription only owns the schedule.

**`028_digest_runs.sql`** — the exactly-once ledger (taskoutbox-shaped,
`UNIQUE(tenant_id, run_date)` to match the issue's "one per tenant per day"):

```sql
CREATE TABLE IF NOT EXISTS digest_runs (
    id              BIGSERIAL   PRIMARY KEY,
    tenant_id       TEXT        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    subscription_id UUID        NOT NULL REFERENCES digest_subscriptions(id) ON DELETE CASCADE,
    run_date        DATE        NOT NULL,            -- date in the tenant's local tz
    status          TEXT        NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending','processing','sent','skipped_empty','failed')),
    attempts        INT         NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    feedback_count  INT         NOT NULL DEFAULT 0,
    theme_count     INT         NOT NULL DEFAULT 0,
    claimed_at      TIMESTAMPTZ,
    next_retry_at   TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    last_error      TEXT        NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, run_date)
);
CREATE INDEX IF NOT EXISTS idx_digest_runs_claim
    ON digest_runs (status, next_retry_at, created_at) WHERE status IN ('pending','failed');
```

### Control flow (one tick, every 60s)

1. **Select due** — `WHERE enabled AND next_run_at <= now()` (uses
   `idx_digest_subscriptions_due`; a NULL cursor is treated as due).
2. **Resolve tz** — `time.LoadLocation(coalesce(sub.timezone, tenant.timezone))`.
   On failure: log error, advance the cursor one nominal day (avoid hot-looping
   the warning), skip. (Write-time validation makes this rare — M2.)
3. **Queue-lag guard (C4)** — compute `run_date` = `now().In(loc)` date and window
   `[from,to)` = yesterday 00:00–24:00 in `loc`. If `QueueDepthByTenant`
   (`task.go:156-164`) shows pending embedding work that could still be clustering
   window rows, **defer** (skip this tick *without* advancing the cursor, so it
   retries) up to a bounded grace (`send_hour + 2h`), after which proceed and log
   the potential gap.
4. **Claim + advance (one tx, idempotent — M3)** —
   `INSERT digest_runs(tenant_id, subscription_id, run_date, 'pending')
   ON CONFLICT (tenant_id, run_date) DO NOTHING RETURNING id`, and in the same tx
   `UPDATE digest_subscriptions SET next_run_at = <next occurrence strictly after
   now() in loc>, last_run_at = now()`. The cursor advances **whether or not** the
   claim won, so a duplicate tick/replica can't hot-loop; advancing to the next
   *future* occurrence skips missed days (no backfill).
5. **Process** the claimed row (plus any stale `pending`/`failed` rows from a
   crash, via `FOR UPDATE SKIP LOCKED` like `taskoutbox`):
   - **Totals** — `feedback_count` = `COUNT(*)` over the window by `created_at`
     (all rows, clustered or not); `urgent_count` = `is_urgent` in window.
   - **Tier on enriched-row count in window** (`enrichment_status='done'`):
     - `0` and not `send_on_empty` → `skipped_empty`, done (no LLM, no webhook).
     - `1 .. llm_min-1` → **themeless** digest (list `id`+`title` of the few rows).
     - `>= llm_min` → **themes** (below).
   - **Render** → **deliver transactionally**: mark `digest_runs` `sent` +
     `outboxrepo.Insert` (`outbox.go:65-88`) a `raw-webhook`, `audience='digest'`
     row carrying the payload; the existing `OutboxWorker` does the HMAC POST
     (`outbox_worker_send.go:28-64`) with at-least-once backoff. Failures →
     `failed` with `taskoutbox` exponential backoff.

### Themes: cluster-then-label (primary) + naive (fallback)

Contract `{themes: [{title, count, example_ids, example_titles}]}`. **Only
`title` comes from the LLM**; `count` and `example_ids` are always SQL/code-derived,
structurally removing the fabricated-count risk.

- **Clustering on** — top-N (N=3) clusters by `COUNT(*)` in-window
  (generalize `queryClusters`, `task.go:459-505`, from a `RecencyDays` lookback to
  an explicit `[from,to)` range over `created_at` — verified the current query
  windows on `created_at` and excludes `cluster_id IS NULL`). Per cluster: the
  cached `cluster_label`; `COUNT(*)`; top-2 example IDs. A same-day cluster still
  unlabeled (labeling triggers at ≥3 members and never re-runs —
  `worker.go:216`, M5) is **labeled on read** via the **existing `cluster_label`
  purpose** (C1) or rendered "unnamed theme".
- **Clustering off** — naive fallback: fetch ≤100 enriched `(id, title,
  rationale)` in window, one structured LLM call (reusing the **`enrich`
  purpose**, guaranteed configured — C1) returns themes *with the member IDs it
  assigned*; **code** groups by those IDs for counts/examples and drops any ID
  not in the window. Naive counts are assignment-derived and may not reconcile to
  `totals` (L2) — documented; this is the degraded path.

**Why reuse purposes, not invent one (C1).** `llmrouter.Complete` returns
`ErrNotConfigured` for any non-empty purpose with no `(tenant, purpose)` route
(`router.go:53-62`); only an *empty* purpose defaults to `enrich`. A new
`digest_theme` purpose would fail for every tenant until routes are seeded.
`cluster_label` (used by #114, `worker.go:252`) is already configured wherever
clustering is on; `enrich` is always configured. So the digest needs **zero new
routing config**. (Audit rows attribute to those purposes — acceptable.)

### Delivery payload

A versioned envelope mirroring `envelopeOut` (`enricher_outbox.go:354-412`), with
structured fields + a pre-rendered markdown block so a customer's Lark/Slack
incoming-webhook renders it without bespoke formatting. The `idempotency_key`
rides **in the payload** (the outbox has no dedup column, L3 — exactly-once
*enqueue* is the `digest_runs` UNIQUE, not the outbox):

```json
{
  "version": "1", "event_type": "feedback.digest",
  "tenant_id": "...", "run_date": "2026-06-12",
  "window": { "from": "...", "to": "..." },
  "totals": { "feedback": 47, "enriched": 45, "urgent": 3, "unclustered": 2 },
  "themes": [
    { "title": "checkout broken on Safari", "count": 12,
      "example_ids": [1024, 1031], "example_titles": ["...", "..."] }
  ],
  "markdown": "*Yesterday: 47 feedbacks…*",
  "idempotency_key": "digest:acme:2026-06-12"
}
```

`totals.unclustered` surfaces any window rows not yet/never clustered so a thin
themes list is never silently misleading (C4).

### Config surface (proto + handlers + Console)

Proto-first per CLAUDE.md §11. New `proto/attune/v1/digest_subscription.proto`
with `Get/Upsert/Delete` (one-per-tenant, so an upsert rather than create+list)
at `/fb/v1/console/digest-subscription` (a `SendTest` action is deferred to a
follow-up); `make proto`,
commit the regenerated Go / TS / OpenAPI. Go handlers in
`internal/handlers/console/digestsubscription/` (router + inventory test). A
Console feature `console/src/features/digest-subscription/` (form: enabled,
frequency, send hour, weekday, threshold, send-on-empty + a hint to configure the
`audience='digest'` raw-webhook target), route
`_authed.digest-subscription.tsx`, msw mocks, vitest; i18n keys; reusing the
existing `clusters` feature/route as the styling precedent. Timezone/weekday
inputs validate IANA + range on submit (M2). **No target picker** — the digest
target is the tenant's `audience='digest'` notify target (C2).

## Alternatives considered

1. **Overload `audience='digest'` as the whole mechanism** (issue's literal text).
   Rejected: a schedule + cursor + LLM config has no home on a per-event routing
   row; all three benchmark products avoided this. `digest` stays a routing
   *filter*; the schedule is its own entity.
2. **Naive single LLM call as the primary theme source.** Rejected as primary
   (kept as fallback): the LLM fabricates counts/IDs and it duplicates #114
   (CLAUDE.md §6). Cluster-then-label gives trustworthy counts and keeps Console
   clusters and the digest in agreement.
3. **A new `digest_theme` LLM purpose.** Rejected: would fail unconfigured for
   every tenant (`router.go:53-62`); reuse `cluster_label`/`enrich` for zero-config.
4. **`digest_subscriptions.target_id` FK to a notify target.** Rejected: the
   outbox resolves targets by `(tenant, dest_type, audience)`, not by id
   (`outbox_worker.go:131`) — a `target_id` would be a dead field. Use
   `audience='digest'` resolution; accept one digest target per tenant in v1.
5. **`robfig/cron/v3`.** Rejected: zero real benefit, in-memory/single-process,
   documented DST skip; the ledger (not the lib) is the guarantee. A ticker
   matches every existing worker and adds no dependency.
6. **Tick every minute and scan all tenants** (Discourse-style) instead of a
   `next_run_at` cursor. Rejected: O(tenants)/minute; the indexed cursor (PostHog)
   is cheaper and still safe because the ledger provides exactly-once.
7. **Multiple subscriptions per tenant in v1.** Deferred: collides with the
   issue's "one per tenant per day" acceptance and with the single-digest-target
   resolution. One-per-tenant now (M4); the entity already generalizes later.

## Risks / tradeoffs

- **Timezone / DST.** Compute every fire from civil fields via `time.Date(...,loc)`
  (never `+24h`); 09:00 avoids the ambiguous window; tests cover spring-forward,
  fall-back, Feb 29. Invalid IANA caught on write + handled gracefully at runtime
  (M2).
- **Async clustering lag (C4).** At 09:00 some window rows may be unclustered. The
  queue-depth guard defers within a grace window; `totals.unclustered` makes any
  residual gap visible rather than silent. Labels are point-in-time snapshots and
  never regenerate (M5) — acceptable for a daily glance; documented.
- **Multi-replica / restart double-fire.** `digest_runs UNIQUE(tenant_id,
  run_date)` claim-by-insert; cursor advanced idempotently in the same tx;
  delivery at-least-once with a payload idempotency key for the receiver.
- **LLM hallucinated counts/IDs.** Structurally prevented (counts/IDs SQL/code-
  derived); naive path validates assigned IDs against the window.
- **Naive fallback quality / count reconciliation (L2).** Degraded path for
  clustering-off tenants; documented.
- **One-subscription-per-tenant (M4).** v1 limitation; a tenant can't yet split
  daily-to-A and weekly-to-B. The schema generalizes (drop `UNIQUE(tenant_id)`,
  reintroduce a target reference) when #34 lands richer channels.
- **Inline raw-webhook router (L4).** Verify before implementing whether the
  in-memory `PushPool`/`PushRadar` router is wired (earlier exploration found
  `buildNotifier()` returns nil ⇒ not wired). If it is wired, also skip
  `audience='digest'` there; if not, `selectOutboxTargets` is the only edit.
- **Single large PR.** Full vertical slice (~5–7 person-days vs. the issue's 1.5).
  Mitigated by layered, independently-green commits; inert until a subscription
  exists.

## Implementation plan

Layered commits, each independently green:

1. **Migrations** — `027_tenant_digest_subscriptions.sql` (+ audience enum +
   `UNIQUE(tenant_id)`), `028_digest_runs.sql`.
2. **Repos** — `internal/repo/digestsubscription/` (upsert/get/delete +
   `FindDue`, `ClaimAndAdvance`), `internal/repo/digestrun/` (taskoutbox-shaped);
   generalize `queryClusters` to a `[from,to)` window; add a feedback
   totals/themeable-count query and a themeless-list query.
3. **Service** — `internal/service/digest/` worker (ticker, tz resolve, queue-lag
   guard, claim+advance), aggregator (cluster + naive, reusing `cluster_label` /
   `enrich` purposes), renderer; `audience='digest'` skip in `enricher_outbox.go`
   + `AudienceDigest` constant; metrics (`attune_digest_runs_total{status}`,
   `_themes`, `_feedback`, latency); wire in `cmd/attune/server.go`.
4. **Contract + handlers** — `proto/attune/v1/digest_subscription.proto`,
   `make proto` (commit generated Go/TS/OpenAPI), handlers under
   `internal/handlers/console/digestsubscription/` + router + inventory test;
   IANA/range validation in the upsert handler.
5. **Console** — `console/src/features/digest-subscription/` (api/components/
   tests), route `_authed.digest-subscription.tsx`, i18n, msw handlers.
6. **Docs** — `CHANGELOG.md` `### Added`; private-deploy note for the
   `audience='digest'` target setup; flip `Status` to `Accepted` → `Implemented`.

## Verification

Mirrors the issue's matrix plus the review-driven cases and attune's real-LLM bar:

- **Aggregation unit** — 100-row fixture → counts + grouping match (cluster +
  naive paths); `totals` include unclustered rows.
- **Tiering** — 0-row → `skipped_empty` (and a send when `send_on_empty`); 1–5 →
  themeless list, **no LLM call**; ≥6 → themed (C3).
- **Theme extraction (mocked LLM, `fakeLLM`)** — 30-row → 3 themes; an LLM-emitted
  bogus `count` is **ignored** for the SQL count; a hallucinated example ID is
  dropped; the call uses `cluster_label`/`enrich` purpose (C1).
- **Scheduler unit** — `next_run_at` civil math across DST spring-forward /
  fall-back and **Feb 29**; weekday selection; double-tick → one `digest_runs`
  row; cursor advances even on conflict and skips missed days (M3); **invalid IANA
  tz → skip + warn, loop survives** (M2).
- **Queue-lag** — backlogged embedding queue → run defers within grace, then
  proceeds and flags `unclustered` (C4).
- **Renderer snapshot** — JSON envelope + markdown.
- **Repo/handler** — Go integration under `test/integration/postgres/digest/`;
  console vitest with msw; one-subscription-per-tenant uniqueness (M4).
- **End-to-end** — 3 tenants in 3 timezones, controllable clock, each gets exactly
  one digest at its local send hour; restart mid-window sends no duplicate.
- **Real-LLM e2e** — one run of the cluster-label + naive theme-naming path
  against a live provider, documented in the PR.

## Review hardening

Corrections applied after a code-verified adversarial review of the first draft
(each verified at the cited location):

| # | Severity | Defect (first draft) | Fix | Verified at |
|---|---|---|---|---|
| C1 | Critical | new `digest_theme` purpose fails unconfigured | reuse `cluster_label` / `enrich` | `llmrouter/router.go:53-62`, `embedding/worker.go:252` |
| C2 | Critical | `target_id` FK unused — outbox resolves by `(tenant,dest_type,audience)` | drop `target_id`; resolve `audience='digest'` | `outbox_worker.go:131`, `notify_targets_crud.go:99-127` |
| C3 | High | `min_feedback=6` skipped 1–5-row digests (issue: skip the *LLM*) | `llm_min_feedback` tier + `send_on_empty`; 1–5 → themeless | issue text |
| C4 | High | totals undefined; unclustered rows dropped; async lag | totals `COUNT(*)`; `unclustered`; queue-lag defer | `embedding/task.go:484,156-164` |
| M1 | Med | window column unstated | window by `created_at`; themes over `enrichment_status='done'` | `embedding/task.go:484` |
| M2 | Med | timezone never validated as IANA | validate on write; graceful worker `LoadLocation` | `tenant/tenants.go:107-125` |
| M3 | Med | claim/cursor atomicity + advance unspecified | same-tx, idempotent, next-future-occurrence | `taskoutbox/queue.go:70` |
| M4 | Med | "one per tenant" vs N subscriptions | v1 `UNIQUE(tenant_id)`; ledger `UNIQUE(tenant_id,run_date)` | issue acceptance |
| M5 | Med | labels frozen; same-day small clusters unlabeled | note snapshot; label-on-read | `embedding/worker.go:216` |
| L1–L4 | Low | FK cascade / naive counts / no outbox dedup column / inline router | resolved or flagged in Risks | `outbox/outbox.go:39-88` |

## References

**OSS source** — Discourse `enqueue_digest_emails.rb`, `user_email.rb`; Zulip
`zerver/lib/digest.py`; Sentry `tasks/summaries/weekly_reports.py`,
`tasks/digests.py`; GitLab `config/schedule.yml`; Metabase `pulse_channel.clj`,
`util/cron.clj`; PostHog `subscription.py` (`next_delivery_date`,
`summary_enabled`); Grafana Reporting API `Schedule`.

**Docs** — [robfig/cron/v3](https://pkg.go.dev/github.com/robfig/cron/v3),
[Go `time`](https://pkg.go.dev/time),
[River periodic/unique jobs](https://riverqueue.com/docs/periodic-jobs),
[Transactional outbox](https://microservices.io/patterns/data/transactional-outbox.html),
[PostgreSQL `INSERT … ON CONFLICT`](https://www.postgresql.org/docs/current/sql-insert.html),
[TnT-LLM](https://dl.acm.org/doi/pdf/10.1145/3637528.3671647),
[k-LLMmeans](https://arxiv.org/pdf/2502.09667).

**attune code (verified)** — `internal/service/embedding/worker.go:213-263`
(`maybeGenerateClusterLabel`, threshold, `cluster_label` purpose),
`internal/repo/embedding/task.go:459-505` (`queryClusters`, `created_at` window),
`:286-299` (`COUNT`), `:156-164` (`QueueDepthByTenant`), `:650-664`
(`GetClusteringConfig`), `internal/service/llmrouter/router.go:53-62`,
`internal/repo/llmconfig/routes.go:77-128` (`ResolveCandidates`/`ErrNoCandidates`),
`internal/service/enrich/enricher_outbox.go:311-329` (`selectOutboxTargets`),
`:354-412` (`envelopeOut`), `internal/service/outbox/outbox_worker.go:131`
(`GetByTenantAudience`), `outbox_worker_send.go:28-64` (HMAC send),
`internal/repo/outbox/outbox.go:39-88` (`OutboxRow`/`Insert`),
`internal/repo/notifytarget/notify_targets.go:45-50`,
`internal/repo/taskoutbox/queue.go:70,120`,
`internal/service/replydraft/worker.go:22-74`, `cmd/attune/server.go:239-247`,
migrations `004:13-14` / `008` / `010` / `015` / `025:22-23,68-70` / `026`,
`internal/repo/tenant/tenants.go:107-125`.
