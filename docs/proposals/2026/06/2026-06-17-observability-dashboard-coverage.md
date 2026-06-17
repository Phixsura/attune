<!-- markdownlint-disable MD013 -->

# Observability dashboard coverage

| Field | Value |
| --- | --- |
| **Issue** | [#63](https://github.com/Phixsura/attune/issues/63) |
| **Status** | Implemented |
| **Started** | 2026-06-17 10:31 CST |
| **Related** | #6 (Prometheus + Grafana overlay), #42 (Helm chart), #24 (LLM observability), #25 (embedding clustering), #26 (reply drafts), #27 (daily digest), #29 (workflow status), #30 (batch/search), #38 (RBAC), #39 (audit log), #40 (OIDC SSO) |

## Problem

Attune's Prometheus metric surface has outgrown its first dashboard. Issue #63
was filed when the service had seven `attune_*` metric families and the
`Attune Overview` dashboard charted five of them. The issue asked for panels for:

- `attune_ingest_rate_limit_total{tenant}`.
- `attune_triage_decisions_total{tenant,decision}`.

That request is still directionally correct, but the context is now stale. The
registered metric catalog has grown to roughly fifty metric families across
ingest, enrichment, guardrails, LLM cost, embedding, reply drafts, daily digest,
workflow, batch operations, search, OIDC, authorization, audit logging, and the
channel-agnostic inbound framework. Only two first-party Grafana dashboards
exist today:

- `observability/dashboards/attune-overview.json`, which covers the original
  core signals.
- `observability/dashboards/llm-cost.json`, which covers LLM cost, calls, tokens,
  and provider errors.

The current situation creates three operator-facing problems:

1. Many production-relevant metrics are emitted and documented but invisible in
   the shipped Grafana assets.
2. There is no drift guard that forces new metrics to receive dashboard
   treatment, so this gap will recur.
3. The Helm chart now packages dashboard JSON under
   `deploy/helm/attune/dashboards/`, so every observability asset has two
   distribution surfaces that can drift.

Simply adding two panels would close the literal issue text but leave the larger
observability product unfinished. Conversely, putting every metric on the
overview page would produce a noisy wall that is hard to operate. The right fix
is dashboard coverage parity with a mixin-grade maintenance model.

## Goals

- Treat first-party observability assets as a product, not as ad hoc JSON files.
- Ensure every registered `attune_*` metric family is represented in at least one
  shipped Grafana dashboard, unless explicitly waived in a documented exception.
- Keep `Attune Overview` useful as an executive and operator landing page:
  service health, traffic, latency, backlog, rate limits, triage, and top-level
  failures.
- Add domain dashboards for drill-down so high-cardinality or specialist signals
  do not crowd the overview.
- Keep dashboards datasource-less, matching the existing
  `observability/README.md` contract.
- Keep compose and Kubernetes distribution paths aligned:
  `observability/dashboards/*.json` and
  `deploy/helm/attune/dashboards/*.json` must contain the same dashboards.
- Add automated drift guards:
  - registered metric catalog to dashboard coverage;
  - generated dashboard source to committed JSON;
  - `observability/` dashboards to Helm dashboard copies.
- Prefer repo-native, deterministic generation over hand-maintained dashboard
  JSON.
- Preserve the current no-hardcoded-datasource behavior so operators can use
  Prometheus, VictoriaMetrics, Grafana Agent, OpenTelemetry Collector, Datadog
  OpenMetrics, or another compatible backend.
- Document the dashboard model so future contributors know whether to update an
  overview panel, a domain dashboard, or the generator catalog.

## Non-goals

- Do not add Alertmanager, paging integrations, or notification routing in this
  issue. The repo ships Prometheus rules, while production routing remains owned
  by the operator's monitoring stack.
- Do not introduce Jsonnet, Grafonnet, Tanka, or a new Node toolchain in this
  issue. Those are reasonable for larger multi-service estates, but Attune can
  get the important properties with a small Go generator and tests.
- Do not make the overview dashboard a complete metric catalog.
- Do not introduce app runtime changes or new metric names.
- Do not create a metrics plugin system. Prometheus/OpenMetrics exposition is
  already the vendor-neutral interface.
- Do not guarantee that every panel is useful for every tenant or deployment
  mode. Some panels remain empty until the matching feature is enabled.

## Current code reconciliation

| Area | Verified reality | Decision |
| --- | --- | --- |
| Metric catalog | Most collectors live in `internal/infra/metrics/metrics.go`; inbound framework collectors live in `internal/inbound/metrics.go`. | Use both registered sets as the source of truth for dashboard coverage tests. |
| Metrics docs | `observability/README.md` should list all process-level `attune_*` metrics and label contracts. | Expand docs to include inbound metrics and keep the docs drift test aligned with the full catalog. |
| Overview dashboard | Charts ingest, enrich latency, notify failures, outbox lag, and claim contention. | Keep it as the landing page and add only high-signal summaries. |
| LLM dashboard | Already charts cost, calls, tokens, and provider errors. | Keep and fold into the generated dashboard set. |
| Helm dashboards | Helm packages dashboard JSON from `deploy/helm/attune/dashboards/`. | Generate or sync these copies from the same source as `observability/dashboards/`. |
| Datasource contract | Overview panels have no hardcoded datasource. `llm-cost` variables use `datasource: null`. | Preserve datasource-less dashboards. Tests should reject panel/target datasource UIDs. |
| CI shape | `go test ./...` is already a hard gate. | Put drift guards in Go tests so the normal backend CI path owns them. |

## Industry benchmarking

Mature observability assets converge on a few repeatable patterns:

| Project / practice | Relevant pattern | Lesson for Attune |
| --- | --- | --- |
| Kubernetes mixins | Dashboards, alerts, and rules are treated as a generated package, not one-off UI exports. | Use a source-of-truth dashboard spec and deterministic generation. |
| kube-prometheus | Ships compiled dashboards and rules as deployable assets, backed by testable source. | Commit generated JSON for operators, but verify it has no drift. |
| Istio dashboards | Splits mesh overview, service, workload, control plane, and performance views. | Use overview plus domain drill-down dashboards, not one giant page. |
| Grafana Mimir and Loki mixins | Publish mixin source plus generated dashboards and alerts, with explicit label assumptions. | Keep Attune label assumptions in `observability/README.md` and build queries on that contract. |
| Grafana dashboard guidance | Avoid dashboard sprawl, use variables, links, clear organization, and focused pages. | Use a small set of domain dashboards with shared tenant/range variables and dashboard links. |
| Prometheus recording rules guidance | Precompute frequent or expensive expressions as rules. | Ship SLI recording rules once dashboards and browser validation prove the operational shape. |

This proposal intentionally borrows the "mixin-grade" shape without adopting a
full Jsonnet stack yet. Attune is a single Go service with a small dashboard set;
a Go generator plus drift tests gives most of the operational rigor with much
less new tooling.

References:

- Kubernetes mixin: <https://github.com/kubernetes-monitoring/kubernetes-mixin>
- kube-prometheus: <https://github.com/prometheus-operator/kube-prometheus>
- Monitoring mixins: <https://monitoring.mixins.dev/>
- Istio Grafana dashboards: <https://istio.io/latest/docs/ops/integrations/grafana/>
- Grafana dashboard best practices: <https://grafana.com/docs/grafana/latest/visualizations/dashboards/build-dashboards/best-practices/>
- Prometheus recording rules: <https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/>
- Prometheus naming practices: <https://prometheus.io/docs/practices/naming/>

## Proposal

### 0. Operational UX bar

Dashboard coverage is not sufficient by itself. The shipped Grafana pages must
help an operator make decisions under normal and high traffic:

- The overview follows RED/golden-signal order: traffic, validation/error rate,
  rate limiting, AI pressure, latency, and queue pressure.
- Every overview stat has a diagnostic description and thresholds where a
  threshold is defensible from the current product semantics.
- Empty/cold-start states should render as zero for aggregate health stats; raw
  sparse series may remain empty until the matching feature emits data.
- Template variables use an explicit `allValue: .*` so dashboard URLs and
  browser tests do not accidentally turn "All" into a literal value.
- High-cardinality panels use `topk` or bounded labels; dashboards should guide
  the user to the tenant/source/model causing the change instead of hiding the
  problem inside a single aggregate.
- Load validation is part of the workflow: generate traffic, wait for scrape
  windows, then verify `/metrics`, Prometheus, Grafana datasource, and browser
  rendering.

### 1. Dashboard set

Ship a small, opinionated dashboard suite:

| Dashboard | Purpose | Metric families |
| --- | --- | --- |
| `Attune Overview` | Landing page for health, traffic, latency, queue/backlog, rate limits, triage, and top-level failure signals. | `attune_ingest_total`, `attune_ingest_rate_limit_total`, `attune_triage_decisions_total`, `attune_enrich_duration_seconds`, `attune_notify_failures_total`, `attune_outbox_lag_seconds`, `attune_claim_contention_total`, selected rollups from guard/workflow/batch/search/audit where useful. |
| `Attune Inbound` | Channel-agnostic inbound source health. | inbound volume, latency, source state, and poll lag. |
| `Attune AI Pipeline` | Enrichment and AI-derived product workflow health. | enrich attrs, enrich payload size/rejections, triage, guard actions/blocks, embedding, reply drafts, daily digest. |
| `Attune Operations` | Console and background work operations. | workflow transitions, workflow batch size, batch jobs, batch operations, idempotency, semantic search, embedding cache, notify failures, outbox lag. |
| `Attune Security & Compliance` | Enterprise access, authorization, audit, and policy signals. | OIDC login/token exchange/role mapping, authorization denied, audit rows written/pruned, audit prune duration, guard blocked/actions. |
| `Attune LLM Cost` | LLM spend and usage. | LLM calls, tokens, cost, provider errors. |

The dashboards should include shared `tenant` variables where tenant labels exist.
Dashboards without tenant-scoped metrics should not fake a tenant filter. `model`,
`operation`, `status`, and similar variables may be added only when they reduce
visual noise without hiding important defaults.

### 2. Overview policy

The overview is not a catalog. It should answer:

- Is traffic flowing?
- Are users hitting ingest rate limits?
- Is the AI pipeline handling feedback or discarding noise?
- Is enrichment slow or failing?
- Is the outbox backing up?
- Are notifications failing?
- Are background workers making progress?
- Are security or audit failures spiking?

Metric families that require domain context belong on the domain dashboard even
if they are also summarized on overview.

### 3. Dashboard-as-code source

Add a repo-native generator:

```text
internal/tools/observabilitydash/
```

The generator owns:

- dashboard metadata;
- variables;
- rows or logical sections;
- panel placement;
- PromQL expressions;
- panel units, legends, thresholds, and descriptions;
- dashboard links between overview and detail pages;
- writing generated JSON into:
  - `observability/dashboards/`;
  - `deploy/helm/attune/dashboards/`.

Generated dashboard JSON should carry a small generated marker, for example:

```json
"description": "Generated by go run ./internal/tools/observabilitydash. Do not edit JSON by hand."
```

Use deterministic JSON formatting so Git diffs stay reviewable.

The generator should not reach into unexported variables from another package.
Metric catalogs needed by drift guards should be exposed through small helpers
owned by the packages that register the collectors.

### 4. Drift guards

Add tests that run in `go test ./...`:

1. **Metric coverage guard**
   - Extract registered metric names from exported catalog helpers covering
     `internal/infra/metrics` and `internal/inbound`.
   - Extract `attune_*` metric names from generated dashboard expressions.
   - Treat histogram coverage as satisfied when the base metric appears through
     `_bucket`, `_sum`, or `_count`.
   - Fail if a registered metric has no dashboard reference and no explicit
     waiver.

2. **Datasource guard**
   - Fail if a panel or target hardcodes a datasource UID.
   - Permit `datasource: null` on template variables where Grafana expects the
     field.

3. **Generation drift guard**
   - Run the generator in-memory or into a temp directory and compare with the
     committed `observability/dashboards/*.json`.
   - Fail if generated output differs.

4. **Helm sync guard**
   - Compare each `observability/dashboards/*.json` file with
     `deploy/helm/attune/dashboards/*.json`.
   - Fail on missing, extra, or different files.

The metric coverage guard should support a small waiver map, but waivers must
include a reason and should be rare. The expected initial state is zero waivers.

### 5. Query conventions

Use these PromQL conventions:

- Counters:
  - rates for live charts: `sum by (...) (rate(metric[$__rate_interval]))`;
  - range totals for stat panels: `sum(increase(metric[$__range]))`.
- Histograms:
  - latency quantiles: `histogram_quantile(0.95, sum by (le, ...) (rate(metric_bucket[$__rate_interval])))`;
  - count panels when useful: `sum(increase(metric_count[$__range]))`.
- Gauges:
  - display current values directly;
  - timeseries panels use the raw gauge.
- Ratios:
  - use `clamp_min(denominator, 1)` to avoid divide-by-zero spikes;
  - format as percent.
- Top-K panels:
  - use `topk(10, ...)` where tenant or action cardinality can be large.
- Labels:
  - aggregate away high-cardinality labels unless the label contract explicitly
    bounds them and the panel needs them.

### 6. Recording and alert rules

Ship Prometheus rule files alongside dashboards:

```text
observability/rules/attune-recording.yml
observability/rules/attune-alerts.yml
observability/runbooks.md
deploy/helm/attune/rules/*.yml
```

Recording rules precompute the SLI expressions that dashboards and alerts share:

- ingest request rate, validation error ratio, and rate-limit pressure;
- inbound availability, p95 latency, enabled/stale source counts, and freshness;
- enrichment p95;
- outbox lag and notification failure rates;
- LLM provider error ratio;
- combined AI queue depth.

Alert rules intentionally target operator symptoms rather than every raw metric:

- high ingest validation errors;
- sustained rate limiting;
- low inbound availability, high inbound latency, and stale inbound sources;
- high enrichment latency, AI queue backlog, and LLM provider errors;
- outbox lag and notification failures;
- elevated authorization denials and suspicious missing audit writes.

Every alert carries actionable annotations:

- `dashboard` and `dashboard_url` identify the first Grafana view to open;
- `runbook_url` points to the matching `observability/runbooks.md` section;
- `action` names the first operator step after opening the dashboard.

The reference Docker Compose stack loads these rules directly through
`deploy/prometheus.yml`. Helm exposes them as an optional Prometheus Operator
`PrometheusRule` resource controlled by `prometheusRule.enabled`.

### 7. Dashboard grouping details

#### Attune Overview

Candidate sections:

- Traffic: ingest requests per minute, ingest success/error split.
- Rate limits: rejected ingest requests by tenant.
- Triage: AI handling rate, noise rate, triage decisions by tenant/decision.
- Enrichment: p50/p95/p99 duration, error rate.
- Backlog: outbox lag, claim contention.
- Delivery: notify failures.
- Risk summary: guard blocks, authorization denials, audit write activity.

#### Attune AI Pipeline

Metric coverage:

- `attune_enrich_duration_seconds`.
- `attune_enrich_attrs_dropped_total`.
- `attune_enrich_suggested_attrs_total`.
- `attune_enrich_attrs_size_bytes`.
- `attune_enrich_attrs_rejected_total`.
- `attune_triage_decisions_total`.
- `attune_guard_actions_total`.
- `attune_guard_blocked_total`.
- `attune_embed_cluster_assignments_total`.
- `attune_embed_errors_total`.
- `attune_embed_duration_seconds`.
- `attune_embed_queue_depth`.
- `attune_reply_draft_generated_total`.
- `attune_reply_draft_errors_total`.
- `attune_reply_draft_duration_seconds`.
- `attune_reply_draft_queue_depth`.
- `attune_digest_runs_total`.
- `attune_digest_duration_seconds`.
- `attune_digest_clustering_fallback_total`.
- `attune_digest_cluster_count`.

#### Attune Inbound

Metric coverage:

- `attune_inbound_total`.
- `attune_inbound_latency_seconds`.
- `attune_inbound_source_state`.
- `attune_inbound_poll_lag_seconds`.

#### Attune Operations

Metric coverage:

- `attune_notify_failures_total`.
- `attune_outbox_lag_seconds`.
- `attune_claim_contention_total`.
- `attune_workflow_transitions_total`.
- `attune_workflow_batch_size`.
- `attune_batch_jobs_claimed_total`.
- `attune_batch_jobs_completed_total`.
- `attune_batch_job_duration_seconds`.
- `attune_batch_jobs_recovered_total`.
- `attune_batch_operations_total`.
- `attune_batch_operation_items_total`.
- `attune_batch_operation_duration_seconds`.
- `attune_idempotency_key_usage_total`.
- `attune_search_queries_total`.
- `attune_search_query_duration_seconds`.
- `attune_search_results_count`.
- `attune_embedding_cache_hits_total`.

#### Attune Security & Compliance

Metric coverage:

- `attune_oidc_login_total`.
- `attune_oidc_login_duration_seconds`.
- `attune_oidc_token_exchange_duration_seconds`.
- `attune_oidc_role_mapping_total`.
- `attune_authz_denied_total`.
- `attune_audit_rows_written_total`.
- `attune_audit_rows_pruned_total`.
- `attune_audit_prune_duration_seconds`.
- `attune_guard_actions_total`.
- `attune_guard_blocked_total`.

#### Attune LLM Cost

Metric coverage:

- `attune_llm_calls_total`.
- `attune_llm_tokens_total`.
- `attune_llm_cost_usd_total`.

The existing dashboard already covers these. The generator should reproduce and
then improve it, rather than replacing it with lower-fidelity panels.

## Alternatives considered

### Add only the two #63 panels

This is the smallest patch, but it solves the historical symptom instead of the
current problem. The metric surface has expanded far beyond the issue text, and
the lack of a drift guard means more dashboard debt would accumulate.

### Put every metric on `Attune Overview`

This maximizes literal coverage but produces an unusable dashboard. Mature
projects use overview/detail layering because operators need a first page that
answers "is the system healthy?" quickly.

### Keep hand-written JSON and add tests only

This avoids a generator, but hand-written Grafana JSON is difficult to review and
easy to drift across compose and Helm copies. A generator makes panel patterns,
placement, and links intentional.

### Adopt Jsonnet/Grafonnet immediately

This follows Kubernetes, Istio, Mimir, and Loki practice most closely. It is a
good future option if Attune grows into many services or starts shipping alert
rules and recording rules. For the current single-service repo, it adds a new
toolchain and dependency review surface before the complexity is justified.

### Add recording rules now

Recording rules are valuable for expensive or reused expressions, especially
ratios and histogram quantiles. They also create another deployable artifact that
must be supported in Compose and Helm. This proposal keeps queries inline first
and leaves rules for a follow-up when dashboard query cost or alerting requires
them.

## Risks and tradeoffs

- **Dashboard volume can still overwhelm users.** Keep the set small and
  domain-oriented. Use links and variables rather than many near-duplicate pages.
- **Generator abstraction can become its own framework.** Keep it plain Go,
  focused on the current dashboard model, and avoid a generic dashboard DSL
  beyond what Attune needs.
- **Coverage tests can reward bad panels.** The test only proves a metric is
  represented. Proposal review and panel descriptions still need to ensure the
  representation is meaningful.
- **Some panels will be empty in small deployments.** Use descriptions and
  sensible titles so empty panels explain feature-specific visibility.
- **Histogram and ratio queries can be expensive at scale.** Keep windows
  standard, aggregate labels deliberately, and revisit recording rules when scale
  demands it.
- **Helm copy generation touches release assets.** The sync guard mitigates this
  by making drift impossible to miss.

## Implementation plan

1. Create `internal/tools/observabilitydash`.
2. Model dashboards, variables, links, panels, targets, field config, thresholds,
   and grid placement in Go structs that marshal to Grafana JSON.
3. Teach the generator to write deterministic JSON to:
   - `observability/dashboards/`;
   - `deploy/helm/attune/dashboards/`.
4. Recreate the existing `Attune Overview` and `Attune LLM Cost` dashboards from
   generator source.
5. Add the new `Attune AI Pipeline`, `Attune Inbound`, `Attune Operations`, and
   `Attune Security & Compliance` dashboards.
6. Add overview summary panels for #63's original rate-limit and triage use
   cases.
7. Add dashboard links from overview to detail dashboards.
8. Add metric coverage, datasource, generation drift, and Helm sync tests.
9. Update `observability/README.md` with dashboard suite and generator workflow.
10. Add a `Makefile` target such as `observability-dashboards` for local
    regeneration.
11. Update `CHANGELOG.md` under `[Unreleased]`.
12. Run focused verification.

## Verification

Required local checks:

```bash
go test ./internal/infra/metrics
go test ./internal/tools/observabilitydash
go test ./...
```

Regeneration workflow:

```bash
go run ./internal/tools/observabilitydash
git diff --exit-code observability/dashboards deploy/helm/attune/dashboards
```

JSON validation:

```bash
jq empty observability/dashboards/*.json
jq empty deploy/helm/attune/dashboards/*.json
```

Dashboard contract checks:

- Every registered metric in the full process catalog appears in at least one
  dashboard expression or has a documented waiver.
- No panel or target hardcodes a datasource.
- Helm dashboard copies match `observability/dashboards`.
- Helm rule copies match `observability/rules`.
- All alert rules include `summary`, `description`, `dashboard`,
  `dashboard_url`, `runbook_url`, and `action`, and every alert has a runbook
  section.
- Existing LLM cost dashboard behavior is preserved.
- `Attune Overview` still opens with a small, readable top row of summary panels.

Optional visual smoke:

```bash
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.obs.yml up -d
```

Then open Grafana, confirm the dashboard list includes:

- Attune Overview.
- Attune Inbound.
- Attune AI Pipeline.
- Attune Operations.
- Attune Security & Compliance.
- Attune LLM Cost.

## References

- Issue #63: <https://github.com/Phixsura/attune/issues/63>
- Issue #6: <https://github.com/Phixsura/attune/issues/6>
- Kubernetes mixin: <https://github.com/kubernetes-monitoring/kubernetes-mixin>
- kube-prometheus: <https://github.com/prometheus-operator/kube-prometheus>
- Monitoring mixins: <https://monitoring.mixins.dev/>
- Istio Grafana integration: <https://istio.io/latest/docs/ops/integrations/grafana/>
- Grafana dashboard best practices: <https://grafana.com/docs/grafana/latest/visualizations/dashboards/build-dashboards/best-practices/>
- Prometheus recording rules: <https://prometheus.io/docs/prometheus/latest/configuration/recording_rules/>
- Prometheus metric naming: <https://prometheus.io/docs/practices/naming/>
