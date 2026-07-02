# Classification quality dashboard and drift detection

| | |
|---|---|
| **Issue** | [#161](https://github.com/Phixsura/attune/issues/161) |
| **Status** | Implemented |
| **Started** | 2026-07-02T01:25:07+08:00 |
| **Related** | [#21 semantic understanding](../06/2026-06-10-semantic-understanding-layer.md), [#24 LLM confidence and cost](../06/2026-06-11-llm-confidence-cost.md), [#83 off-list eval](../06/2026-06-20-eval-suggested-attrs.md), [#159 terminal failure workbench](../06/2026-06-30-terminal-failure-workbench.md) |

---

## Problem

attune lets operators configure Dimensions, taxonomy values, prompt policy, and
LLM routing. Those controls are powerful, but the product does not yet give
operators a quality surface that answers:

- Did a Dimension's distribution suddenly change?
- Which value changed, and by how much?
- Are low-confidence classifications rising?
- Is the classifier producing unknown Dimensions or off-list taxonomy values?
- Are parse failures or terminal enrichment failures increasing?
- Which rows should an operator review next?

The current system has useful raw signals, but they are scattered:

- `user_feedback.classification_confidence` is visible per row.
- `semantic_extraction_runs.dropped_attrs` stores gate diagnostics.
- Prometheus counts suggested and dropped attributes.
- the feedback stats surface has monthly usage and top values.
- the terminal failure workbench clusters terminal enrichment failures.

Those pieces do not yet form a dashboard. Issue #161 asks for stored aggregates,
drift detection, warnings, and links from low-quality samples back into the
feedback workbench so operators can spot taxonomy drift without reading rows one
by one.

## Goals / Non-goals

### Goals

- Add stored quality aggregates for Dimension/value distributions, unknown or
  off-list suggestions, confidence trends, parse failures, and terminal
  enrichment failures.
- Compare a current time window against a baseline window and return
  explainable drift signals per Dimension and value.
- Add a Console quality dashboard with time-window, baseline, tenant, source,
  model, and channel filters where the current auth model and persisted facts
  allow them.
- Return warnings when drift, low-confidence rate, off-list rate, parse failure
  rate, or terminal failure rate crosses thresholds.
- Link low-quality samples into the feedback workbench with stable filters and
  sample row IDs.
- Keep Prometheus metrics bounded enough for production dashboards.
- Cover rollup, drift math, handler/API shape, and Console rendering with tests.

### Non-goals

- Do not claim classification accuracy without human labels or eval datasets.
  This issue reports proxy quality signals, not ground-truth correctness.
- Do not automatically edit tenant taxonomy, prompt policy, or Dimensions.
- Do not add a model retraining workflow or bulk re-enrichment workflow.
- Do not replace the terminal failure workbench; reuse it for terminal
  enrichment failures.
- Do not add unbounded Prometheus labels such as raw values, raw errors, prompt
  text, or sample IDs.
- Do not build a natural-language insights report generator. The dashboard is an
  operational quality surface with drill-down actions.

## Industry alignment findings

The cross-product pattern is consistent: leading support, product-feedback, and
ML-observability systems treat classification quality as a time-windowed,
explainable, drill-down workflow. They do not ask operators to infer drift from
individual rows.

| Product | Relevant practice | attune decision |
|---|---|---|
| [Zendesk Intelligent Triage](https://support.zendesk.com/hc/en-us/articles/4550640560538-Automatically-classifying-tickets-with-intelligent-triage) and its [dashboard](https://support.zendesk.com/hc/en-us/articles/7934127855002-Overview-of-the-Intelligent-triage-dashboard) | Classifies business fields such as intent, sentiment, and language, then exposes activity and quality recommendations. | Treat attune Dimensions as the product objects. Show distribution, confidence, and recommendation cues per Dimension. |
| [Intercom Topics Explorer](https://www.intercom.com/help/en/articles/11390087-use-the-topics-explorer-to-see-what-s-driving-volume), [CX Score](https://www.intercom.com/help/en/articles/10495092-understand-customer-experience-at-scale-with-the-cx-score), and [Trends](https://www.intercom.com/help/en/articles/11875255-how-to-use-trends-to-spot-shifts-in-your-support-data) | Combines topic volume, quality score, and trend changes into drill-down analytics. | Put drift, confidence, and samples on the same Console surface instead of separate reports. |
| [Salesforce Einstein Case Classification](https://help.salesforce.com/s/articleView?id=service.cc_service_what_is.htm&language=en_US&type=5) and [performance tracking](https://help.salesforce.com/s/articleView?id=service.cc_service_performance.htm&language=en_US&type=5) | Separates prediction automation from model performance monitoring and setup constraints. | Keep quality monitoring read-only. Do not let warning generation silently change classification behavior. |
| [Qualtrics Text Analytics](https://www.qualtrics.com/support/omnichannel-listening/text-analytics-xm/text-analytics-overview/) and [sentiment tuning](https://www.qualtrics.com/support/xm-discover/designer/sentiment/sentiment-tuning-designer/) | Presents topics, sentiment, and tuning as governed taxonomy work, not only model internals. | Link off-list and low-confidence samples to review surfaces; do not auto-promote values. |
| [Medallia Text Analytics](https://www.medallia.com/platform/text-analytics/) | Emphasizes trend detection across customer feedback themes and operational drill-down. | Make drift explanations value-level and actionable. |
| [Sprinklr sentiment and theme analysis](https://www.sprinklr.com/help/articles/ai-enrichments/detect-the-sentiment-present-in-customer-messages-accurately/645b5278e66f2e36b45187ac/) | Uses sentiment/theme monitoring and alerts for changes in customer conversation patterns. | Return alert-grade warnings and expose low-cardinality metrics that can feed external alerting. |
| [Pendo Listen](https://support.pendo.io/hc/en-us/articles/18159674293531-Overview-of-Pendo-Listen) and [AI feedback insights](https://support.pendo.io/hc/en-us/articles/37717114561819-Explore-feedback-with-AI-in-Listen) | Turns feedback into AI-discovered themes with volume and sample exploration. | Always pair aggregate signals with sample feedback IDs and workbench links. |
| [Productboard Pulse](https://support.productboard.com/hc/en-us/articles/34627982878483-Generate-insight-reports-with-Productboard-Pulse) | Builds insight reports from product feedback while preserving evidence links. | Keep quality cards evidence-backed and traceable to rows. |
| [Arize drift tracing](https://arize.com/docs/ax/machine-learning/machine-learning/how-to-ml/drift-tracing) | Compares production distributions with baselines and traces high-impact drift features. | Use current-vs-baseline windows and return top contributing values. |
| [Evidently data drift](https://docs-old.evidentlyai.com/presets/data-drift) and [classification performance](https://docs-old.evidentlyai.com/presets/class-performance) | Separates unsupervised drift metrics from supervised performance metrics. | Implement unsupervised drift now; keep supervised accuracy out until attune has labels. |

Cross-checks from WhyLabs, Fiddler, and NannyML point to the same design guard:
drift detection must define a baseline, minimum volume, statistical method, and
actionable slices. For categorical Dimensions, Jensen-Shannon distance and share
delta are a practical first pair; PSI can be included as a secondary diagnostic
when operators need a familiar monitoring score.

## Current code reality

| Area | Current state | Consequence for #161 |
|---|---|---|
| Dimension grammar | [`internal/domain/dimension.go`](../../../../internal/domain/dimension.go) owns Dimension names, single/multi kinds, taxonomy, urgent sets, and `FilterAttrsWithDiagnostics`. | Quality rollup can reuse the same gate semantics instead of inventing a second taxonomy parser. |
| Current classification snapshot | [`user_feedback.enriched_attrs`](../../../../internal/infra/database/migrations/019_semantic_understanding.sql) stores the fast current-state attrs used by list/detail/stats. | The dashboard should link samples back to the current row snapshot, while historical quality buckets use classification-event facts. |
| Extraction evidence | [`semantic_extraction_runs`](../../../../internal/infra/database/migrations/019_semantic_understanding.sql) stores model, prompt version, attrs, confidence, rationale, dropped attrs, and `created_at`. | Successful-classification rollup should consume semantic runs as append-only events instead of treating `user_feedback.created_at` as the only time axis. |
| Confidence | [`user_feedback.classification_confidence`](../../../../internal/infra/database/migrations/022_llm_confidence_cost.sql) and [`semantic_extraction_runs.confidence`](../../../../internal/repo/feedback/semantic_extraction.go) are already persisted. | Confidence trend and low-confidence sample links do not require a new per-row field. |
| Attr gate metrics | [`internal/service/enrich/enricher.go`](../../../../internal/service/enrich/enricher.go) increments suggested and dropped attr counters after `applyAttrsGate`. | Prometheus already has event-level signal; #161 adds stored time-window analytics and drill-down. |
| LLM routing facts | [`llm_audit`](../../../../internal/infra/database/migrations/022_llm_confidence_cost.sql) stores logical model; managed-channel migrations add provider model and channel ID. | Model/channel filters need an explicit fact-source decision. Prefer copying the chosen channel/provider snapshot onto semantic extraction evidence instead of relying on fuzzy audit joins. |
| Existing console stats | [`internal/repo/feedback/feedback_console_stats.go`](../../../../internal/repo/feedback/feedback_console_stats.go) has usage by day, top values by Dimension, and urgent count. | Reuse query style, but add a quality-specific aggregate contract instead of overloading generic stats. |
| Feedback list/detail | [`proto/attune/v1/ingest.proto`](../../../../proto/attune/v1/ingest.proto) exposes confidence on rows, but the list request currently lacks confidence, created-window, and ID-set filters. | Low-quality workbench links require a small feedback-list API extension, not only a dashboard route. |
| Terminal failures | [#159](../06/2026-06-30-terminal-failure-workbench.md) adds a dedicated terminal failure workbench and summary behavior over current terminal rows. | #161 should reuse the workbench for remediation while adding event facts for parse and terminal failure rates. |

## Acceptance criteria mapping

| #161 acceptance criterion | Proposal mechanism | Verification signal |
|---|---|---|
| Operators can spot sudden taxonomy drift without reading individual feedback rows. | Stored current-vs-baseline aggregates, Dimension drift severity, and top contributing values. | Unit tests for drift math plus handler tests for current, baseline, watch, alert, and insufficient-data states. |
| Dashboard explains which Dimension/value changed. | Each drift response includes Dimension name, value, current share, baseline share, share delta, JS contribution, and sample feedback IDs. | Console tests assert value-level explanation text and links render for drifted Dimensions. |
| Low-quality samples are actionable. | Low-confidence, off-list, parse-failure, and terminal-failure cards link to feedback and terminal workbench filters. | Handler and route tests assert generated links include stable time-window and quality filters. |
| Tests cover aggregation and display of drift states. | Add Go tests for rollup and drift; add Console tests for normal, warning, alert, and empty states. | `go test` and `pnpm vitest run` fail on aggregation or UI regression. |

## Proposal

### 1. Add stored quality aggregates

Add two fact-source changes and three aggregate objects.

First, make the event facts explicit:

- Extend `semantic_extraction_runs` with nullable routing snapshot fields needed
  for success rollup: `source`, `logical_model`, `provider_model`,
  `channel_id`, and `channel_name`. `model` remains supported as the legacy
  logical-model field. The enricher should write these values from the selected
  LLM route at the same time it writes attrs, confidence, rationale, and dropped
  attrs. This avoids fuzzy joins from semantic runs to `llm_audit`.
- Add `classification_quality_failure_events` for enrichment failure attempts:
  `id`, `tenant_id`, `feedback_id`, `event_at`, `event_kind`,
  `reason_class`, `logical_model`, `provider_model`, `channel_id`,
  `channel_name`, `source`, `attempts`, and `terminal`. `MarkFailed` should
  insert one event from the same SQL statement that updates
  `user_feedback.enrichment_status`, using a writable CTE:

  ```sql
  WITH updated AS (
      UPDATE user_feedback
      SET ...
      WHERE ...
      RETURNING id, tenant_id, source, enrichment_attempts, ...
  )
  INSERT INTO classification_quality_failure_events (...)
  SELECT ...
  FROM updated
  RETURNING terminal, tenant_id;
  ```

  Owner-fenced failure paths must insert no event when the `UPDATE` matches no
  row. This table is the source for parse-failure and terminal-failure rates.
  The terminal failure workbench can continue reading current terminal rows from
  `user_feedback`.

Failure event enums:

| Field | Allowed values |
|---|---|
| `event_kind` | `attempt_failed` |
| `reason_class` | `llm_err`, `parse_err`, `other_err` |

Rows migrated from older data can use available failure snapshot columns and the
best available event timestamp. New writes must use database time for
`event_at` so event ordering is consistent with the status update.

The dashboard uses classification event time:

- successful classification event time is `semantic_extraction_runs.created_at`
- failure event time is `classification_quality_failure_events.event_at`
- feedback `created_at` remains a row attribute and sample-link filter, not the
  rollup cursor

This distinction prevents retries and re-enrichment from being missed. A
re-enriched row produces a new semantic extraction run and therefore a new
classification event in the bucket where the new classification was produced.

`classification_quality_value_buckets` stores per-Dimension value counts:

| Column | Meaning |
|---|---|
| `tenant_id` | Tenant scope. |
| `bucket_start` | UTC bucket for successful classification event time. |
| `bucket_width` | `hour` or `day`. |
| `dimension_name` | Dimension key from tenant config or dropped-attr diagnostics. |
| `dimension_value_hash` | SHA-256 of the normalized full value; empty string for Dimension-level rows. |
| `dimension_value_display` | Capped display copy of the configured value or off-list value; empty string for Dimension-level rows. |
| `value_status` | `configured`, `off_list`, `unknown_dimension`, or `all`. |
| `source` | Bounded feedback source key; empty string means unknown or unset, not all sources. |
| `logical_model` | Logical model key used for the classification; empty string means unknown or unset. |
| `provider_model` | Provider model key when known; empty string means unknown or unset. |
| `channel_id` | Bounded LLM channel key when known; empty string means unknown or unset. |
| `appearance_count` | Number of value appearances in this bucket. For multi-value Dimensions, one classification event can contribute more than one appearance. |
| `event_count` | Number of successful classification events that contributed this Dimension/value row. |
| `confidence_count` | Number of appearances with a confidence value. |
| `confidence_sum` | Sum of confidence values for average confidence. |
| `low_confidence_count` | Count at or below the configured threshold. |
| `sample_feedback_ids` | Hard-capped sample IDs for drill-down. |
| `created_at`, `updated_at` | Aggregate maintenance timestamps. |

Primary key:

```text
(tenant_id, bucket_width, bucket_start, dimension_name, dimension_value_hash,
 value_status, source, logical_model, provider_model, channel_id)
```

`classification_quality_signal_buckets` stores bucket-level quality signals:

| Column | Meaning |
|---|---|
| `tenant_id`, `bucket_start`, `bucket_width`, `source`, `logical_model`, `provider_model`, `channel_id` | Same scope fields as value buckets. |
| `classification_event_count` | Successful semantic extraction runs in the bucket. |
| `failed_attempt_count` | Failure events in the bucket. |
| `parse_failure_count` | Failure events whose `reason_class` is `parse_err`. |
| `terminal_failure_count` | Failure events with `terminal = true`. |
| `terminal_parse_failure_count` | Terminal failure events whose `reason_class` is `parse_err`. |
| `off_list_count` | Dropped values for known Dimensions. |
| `unknown_dimension_count` | Dropped values for unknown Dimensions. |
| `confidence_count`, `confidence_sum`, `low_confidence_count` | Bucket-level confidence signals. |
| `sample_feedback_ids` | Hard-capped sample IDs for low-quality drill-down. |
| `created_at`, `updated_at` | Aggregate maintenance timestamps. |

Primary key:

```text
(tenant_id, bucket_width, bucket_start, source, logical_model, provider_model,
 channel_id)
```

`classification_quality_rollup_state` stores independent append-only cursors per
tenant and bucket width:

- `last_semantic_run_id` for successful classification events
- `last_failure_event_id` for failure events
- `recompute_from` for bounded repair jobs that intentionally rebuild recent
  buckets

Do not cursor by `bucket_start` or `user_feedback.created_at` alone; those keys
miss late completions, retries, and re-enrichment.

Rollup rules:

- Use UTC hour buckets for recent windows and UTC day buckets for wider windows.
- Consume successful classification facts from `semantic_extraction_runs` in ID
  order.
- Consume failure facts from `classification_quality_failure_events` in ID order.
- Join to `user_feedback` only for stable row attributes such as source and
  sample display data when those fields are not already copied onto the fact.
- Store sample IDs with a small fixed cap per bucket and signal type.
- Normalize off-list values with the same value normalization used by
  `FilterAttrsWithDiagnostics`; store full identity as a hash and only keep a
  capped display copy in aggregate rows.
- Store only atomic source/model/channel scopes. "All sources", "all models",
  and "all channels" views are query-time rollups over the atomic rows.
- Store rich Dimension/value detail in SQL, not Prometheus labels.

Cardinality and privacy controls:

- `dimension_name` for configured Dimensions uses the existing
  `^[a-z][a-z0-9_]{0,30}$` grammar. Unknown Dimension names that do not match
  that grammar are grouped under `__invalid_dimension__`.
- `dimension_value_display` is capped at 160 UTF-8 bytes after normalization.
  The hash keeps the identity stable without indexing arbitrarily long text.
- Off-list and unknown-Dimension drill-downs return top values by count plus an
  `other_count`; they do not return every unique hallucinated value.
- Sample IDs are selected as the latest events by `(event_at DESC, feedback_id
  DESC)` and capped at 5 per aggregate row and 50 per dashboard response.
- Sample lookups must respect soft deletes and tenant scope. If a sampled row was
  deleted or erased, the API omits the row and keeps the aggregate count.
- Aggregate rows must not store raw feedback content, raw errors, prompt text,
  credentials, or source metadata.

Distribution semantics:

- Single-value Dimensions use event share:
  `value.event_count / dimension_all.event_count`.
- Multi-value Dimensions use appearance share:
  `value.appearance_count / sum(value.appearance_count for the Dimension)`.
- Multi-value Dimension coverage is reported separately:
  `dimension_all.event_count / successful classification events`.
- The `value_status=all` row uses empty `dimension_value` and stores the
  Dimension-level `event_count`; it is not included in value-distribution
  distance calculations.

Backfill and retention:

- Initial backfill should cover the last 90 days by default, bounded by a config
  value so large installs can choose a smaller window.
- Hour buckets are retained for 30 days.
- Day buckets are retained for 400 days.
- Failure event facts are retained for at least the same 400-day day-bucket
  window because they are the audit source for parse and terminal rates.
- Backfill must be idempotent: rebuild target buckets from source facts inside a
  transaction, then advance `recompute_from` only after bucket writes succeed.

### 2. Compute explainable drift

Add a quality service that compares a current classification-event window with a
baseline classification-event window.

Default windows:

- current: last 7 complete UTC days
- baseline: the preceding 28 complete UTC days
- bucket width: day
- low-confidence threshold: `0.60`

The API must allow explicit current and baseline windows so operators can inspect
incidents. Minimum volume gates reduce noise:

- Dimension-level drift requires at least 30 successful classification events in
  both current and baseline windows.
- Value-level warnings require at least 10 current appearances or a 10 percentage
  point share change.
- Windows below the threshold return `insufficient_data`, not `normal`.

Window validation:

- reject `current_to <= current_from` and `baseline_to <= baseline_from`
- cap each requested window at 90 days
- use day buckets for windows longer than 14 days
- use hour buckets only when both windows are 14 days or shorter
- include `generated_at`, `data_through`, and `rollup_lag_seconds` in responses

Rate denominators:

- low-confidence rate = low-confidence successful events / successful events
- off-list rate = off-list dropped values / successful events
- unknown-Dimension rate = unknown-Dimension dropped values / successful events
- parse failure rate = parse failure events / all classification attempts
- terminal failure rate = terminal failure events / all classification attempts

`all classification attempts` means successful semantic extraction events plus
failure events in the same event-time window. This keeps parse and terminal
signals comparable even when a row fails before it can produce semantic attrs.

Categorical drift metrics:

- Jensen-Shannon distance over the Dimension's configured and observed values.
- absolute share delta in percentage points per value.
- optional PSI as a secondary score for operators used to monitoring dashboards.

For multi-value Dimensions, JS and PSI use appearance-share distributions. The
dashboard should show this as "share of emitted values" and pair it with the
Dimension coverage metric so operators do not confuse a multi-label appearance
share with a percentage of feedback rows.

Severity rules:

| Severity | Rule |
|---|---|
| `normal` | Volume gates pass and no watch/alert threshold is crossed. |
| `watch` | JS distance >= 0.10, value share delta >= 10 percentage points, low-confidence rate delta >= 5 percentage points, parse failure rate >= 2%, off-list rate >= 2%, or terminal failure rate >= 1%. |
| `alert` | JS distance >= 0.20, value share delta >= 20 percentage points, low-confidence rate delta >= 10 percentage points, parse failure rate >= 5%, off-list rate >= 5%, or terminal failure rate >= 3%. |
| `insufficient_data` | Current or baseline volume is below the minimum. |

Each Dimension response should explain:

- current count and baseline count
- current share and baseline share per top value
- share delta in percentage points
- JS distance and top contributing values
- average confidence and low-confidence rate
- off-list and unknown-Dimension counts
- parse failure and terminal failure rates for the same event-time scope
- sample feedback IDs
- recommended links to feedback, terminal failures, Dimensions config, or LLM
  config where applicable

### 3. Add a proto-defined Console API

HTTP shape changes must be made through proto. Add a quality service or extend
the existing Console proto surface with generated Go, TypeScript, and OpenAPI
output.

Proposed endpoints:

```text
GET /fb/v1/console/classification-quality
GET /fb/v1/console/classification-quality/samples
```

Request fields:

- `current_from`, `current_to`
- `baseline_from`, `baseline_to`
- `bucket_width`
- `tenant_id` only for contexts that already permit cross-tenant console views;
  ordinary console requests use the tenant from auth context
- `source`
- `logical_model`
- `provider_model`
- `channel_id`
- `dimension_name`
- `severity`
- `low_confidence_threshold`
- `limit`

Response shape:

- data freshness: `generated_at`, `data_through`, and `rollup_lag_seconds`
- dashboard summary: successful classification events, failed attempts, average
  confidence, low-confidence rate, off-list rate, parse failure rate, terminal
  failure rate, and worst severity
- time series: confidence, low-confidence, parse failure, terminal failure, and
  off-list rates by bucket
- Dimension drift list: one row per Dimension with severity and explanation
- value drift list: top contributing values for the selected Dimension
- warnings: stable reason codes, severity, metric values, thresholds, and links
- samples: feedback ID, row created time, quality event time, title/display
  title, confidence, Dimension/value context, signal reason, and route link

Sample links use bounded filters rather than raw SQL concepts. The quality
samples endpoint is canonical for event-time samples because a feedback row's
`created_at` can differ from the classification event time. Feedback workbench
links should use supported row filters where they exist and bounded ID filters
where the signal only exists in quality facts.

Extend `ListFeedbackRequest`, feedback repo queries, Console URL state, and
route tests with:

- `ids`
- `confidence_lte`
- `created_from`
- `created_to`
- `enriched_from`
- `enriched_to`
- `quality_signal` with values such as `low_confidence`, `off_list`,
  `parse_failure`, and `terminal_failure`

Filter semantics:

- `ids` is a bounded exact sample set for dashboard drill-downs.
- `confidence_lte` filters current row confidence.
- `created_from` / `created_to` filter row creation time.
- `enriched_from` / `enriched_to` filter current row enrichment completion time.
- `quality_signal=terminal_failure` maps to the existing terminal failure
  predicate.
- `quality_signal=off_list` and `quality_signal=parse_failure` should use
  dashboard-provided sample IDs unless a dedicated row-level index is added.

Terminal failure samples should route to the terminal workbench with
`terminal_failed_only=true` and the selected time window.

Authorization and errors:

- Both quality endpoints are tenant-scoped through the existing console auth
  context. Do not trust a caller-supplied tenant ID unless the route already has
  an admin cross-tenant authorization path.
- The dashboard and sample endpoints require the same read posture as the
  feedback workbench. If an API-key-accessible surface is added, require
  `feedback:read`; add `enrich:read` only for responses that expose enrichment
  runtime configuration metadata.
- Invalid windows, unsupported bucket widths, invalid thresholds, and oversized
  `ids` filters return HTTP 400 with `ErrorCode_VALIDATION`.
- A tenant filter outside the caller's authorization returns HTTP 403 with
  `ErrorCode_FORBIDDEN`, not an empty successful response.
- Unexpected rollup gaps return a successful response with `data_through` and
  `rollup_lag_seconds`; they should not become HTTP 500 unless the aggregate
  query itself fails.

### 4. Add the Console dashboard

Add a route under the authenticated analytics or feedback area:

```text
/analytics/classification-quality
```

The page should be a dense operator dashboard, not a marketing report. It should
include:

- time-window and baseline controls
- filters for source, logical model, provider model, channel, Dimension, and
  severity when data exists
- summary cards for worst severity, successful classification events, average
  confidence, off-list rate, parse failure rate, and terminal failure rate
- confidence and failure-rate trends
- a Dimension drift table with severity, JS distance, share delta, and samples
- a value delta drill-down for the selected Dimension
- a low-quality samples table
- links to feedback rows, terminal failure workbench, Dimensions config, and LLM
  config

Empty and low-volume states should be explicit:

- no data in current window
- no baseline data
- insufficient volume for drift
- quality signals normal

### 5. Add bounded alert hooks

Expose warning information in the API response and publish low-cardinality
metrics for external alerting.

Proposed metrics:

```text
attune_classification_quality_drift_score{tenant,dimension}
attune_classification_quality_low_confidence_ratio{tenant}
attune_classification_quality_off_list_ratio{tenant}
attune_classification_quality_parse_failure_ratio{tenant}
attune_classification_quality_terminal_failure_ratio{tenant}
attune_classification_quality_warning_active{tenant,reason,severity}
```

Do not use `dimension_value`, raw off-list value, raw error, model prompt,
feedback ID, or URL as labels. Value-level detail belongs in SQL and API
responses.

These are gauges over the latest computed quality state. Do not increment a
warning counter from every dashboard query or worker tick; that would double
count the same active warning. If transition counting is added, it must be
backed by a persisted warning-state table and only increment on state changes.

Warnings should use stable reason codes:

- `dimension_distribution_drift`
- `value_share_spike`
- `low_confidence_rate_spike`
- `off_list_rate_spike`
- `parse_failure_rate_spike`
- `terminal_failure_rate_spike`
- `insufficient_baseline`

### 6. Keep remediation operator-driven

The dashboard should recommend actions without changing data automatically.

Recommended actions:

| Signal | Action |
|---|---|
| Low confidence | Open feedback workbench filtered to the window and threshold. |
| Off-list value | Open affected samples and link to Dimensions config. |
| Unknown Dimension | Open samples and link to prompt policy / Dimensions config. |
| Parse failures | Open affected samples and link to LLM prompt/config surfaces. |
| Terminal failures | Open the terminal failure workbench. |
| Distribution drift | Open value-level samples and compare against baseline window. |

Any taxonomy change, retry, prompt edit, or re-enrichment remains an explicit
operator action through the existing audited surfaces.

## Alternatives considered

### A. Query everything live from `user_feedback`

Rejected. Live JSONB aggregation is useful for small windows, but #161 asks for
stored aggregates. Drift dashboards need predictable latency, baseline windows,
sample IDs, and repeatable warning state.

### B. Prometheus-only monitoring

Rejected. Prometheus is good for alert hooks, but it is the wrong store for
per-tenant Dimension/value history and sample drill-down. It also creates
cardinality risk if raw taxonomy values become labels.

### C. Supervised accuracy dashboard

Rejected. Accuracy, precision, recall, and confusion matrices require labels.
attune should add those metrics when human-reviewed labels or eval datasets are
available. This proposal reports unsupervised quality proxies.

### D. Auto-promote off-list taxonomy values

Rejected. Off-list values can be useful taxonomy expansion candidates, but
automatic promotion would bypass governance and could create tenant-specific
classification drift.

### E. Use embedding clusters as the drift primitive

Rejected for this issue. Embeddings are valuable for semantic novelty and theme
discovery, but #161 is about configured Dimensions and classifier quality. The
first dashboard should be grounded in the attributes operators already use.

### F. Extend only the existing feedback stats endpoint

Rejected. The existing stats endpoint is monthly and generic. Quality monitoring
needs baseline windows, severity, warnings, sample actions, and Dimension/value
explanations.

## Risks / tradeoffs

| Risk | Mitigation |
|---|---|
| Low-volume tenants generate noisy drift. | Use minimum-volume gates and return `insufficient_data`. |
| Taxonomy edits make historical comparisons confusing. | Treat stored attrs and extraction evidence as classification-time facts; show current config links as remediation, not as historical truth. |
| Off-list values can become high-cardinality. | Hash full values, cap display strings, group invalid unknown Dimensions, return top values plus `other_count`, and keep raw values out of metric labels. |
| Sample IDs can point to deleted or erased rows. | Resolve samples at read time under tenant scope, omit deleted rows, and keep aggregate counts separate from sample availability. |
| Cross-tenant filters can accidentally expose data. | Default to auth-context tenant scope and allow explicit `tenant_id` only through existing cross-tenant authorization paths. |
| Dashboard warnings may be mistaken for accuracy claims. | Label the surface as quality and drift, and define confidence as a review signal rather than calibrated probability. |
| Rollups may lag behind ingestion. | Track rollup watermarks and expose last-updated time in the API response. |
| Event-time windows may not match row-created windows. | Name the dashboard windows as classification-event windows and keep row-created filters only for sample navigation. |
| Failure event writes add another persistence path to `MarkFailed`. | Insert failure events in the same database operation as the status update and cover retry, terminal, and owner-fenced paths with tests. |
| Query cost grows with tenants and windows. | Use hour/day bucket tables, fixed response limits, and indexes on tenant/window/filter columns. |
| The UI may overwhelm operators. | Start with summary severity, then reveal Dimension and value explanations through tables and drill-downs. |

## Implementation plan

1. Add this proposal and keep it linked from the implementing PR with
   `Closes #161`.
2. Add database migrations for semantic routing snapshot fields, failure event
   facts, value buckets, signal buckets, rollup state, and
   tenant/window/filter indexes.
3. Convert `MarkFailed` and owner-fenced failure paths to write failure events
   through the same SQL statement that records the status update.
4. Add a repo package for quality rollup and read queries. Keep raw SQL in repo,
   not service or handlers.
5. Add a service package for rollup orchestration, drift math, severity rules,
   warning construction, and sample-link construction.
6. Wire the rollup into the existing worker/runtime pattern with independent
   semantic-run and failure-event cursors, bounded backfill, and recompute
   support for recent buckets.
7. Add retention cleanup for hour buckets, day buckets, and failure facts.
8. Add proto messages and RPCs, run `make proto`, and commit generated Go,
   TypeScript, and OpenAPI output.
9. Add console handlers that adapt auth, query params, service responses, and
   enum-backed error codes.
10. Extend feedback-list filters for bounded sample drill-downs, then add Console
   API client types and the
   `/analytics/classification-quality` route with tests for normal, watch,
   alert, empty, and insufficient-data states.
11. Add Prometheus metrics with bounded labels and tests in the metrics coverage
   suite.
12. Add documentation notes for the dashboard and warning semantics if the
    Console help/docs surface changes.

## Verification

- `go test ./internal/service/... ./internal/repo/...`
- `go test ./internal/handlers/console/...`
- `go test -tags=integration ./test/integration/postgres/...` for aggregate
  migration and rollup coverage
- unit tests that prove retries and re-enrichment create new quality events
  without stale bucket state
- unit tests that prove `parse_failure_count` and `terminal_failure_count` come
  from failure events with stable reason-class semantics
- repo tests that prove owner-fenced `MarkFailed` paths insert no failure event
  when the status update matches no row
- drift tests that prove single-value Dimensions use event share and multi-value
  Dimensions use appearance share
- API tests for invalid windows, 90-day caps, bucket-width selection, and
  freshness fields
- API auth tests for tenant scoping, forbidden cross-tenant filters, and
  feedback-read access
- cardinality tests that prove long off-list values use hashes, capped display
  strings, and top-values plus `other_count`
- adversarial integration tests that persist malformed audit shapes, duplicate
  values, non-positive diagnostic counts, and failure events without feedback
  IDs before refreshing aggregates, so database constraints and sample-array
  invariants are tested together with in-memory rollup logic
- persisted bucket invariant scans that reject impossible count relationships,
  over-cap sample arrays, non-positive sample IDs, and overlong display values
- sample tests that prove deleted or erased feedback IDs are omitted from sample
  responses without changing aggregate counts
- retention/backfill tests that prove target buckets are rebuilt idempotently
  before rollup state advances
- handler/repo tests for `ids`, `confidence_lte`, `created_from`,
  `created_to`, `enriched_from`, `enriched_to`, and `quality_signal` feedback
  filters
- metrics tests that assert warning state is exported as a gauge, not an
  increment-on-read counter
- `make proto && git diff --exit-code internal/proto console/src/proto docs/openapi`
- `pnpm -C console tsc -b --noEmit`
- `pnpm -C console biome check`
- `pnpm -C console vitest run --coverage` for affected Console packages
- `bash scripts/lint-slog.sh --strict`
- `bash scripts/lint-artifacts.sh --strict`
- `make ci-check` before marking the implementation complete

## References

### Internal

- [Issue #161](https://github.com/Phixsura/attune/issues/161)
- [#21 semantic understanding](../06/2026-06-10-semantic-understanding-layer.md)
- [#24 LLM confidence and cost](../06/2026-06-11-llm-confidence-cost.md)
- [#83 off-list eval](../06/2026-06-20-eval-suggested-attrs.md)
- [#159 terminal failure workbench](../06/2026-06-30-terminal-failure-workbench.md)
- [`internal/domain/dimension.go`](../../../../internal/domain/dimension.go)
- [`internal/service/enrich/enricher.go`](../../../../internal/service/enrich/enricher.go)
- [`internal/repo/feedback/semantic_extraction.go`](../../../../internal/repo/feedback/semantic_extraction.go)
- [`internal/repo/feedback/feedback_console_stats.go`](../../../../internal/repo/feedback/feedback_console_stats.go)
- [`proto/attune/v1/ingest.proto`](../../../../proto/attune/v1/ingest.proto)

### External

- [Zendesk: Automatically classifying tickets with intelligent triage](https://support.zendesk.com/hc/en-us/articles/4550640560538-Automatically-classifying-tickets-with-intelligent-triage)
- [Zendesk: Intelligent triage dashboard](https://support.zendesk.com/hc/en-us/articles/7934127855002-Overview-of-the-Intelligent-triage-dashboard)
- [Intercom: Topics Explorer](https://www.intercom.com/help/en/articles/11390087-use-the-topics-explorer-to-see-what-s-driving-volume)
- [Intercom: CX Score](https://www.intercom.com/help/en/articles/10495092-understand-customer-experience-at-scale-with-the-cx-score)
- [Intercom: Trends](https://www.intercom.com/help/en/articles/11875255-how-to-use-trends-to-spot-shifts-in-your-support-data)
- [Salesforce: Einstein Case Classification](https://help.salesforce.com/s/articleView?id=service.cc_service_what_is.htm&language=en_US&type=5)
- [Salesforce: Case Classification performance](https://help.salesforce.com/s/articleView?id=service.cc_service_performance.htm&language=en_US&type=5)
- [Qualtrics: Text Analytics overview](https://www.qualtrics.com/support/omnichannel-listening/text-analytics-xm/text-analytics-overview/)
- [Qualtrics: Automated Text Analytics](https://www.qualtrics.com/support/omnichannel-listening/text-analytics-xm/ai-powered-topic-models/)
- [Qualtrics: Sentiment tuning](https://www.qualtrics.com/support/xm-discover/designer/sentiment/sentiment-tuning-designer/)
- [Medallia: Text Analytics](https://www.medallia.com/platform/text-analytics/)
- [Sprinklr: Sentiment enrichment and alerts](https://www.sprinklr.com/help/articles/ai-enrichments/detect-the-sentiment-present-in-customer-messages-accurately/645b5278e66f2e36b45187ac/)
- [Pendo: Listen overview](https://support.pendo.io/hc/en-us/articles/18159674293531-Overview-of-Pendo-Listen)
- [Pendo: Explore feedback with AI](https://support.pendo.io/hc/en-us/articles/37717114561819-Explore-feedback-with-AI-in-Listen)
- [Productboard: Pulse insight reports](https://support.productboard.com/hc/en-us/articles/34627982878483-Generate-insight-reports-with-Productboard-Pulse)
- [Arize: Drift tracing](https://arize.com/docs/ax/machine-learning/machine-learning/how-to-ml/drift-tracing)
- [Evidently: Data drift preset](https://docs-old.evidentlyai.com/presets/data-drift)
- [Evidently: Classification performance preset](https://docs-old.evidentlyai.com/presets/class-performance)
- [WhyLabs: Drift algorithms](https://docs.whylabs.ai/docs/drift-algorithms/)
- [Fiddler: Data drift](https://docs.fiddler.ai/observability/platform/data-drift-platform)
- [Fiddler: Model performance](https://docs.fiddler.ai/glossary/model-performance)
- [NannyML: Univariate drift detection](https://nannyml.readthedocs.io/en/v0.8.0/how_it_works/univariate_drift_detection.html)
