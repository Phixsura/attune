# Anomaly & Spike Detection for Product Signals — Design

- **Issue**: [#237](https://github.com/Phixsura/attune/issues/237) (part of #202, gap item #45)
- **Date**: 2026-08-11
- **Status**: Approved design, pending implementation plan

## 1. Summary

Detect sudden changes (spikes and drops) in feedback volume across sources,
dimensions, clusters, customer cohorts, and operator-defined custom slices.
Detected anomalies become first-class records with evidence links, feed the
control-tower quality-action ledger, notify operators through the existing
notify adapter framework, and are explorable in a new Console analytics page
with drilldown to the underlying feedback.

Competitive research (PostHog, Mixpanel, Datadog, Amplitude, Intercom,
Zendesk, Qualtrics, Medallia, Pendo, Canny, Productboard, Sprig, Grafana,
Honeycomb) converged on the shape implemented here: detect on **single-slice
series against their own history** with robust statistics; suppress noise
with minimum-volume guards; keep user knobs minimal (a sensitivity tier);
present anomalies as cards → annotated chart → drilldown to raw records;
leave cross-slice analysis to post-detection contribution breakdown.

## 2. Decisions

| Decision | Choice |
|---|---|
| Slice scope | Fixed slice set (total / source / dimension / cluster / cohort) **plus** per-tenant custom slices (max 20) |
| Algorithm | Daily buckets; same-weekday robust baseline (median/MAD over past 8 weeks, ≥4 points) with a Poisson noise floor; z-score with 3 sensitivity tiers |
| Persistence | New `anomaly_events` detail table **linked to** upserted `feedback_quality_actions` rows |
| Notification | Immediate delivery via `notify.Transport` to `radar`-audience targets, or aggregation into the daily digest, or off (per-tenant `notify_mode`) |
| Execution | Dedicated background worker (digest-worker skeleton: claim / heartbeat / drain / `ProcessOnce(ctx, now)`), **recompute-window rollup** (no incremental cursor) |

### Why recompute-window rollup, not a cursor

`user_feedback` rows are enriched asynchronously (severity/dimension values
land in `enriched_attrs` minutes-to-hours after insert, with retries), cluster
ids are assigned late and reassigned on recluster, cohort memberships change
retroactively, and GDPR deletes remove rows in place. A cursor over feedback
ids would systematically undercount dimension slices. Instead, each worker
tick recomputes the **last 3 days** of buckets per tenant from scratch (one
aggregate query per slice family, all hitting
`idx_user_feedback_tenant_created`), and detection only runs on buckets that
are closed **plus a settle delay** (default 3h — the Medallia/Qualtrics
late-data pattern). Buckets older than the 3-day window are frozen; drift
outside the window is accepted and documented (aggregate counts carry no PII).

## 3. Slice model

```
slice_type ∈ {total, source, dimension, cluster, cohort, custom}
slice_key (storage + quality-action action_key reuse; URL-safe):
  total     → "total"
  source    → "source:" + source                     e.g. source:zendesk
  dimension → "dim:" + name + "=" + sha256(value)[:8]  (hash guards length/charset)
  cluster   → "cluster:" + cluster_id (uuid)
  cohort    → "cohort:" + cohort_id (uuid)
  custom    → "custom:" + custom_slice_id (uuid)
slice_display: human-readable snapshot ("severity=critical", cluster label, cohort name)
```

- **dimension** values come from `user_feedback.enriched_attrs` (JSONB;
  `enriched_severity` was folded into dimensions by migration 014). Single
  dims read `->> name`; multi dims expand `jsonb_array_elements_text` (one
  feedback may count in several value buckets — same semantics as
  `feedback_console_stats.go`). Only rows with `enrichment_status='enriched'`
  enter dimension slices; total/source are unconditional.
- Sliced dimensions: those with non-empty taxonomy (bounded value domain).
  Freeform multi dims (labels-style) are not sliced. Per-dimension guard:
  if active values > 50, keep top 50 by 30-day volume and emit a metric.
- **cluster**: buckets only for clusters with day-count ≥ `min_count`
  (natural top-N). Cluster label snapshotted into `slice_display`.
- **cohort**: JOIN `cohort_memberships` (`left_at IS NULL`) on `subject_key`
  (same join as `feedback_console.go` cohort filter).
- **custom**: conjunction of ≤3 conditions over whitelisted fields
  (source / dimension / cohort), ≤10 values each, ≤20 slices per tenant.
  Definitions compile to parameterized SQL only — never string-built.

Series-count guard: enabled slices are capped at 500 distinct keys per tenant
(config validation, measured over 30 days) with a hard runtime cap of 1000
per detection tick (truncate + metric + log).

## 4. Detector (pure function, `internal/service/anomaly`)

```go
type DetectorConfig struct {
    ZThreshold        float64 // high 2.0 / medium 2.5 (default) / low 3.0
    MinCount          int64   // spike observed floor, default 10
    MinBaselinePoints int     // default 4
}
func Detect(baseline []int64, observed int64, cfg DetectorConfig) Verdict
```

```
med   = median(baseline)                    // baseline = same weekday, past 8 weeks
mad   = median(|x − med|)
sigma = max(mad/0.6745, sqrt(max(med,1)), 1.0)   // Poisson noise floor; no MAD=0 degeneracy
z     = (observed − med) / sigma
spike ⇐ z ≥ ZThreshold AND observed ≥ max(MinCount, 2·med)   // multiplier guard vs. trend growth
drop  ⇐ z ≤ −ZThreshold AND med ≥ 5                          // observed may be 0 (stream stopped)
expected_low/high = med ∓ ZThreshold·sigma (low clipped at 0)
baseline < MinBaselinePoints → insufficient_data (no verdict, no alert)
```

Rationale:
- Same-weekday baseline neutralizes weekly seasonality without decomposition
  (Datadog weekly-seasonality semantics; PostHog first-differencing intent).
- `sqrt(med)` floor: count series have variance ≈ mean (Poisson); a sampled
  MAD of 8 points cannot be trusted below that.
- The `2·med` multiplier guard absorbs steady growth (+10%/week compounds to
  ~2.1× over 8 weeks) so V1 needs no explicit trend fit; genuine spikes
  (≥3×) pass unharmed. Explicit trend fitting (Theil-Sen) is out of scope.
- Missing buckets count as 0 (`count_default_zero=true` semantics).
- Deterministic: no clock, no IO; tests feed fixed series.

Edge matrix (each is a unit test):
| Baseline | Observed | Expect |
|---|---|---|
| all zeros | 15 | spike (sigma floor 1.0) |
| [3,2,4,3] | 6 | no verdict (2·med passes but z≈1.7 < 2.5) — low-count doubling immune |
| steady +10%/wk | trend value | no verdict (multiplier guard) |
| [10,10,10,10] | 25 | spike (MAD=0 → sigma=√10) |
| med ≥ 5 | 0 | drop (stream-stopped alert) |
| < 4 points | any | insufficient_data |

Sensitivity tiers map to `ZThreshold` only; `min_count` and
`settle_delay_hours` are separately configurable. Defaults are safe:
medium (z ≥ 2.5), min_count 10, drop guard med ≥ 5.

## 5. Data model (migration 146)

**`feedback_volume_buckets`** — the rollup
- PK `(tenant_id, bucket_date DATE, slice_type, slice_key)`; `config_version INT`
- `feedback_count BIGINT`, `sample_feedback_ids BIGINT[]` (≤5, joined against
  live rows at read time), `slice_display`, `computed_at`
- `bucket_date` = civil date in the tenant timezone (`tenants.timezone`,
  migration 008); index `(tenant_id, slice_type, slice_key, bucket_date DESC)`
- Retention 400 days (covers 8-week baselines + a year of lookback);
  cleanup folded into the worker tick.

**`anomaly_events`** — detection detail (lifecycle below)
- `id UUID`, tenant, slice triple + display, `direction (spike|drop)`,
  `first_bucket_date`, `last_bucket_date`, `observed`, `expected_med/low/high`,
  `z_score`, `status (open|resolved|retracted)`,
  `quality_action_id UUID NULL → feedback_quality_actions`,
  `evidence JSONB` ({sample_ids, contribution[]}), timestamps
- Partial unique index `(tenant_id, slice_type, slice_key, direction) WHERE
  status='open'` — one live event per series+direction.

**`tenant_anomaly_configs`** — per-tenant knobs (safe defaults; zero-config OK)
- `sensitivity (high|medium|low)`, `min_count`, `settle_delay_hours`,
  `enabled_slice_types TEXT[]`, `drop_enabled_slice_types TEXT[]`
  (cluster excluded from drops by default — reclustering reassigns ids and
  would storm drop alerts), `notify_mode (immediate|digest|off)`,
  `detection_enabled BOOL`, `config_version INT`, `backfilled_at`
- Timezone is **not** duplicated here — `tenants.timezone` is authoritative;
  the worker treats a tz change as a config-version bump (full recompute).

**`tenant_anomaly_custom_slices`** — `name`, `definition JSONB`, `enabled`,
`last_error` (invalid definitions auto-disable with a visible error — the
PostHog invalid-config pattern); UNIQUE (tenant_id, name).

**`anomaly_detection_runs`** — PK `(tenant_id, bucket_date)`, claim columns
(status/claimed_by/claimed_at/heartbeat) — digest_runs concurrency pattern;
doubles as run-once idempotency and audit. Retention 90 days.

New audit actions: `anomaly_config.update`, `anomaly_custom_slice.create/delete`.

## 6. Lifecycle state machine

```
(none) ──hit──▶ open ──2 consecutive normal settled buckets──▶ resolved (auto)
open ──same-direction hit──▶ open (last_bucket_date advances; "ongoing N days";
                              no new row, no re-notification)
open/resolved ──recompute clears the breach──▶ retracted (kept for audit;
                              Console badge "retracted after data correction")
```

- Quality action linkage on **NEW** events only:
  `action_key = "anomaly:" + slice_key`, `signal = "anomaly_detection"`,
  severity `alert` when z ≥ 2×threshold else `watch`,
  `target_path = /analytics/anomalies?event=<id>`, evidence carries event id.
  Upsert semantics give dedup + `last_seen_at` refresh; a resolved/dismissed
  action re-opens on recurrence. Actions are **not** auto-resolved (human
  ledger); events are.
- Notification fires **once**, on NEW, per `notify_mode`. Retractions are
  Console-only (no retraction pushes — noise).

## 7. Worker

Digest-worker skeleton (claim / heartbeat 90s / drain / `ProcessOnce(ctx, now)`
with injected clock). Config: `anomaly: {interval: "1h",
backfill_tenants_per_tick: 10}` in config.yaml. Per tenant per tick:

1. **Backfill** (first run / config_version or tz change): recompute 90 days
   of buckets; throttled to N tenants per tick; `backfilled_at` gates
   detection; metric `anomaly_backfill_pending_tenants`.
2. **Rollup recompute** of the last 3 days (§2 rationale). One transaction:
   GROUPING-SETS/UNION aggregate per slice family → upsert → anti-join DELETE
   for buckets that vanished (GDPR zeroing).
3. **Detection** for each settled unclaimed date: claim
   (`INSERT … ON CONFLICT DO NOTHING` + stale reclaim), enumerate slices
   (detection-day buckets ∪ slices seen in the 8 baseline dates — the union
   makes drops of vanished slices visible), fetch baselines
   (`bucket_date = ANY($8dates)` per slice), run `Detect`, apply state
   machine, upsert quality actions, enqueue notifications, mark done.
4. **Reconcile** open events (resolve / retract).
5. Retention cleanup (buckets 400d, runs 90d).

Failure isolation: per-tenant errors recorded in `runs.error`, other tenants
unaffected; safegoroutine wrapper; notification failures are logged +
metriced, never block detection (alerts are not a critical path).
Notification storm fuse: >20 NEW events in one tenant-tick → send top 20 by
|z| plus one summary message (PostHog breach-cap pattern).

## 8. Notifications

Immediate mode: `notify.Transport` (digest sender pattern) to
`tenant_notify_targets` with audience `radar`.

raw-webhook payload (wire contract):
```json
{
  "type": "anomaly.detected",
  "tenant_id": "…", "event_id": "…",
  "slice": {"type": "dimension", "key": "dim:severity=1a2b3c4d", "display": "severity=critical"},
  "direction": "spike",
  "bucket_date": "2026-08-10",
  "observed": 31, "expected": {"med": 12, "low": 6.2, "high": 21.4},
  "z_score": 3.8,
  "deep_link": "https://<console>/analytics/anomalies?event=<id>"
}
```
lark/slack render via the digest card builder (title = direction + display,
body = magnitude sentence + top contributions, button = deep link).

Digest mode: `digest.Aggregator` gains an optional `anomalyReader`
(`OpenEventsInWindow`) and the render adds an "Anomalies" section; NEW events
skip immediate delivery. Config validation warns when digest mode is chosen
without a digest subscription (falls back to immediate).

## 9. API (`proto/attune/v1/anomaly.proto`)

```
AnomalyService:
  ListAnomalies        GET  /fb/v1/console/anomalies            (viewer)
  GetAnomalySeries     GET  /fb/v1/console/anomalies/series     (viewer)
  GetAnomalyEvidence   GET  /fb/v1/console/anomalies/{event_id}/evidence (viewer)
  GetAnomalyConfig     GET  /fb/v1/console/anomaly-config       (viewer)
  UpdateAnomalyConfig  POST /fb/v1/console/anomaly-config       (admin + audit)
```

- `GetAnomalySeries(slice_type, slice_key, days ≤ 180)` returns per-day
  `{date, count, expected_med, expected_low, expected_high, is_anomalous}` —
  the server replays the **same `Detect` function** point-by-point, so chart
  annotations and alerts can never disagree.
- `GetAnomalyEvidence(event_id)` returns the feedback rows for the anomaly
  window+slice (reusing the existing feedback list projection → tenant
  isolation for free) plus the contribution breakdown.
- Contribution (computed once at event creation, stored in evidence): group
  the anomalous slice's day-feedback by source and each single dimension;
  `share_v = (obs_v − exp_v) / (obs_total − exp_total)` with expectations
  from the same same-weekday baselines; keep |share| ≥ 15% top-3; all below
  → `{spread: true}` ("broadly distributed").
- Handler layout follows `handlers/console/feedback/quality_actions.go`
  (interface-sliced store, dispatcher.Bind, ErrorResponse enum codes,
  rbac.RequireViewer/RequireAdmin, enrichconfig-style SetAuditLogger).

### Config validation (UpdateAnomalyConfig)

| Field | Rule |
|---|---|
| sensitivity | enum high/medium/low |
| min_count | 0–10000 (0 disables the guard; documented warning) |
| settle_delay_hours | 0–48 |
| enabled/drop_enabled_slice_types | subset of the full set; drop ⊆ enabled |
| notify_mode | enum; digest without subscription → 200 + warning |
| custom_slices | ≤20; each: 1–3 conditions, whitelisted fields, values validated against SourceSet / DimensionSet taxonomy / tenant cohorts |
| series estimate | distinct slice_key over 30 days ≤ 500, else reject with the count |

## 10. Console

```
features/anomalies/
  api/…                      TanStack Query wrappers for the 5 RPCs
  components/AnomalyCard     direction arrow, slice_display, "observed 31,
                             expected 12 (6–21)", ongoing-days badge,
                             severity tone (control-tower SignalSeverity palette)
  components/AnomalySeriesChart  SVG line + expected-interval band + red
                             anomaly dots (existing stats-chart pattern,
                             dataviz palette)
  components/ContributionBars    top-3 horizontal bars + "broadly distributed"
routes/_authed.analytics.anomalies.tsx   list (open/resolved/all tabs) →
                             detail drawer (chart + contributions +
                             "view N feedback items" → /feedback filtered)
control-tower                anomaly lane appears automatically (existing
                             quality-action rendering, signal=anomaly_detection)
                             + open-anomaly count metric in the hero
routes/_authed.configuration.anomaly-detection.tsx   sensitivity select,
                             min_count, settle delay, slice toggles,
                             notify_mode, custom-slice table (add/remove,
                             error badges for auto-disabled definitions)
i18n                         `anomalies.*` keys, en inline + zh-CN.json
```

Coverage thresholds (vite.config.ts), dependency-cruiser layering, and biome
apply as usual.

## 11. Observability & security

- Metrics: `anomaly_rollup_duration_seconds`, `anomaly_detect_slices_total`,
  `anomaly_events_created_total{direction}`, `anomaly_notify_failures_total`,
  `anomaly_worker_lag_seconds`, `anomaly_backfill_pending_tenants`,
  `anomaly_slices_truncated_total`.
- Logging via logext facade; never log slice values verbatim (may contain
  user content) — slice_type + hashed key only.
- Custom-slice definitions compile to parameterized SQL against whitelisted
  fields; no string concatenation.
- GDPR: buckets store counts + feedback-id arrays only; evidence sample ids
  are joined against live rows at read time; recompute window self-corrects
  recent deletions; no changes needed to export/delete jobs.

## 12. Testing

| Layer | Cases |
|---|---|
| Detector unit | full §4 edge matrix, three tiers, drop/stream-stop, insufficient data |
| Rollup integration (pg) | per-slice-family counts (multi-dim expansion, cohort join, custom conjunction), 3-day recompute idempotency, GDPR delete zeroing, tz change full recompute, retention cleanup |
| Worker integration | injected spike → `ProcessOnce(fixed now)` → assert event + action + notify payload; ONGOING no re-notify; resolve/retract; two instances claim once; no detection before settle delay; DST-week bucket boundaries |
| Handler | binding, validation, roles, error-code enum |
| Console vitest | card/chart/contribution rendering, config form, route wiring |
| Quality gates | lizard CCN ≤ 15 / NLOC ≤ 100 (detector is small pure functions), jscpd, ptrext, slog lint, lint-errorcode, buf lint/breaking |

## 13. Delivery plan (PR sequence, each independently green)

1. `feat(anomaly): detector core` — pure functions + unit tests (no DB)
2. `feat(anomaly): rollup schema + repo` — migration 146 + rollup repo + pg tests
3. `feat(anomaly): worker + quality actions + notifications` — state machine + server wiring + config.yaml
4. `feat(anomaly): console API` — proto + handlers + generated artifacts
5. `feat(anomaly): console UI` — pages + control-tower lane + i18n
6. `feat(anomaly): custom slices + config UI` — config surface + audit

Each PR updates CHANGELOG `[Unreleased]` (Added). Version impact: MINOR.

## 14. Out of scope (follow-up issues)

Hourly granularity; explicit trend fitting (Theil-Sen); pre-save threshold
simulator; per-user alert subscriptions; feedback-driven adaptive
sensitivity; LLM-narrated root-cause summaries.

## 15. Acceptance criteria (from #237, mapped)

- *Operators see and investigate spikes without manual queries* → control-tower
  lane + `/analytics/anomalies` + immediate/digest notifications.
- *Anomaly records link evidence and affected tenant/source/dimension* →
  `anomaly_events.evidence` (sample ids + contribution) + slice triple +
  `GetAnomalyEvidence` drilldown.
- *Thresholds configurable and safe by default* → `tenant_anomaly_configs`
  with zero-config defaults (medium sensitivity, min_count 10, settle 3h,
  cluster drops off).
- *Detector output deterministic in tests* → pure `Detect`, injected clock,
  fixed-series test matrix.
