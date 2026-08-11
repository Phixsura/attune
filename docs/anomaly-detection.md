# Anomaly & Spike Detection

attune detects sudden changes (spikes and drops) in feedback volume so
operators act on unusual activity without writing queries (#237).

## What is monitored

Every hour, a background worker rolls daily feedback counts into
per-slice time series and judges each settled day against its own history:

| Slice | Series |
|---|---|
| `total` | all feedback per day |
| `source` | per source channel (api, web, zendesk, …) |
| `dimension` | per taxonomy value of each configured single/multi dimension (severity=critical, kind=bug, …) |
| `cluster` | per HDBSCAN cluster with ≥ `min_count` items that day (spike-only by default) |
| `cohort` | per synced cohort (active memberships) |
| `custom` | operator-defined conjunctions, e.g. source=zendesk AND severity=critical |

## How detection works

For each slice and day, the detector compares the observed count to the
**same weekday over the past 8 weeks** (this neutralizes weekly
seasonality without any model fitting):

```
med   = median(baseline)
sigma = max(MAD/0.6745, sqrt(med), 1)     # Poisson noise floor
z     = (observed − med) / sigma
spike ⇐ z ≥ threshold AND observed ≥ max(min_count, 2·med)
drop  ⇐ z ≤ −threshold AND med ≥ 5
```

- The `2·med` multiplier guard absorbs steady growth so a healthy trend
  never alerts.
- A dead stream (observed 0 with a live baseline) fires a drop.
- Fewer than 4 baseline points → no judgment (cold start is silent).
- The detector is a pure function; the Console series chart replays the
  same function, so chart bands and alerts always agree.

## Lifecycle

```
open ──2 consecutive quiet settled days──▶ resolved (automatic)
open ──same-direction hit──▶ open ("ongoing N days"; no re-notification)
open/resolved ──recompute clears the breach──▶ retracted (data correction)
```

Each **new** event also upserts a control-tower quality action
(`action_key = anomaly:<slice>`, signal `anomaly_detection`) — the human
ledger; it is never auto-resolved.

## Configuration (Console → Configuration → Anomaly detection)

| Knob | Default | Notes |
|---|---|---|
| Sensitivity | medium (z ≥ 2.5) | high = 2.0 (more alerts), low = 3.0 (fewer) |
| min_count | 10 | absolute observed floor for spikes; 0 disables the guard |
| settle_delay_hours | 3 | wait after day close before judging (late-arriving data) |
| enabled slice types | all | drop detection excludes `cluster` by default (reclustering reassigns ids) |
| notify_mode | immediate | `digest` folds new events into the daily digest; `off` keeps Console-only |
| custom slices | none | ≤20, each 1–3 AND-ed conditions over source / dimension / cohort |

Zero configuration is safe: defaults apply to every active tenant.

## Notifications

Immediate mode posts JSON to the tenant's `radar`-audience notify targets:

```json
{
  "type": "anomaly.detected",
  "slice": {"type": "dimension", "key": "dim:severity=1a2b3c4d", "display": "severity=critical"},
  "direction": "spike",
  "observed": 31,
  "expected": {"med": 12, "low": 6.2, "high": 21.4},
  "z_score": 3.8,
  "deep_link": "https://<console>/analytics/anomalies?event=<id>"
}
```

A per-tick fuse caps delivery at 20 events (top |z|) plus one summary.

## False-positive triage

1. **Low-volume noise** — raise `min_count`; counts of 3→6 are normal
   Poisson variance and are already suppressed by default.
2. **Weekly patterns** — already handled (same-weekday baseline). Monthly
   or holiday patterns are not modeled; expect alerts on unusual days.
3. **Recluster storms** — cluster drops are off by default; keep them off
   unless your clustering is stable.
4. **Growth** — the 2× multiplier guard absorbs compounding growth; if a
   fast-growing tenant still alerts, lower sensitivity to `low`.
5. **Data corrections** — GDPR deletions or backfills that erase a spike
   retract the event automatically on the next recompute.

## Operations

- Metrics: `attune_anomaly_*` (see observability/README.md).
- Worker cadence: `anomaly.interval` (default 1h); backfill throttle:
  `anomaly.backfill_tenants_per_tick` (default 10).
- Rollup retention: 400 days; run-claim retention: 90 days.
- Multi-instance safe: per-(tenant, day) claims with stale takeover.
