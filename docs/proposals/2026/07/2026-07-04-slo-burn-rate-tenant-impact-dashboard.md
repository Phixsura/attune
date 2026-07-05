<!-- markdownlint-disable MD013 -->

# SLO Burn-Rate Alerts and Tenant Impact Dashboard

| Field | Value |
| --- | --- |
| **Issue** | [#156](https://github.com/Phixsura/attune/issues/156) |
| **Status** | Implemented |
| **Started** | 2026-07-04T13:25:21+08:00 |
| **Related** | #63 (dashboard coverage), #93 (MCP observability), #155 (MWMB precedent for worker leases) |

## Problem

Attune already ships a strong observability contract: metrics are documented,
recording rules precompute operational slices, dashboards are generated, and
alerts carry runbook metadata. The current system still stops at threshold
alerts and symptom dashboards, though.

Issue #156 asks for a reliability operating layer:

- explicit SLOs for ingest success, enrichment latency, outbox delivery, MCP
  errors, GDPR job completion, and auth/API-key failures;
- multi-window multi-burn-rate alerts;
- a tenant impact view that tells operators which tenants are burning the most
  budget;
- runbook links on every page.

Without that layer, operators can see that something is red, but they cannot
quickly answer:

- Which SLO is actually burning error budget?
- Is this a service reliability issue, a client misuse issue, or a
  tenant-specific hot spot?
- Is the problem page-worthy now, or just a symptom that belongs on a
  dashboard?
- Which runbook should they open first?

The repo also had a telemetry gap in two of the requested areas before this
patch landed:

- there was no `attune_mcp_*` metric family, even though the earlier MCP
  proposal already sketched one;
- there was no `attune_gdpr_*` metric family, so GDPR completion needed
  dedicated counters or latency series before it could be measured cleanly.

## World-Class Bar

Top-tier SLO implementations converge on the same shape:

- Google SRE popularized the 14.4x / 6x multi-window burn-rate pattern.
- Grafana SLO and Datadog SLO both center a service-level object, then fan out
  to dashboards, alerts, and drill-down.
- Nobl9 and AWS Application Signals emphasize service health plus impact
  attribution, not just raw counters.
- OpenSLO shows that portable SLO definitions should be explicit about
  objective, window, and good/bad events.

For Attune, that means:

- one source of truth for each SLO;
- recorded SLI series instead of repeated PromQL;
- page-worthy burn-rate alerts only for service-owned failures;
- a tenant-impact dashboard that ranks the tenants and paths most responsible
  for the burn;
- a Console summary that reuses the same numbers.

## Current Code Reconciliation

| Area | Verified reality | Decision |
| --- | --- | --- |
| Metric registry | `internal/infra/metrics/metrics.go` owns the exported `attune_*` families. | Add only the minimal missing counters or histograms there when the current surface is not enough for a real SLO. |
| Recording rules | `observability/rules/attune-recording.yml` already records ingest validation ratio, ingest rate-limit rate, enrichment p95, outbox lag, notify failures, LLM provider error ratio, and inbound availability/freshness. | Extend these rules with burn-rate inputs rather than repeating expensive expressions in alerts. |
| Alert rules | `observability/rules/attune-alerts.yml` has good threshold alerts and runbook annotations, but no MWMB policy yet. | Add fast/slow burn rules with minimum-traffic guards and the same annotation contract. |
| Dashboards | `internal/tools/observabilitydash` already generates the first-party dashboards and has coverage or drift tests. | Add a reliability dashboard family there instead of hand-editing JSON. |
| Console | The existing Control Tower is feedback-quality focused, not reliability focused. | Implement a small reliability summary surface, but keep Grafana as the primary deep-dive view. |
| SLO tracker | `internal/pkg/slo/slo.go` is an in-memory tracker, not the production observability source of truth. | Do not wire alerts or dashboards to it. |
| MCP / GDPR telemetry | `attune_mcp_*` and `attune_gdpr_*` families are exported now. | Keep the new telemetry and burn-rate slices as the SLO source of truth. |
| OpenSLO portability | The shared reliability catalog now exports to `observability/openslo/attune-slo.yaml` and round-trips in tests. | Keep the portable bundle generated from the catalog so vendor-neutral SLO tooling stays in sync. |
| Historical / dependency triage | Burn-history, remaining-budget, replay-report, and dependency panels are now present on the tenant-impact surface. | Keep the historical burn view, remaining-budget view, replay comparison worksheet, and dependency-health triage panel generated from the same source data. |
| Routing metadata | Owner, escalation path, and runbook links were fragmented across rule annotations and console cards. | Generate owner / escalation annotations from the catalog and surface the same routing metadata in Grafana and Console. |
| Policy guidance | There was no generated starting-point policy surface for new SLOs. | Generate policy-summary annotations, a policy reference report, and Console policy cards with explicit budget-exception stance from the same catalog. |

## Proposal

This patch lands the executable observability slice: burn-rate recording rules
and MWMB alerts in a generated `attune-slo.yml`, the generated tenant-impact
dashboard, exact 5s enrichment bucketing, the Console reliability summary,
the portable OpenSLO bundle, the replay comparison worksheet, and the
rule / dashboard drift tests. MCP / GDPR metric additions and burn-rate slices
are included in this patch, and API key access-denial telemetry is now
recorded and alertable. The same catalog now also drives a generated policy
reference report plus policy cards in Console so the recommended starting
objective, burn windows, low-traffic guardrails, and budget-exception stance
stay visible beside the live SLO surface. The Console reliability page also
offers a tenant-prefilled replay worksheet download, a replay workspace card
with live markdown preview and copy action, and a direct OpenSLO bundle link
so operators can move from triage into backfill or portability checks without
leaving the page.

### 1. SLO Taxonomy

Split the observability contract into two classes:

- **Reliability SLOs** consume error budget and can page.
- **Diagnostic quality signals** do not consume budget, but they stay visible
  on dashboards and may page only if a separate policy says so.

This distinction matters for Attune because some failures are user- or
policy-driven:

- validation errors,
- expected authorization denials,
- normal rate limiting,
- some tenant-specific policy rejections.

Those signals are important, but they should not automatically burn reliability
budget unless the product owner explicitly chooses that semantics.

### 2. SLO Candidates

| SLO | Source today | Proposed SLI shape | Status |
| --- | --- | --- | --- |
| Ingest success | `attune_ingest_total`, `attune_ingest_rate_limit_total` | accepted requests that are not service failures; separate validation-error panel stays diagnostic | Can ship now |
| Enrichment latency | `attune_enrich_duration_seconds_bucket`, `attune_enrichment_terminal_failures_total` | share of enrichment attempts that finish successfully within the latency target | Can ship now |
| Outbox delivery | `attune_outbound_delivery_attempts_total`, `attune_notify_failures_total`, `attune_outbox_lag_seconds`, `attune_outbox_dead_rows` | attempt-level delivery success plus a separate queue-freshness guard | Can ship now, with a gauge-based freshness guard |
| MCP errors | `attune_mcp_tool_calls_total`, `attune_mcp_tool_latency_seconds` | tool-call success and latency by tool | Implemented |
| GDPR completion | `attune_gdpr_job_total`, `attune_gdpr_job_duration_seconds` | request completion before SLA deadline | Implemented |
| Auth/API-key failures | `attune_authz_denied_total`, `attune_apikey_*` counters | access-denial SLO plus separate authorization-denial diagnostics | Implemented |

### 3. Recording Rules to Add

Use recorded ratios so alerts and dashboards stay readable.

```promql
# Generic 30d burn rate for a service-owned SLO.
burn_rate = bad_rate / clamp_min(total_rate, 1e-9) / (1 - objective)
```

For a 30d SLO, the canonical Google-style thresholds are:

- fast burn: 5m and 1h windows above 14.4x;
- slow burn: 30m and 6h windows above 6x;
- optional ticket-tier burn: 6h and 3d windows above 1x.

#### Ingest

Keep validation errors as a diagnostic panel, but do not automatically count
them as reliability budget unless a product owner wants that semantics.

```promql
# Service-owned ingest failure ratio.
(
  sum(rate(attune_ingest_total{result="internal_err"}[5m]))
  + sum(rate(attune_ingest_rate_limit_total[5m]))
)
/
clamp_min(
  sum(rate(attune_ingest_total[5m]))
  + sum(rate(attune_ingest_rate_limit_total[5m])),
  1e-9
)
```

Recommended recording rule names:

- `attune:ingest_service_failure_ratio:ratio5m`
- `attune:ingest_service_failure_ratio:ratio1h`
- `attune:ingest_service_failure_ratio:ratio30m`
- `attune:ingest_service_failure_ratio:ratio6h`

Validation error ratio stays as a separate quality signal:

- `attune:ingest_validation_error_ratio:ratio5m`

#### Enrichment latency

```promql
# Share of enrichment attempts completed successfully within 5 seconds.
sum(rate(attune_enrich_duration_seconds_bucket{le="5",result="ok"}[5m]))
/
clamp_min(sum(rate(attune_enrich_duration_seconds_count[5m])), 1e-9)
```

Recommended recording rule names:

- `attune:enrich_success_under_5s:ratio5m`
- `attune:enrich_success_under_5s:ratio1h`
- `attune:enrich_success_under_5s:ratio30m`
- `attune:enrich_success_under_5s:ratio6h`

This is a better SLO input than only the p95 stat because it yields an actual
error budget.

#### Outbox delivery

Keep `attune_outbox_lag_seconds` as a queue-freshness symptom and
`attune_outbox_dead_rows` as dead-letter depth. Use delivery outcomes for the
actual burn-rate math.

```promql
# Delivery attempt failure ratio.
sum(rate(attune_outbound_delivery_attempts_total{result="terminal"}[5m]))
/
clamp_min(sum(rate(attune_outbound_delivery_attempts_total[5m])), 1e-9)
```

If implementation needs a final-outcome counter instead of attempt-level
outcomes, add:

- `attune_outbound_delivery_results_total{destination_type,result}`
- or a per-row final delivery success counter.

Recommended recording rule names:

- `attune:outbox_delivery_failure_ratio:ratio5m`
- `attune:outbox_delivery_failure_ratio:ratio1h`
- `attune:outbox_delivery_failure_ratio:ratio30m`
- `attune:outbox_delivery_failure_ratio:ratio6h`

#### MCP

Add:

- `attune_mcp_tool_calls_total{tenant,tool,result}`
- `attune_mcp_tool_latency_seconds{tenant,tool}`

Then record:

```promql
sum(rate(attune_mcp_tool_calls_total{result!="ok"}[5m]))
/
clamp_min(sum(rate(attune_mcp_tool_calls_total[5m])), 1e-9)
```

#### GDPR

Add:

- `attune_gdpr_job_total{tenant,request_type,result}`
- `attune_gdpr_job_duration_seconds{tenant,request_type}`

Then record completion or deadline miss ratios from the job status series. If
the initial implementation only exposes counters, add a separate "overdue" gauge
or a completion-time histogram so the SLO can measure deadline compliance
instead of just work started.
Cancelled and revoked jobs stay visible in the dashboard state mix, but they do
not count against the completion denominator used for the burn-rate lens.

#### Auth / API Keys

Current counters are still valuable:

- `attune_authz_denied_total`
- `attune_apikey_scope_denied_total`
- `attune_apikey_expired_total`
- `attune_apikey_ip_denied_total`
- `attune_apikey_rate_limited_total`
- `attune_apikey_usage_total`

But these mostly describe policy outcomes, not a denominator for a true
reliability budget. To make auth/API-key failures pageable as a burn-rate SLO,
add one attempt counter with a clean success/failure split.

### 4. Alert Policy

Use MWMB alerts on the recorded ratios.

Fast burn:

- page when the 5m and 1h burn rates are both above 14.4x;
- require a minimum traffic floor so tiny tenants do not generate noise.

Slow burn:

- page or ticket when the 30m and 6h burn rates are both above 6x;
- do not suppress long enough that the operator only learns after the budget is
  gone.

Ticket-tier:

- optional non-paging signal for sustained low-grade burn;
- useful for enterprise tenants where a human should watch the trend before it
  becomes a page.

Alert annotations should continue the existing contract:

- `dashboard`
- `dashboard_url`
- `runbook_url`
- `action`

### 5. Tenant Impact Dashboard

Build a dedicated reliability dashboard family in
`internal/tools/observabilitydash`:

- `Attune Reliability Overview`
- `Attune Tenant Impact`
- optional drill-down tabs for `Ingest`, `Enrichment`, `Outbox`, `MCP`,
  `GDPR`, `Auth`

The top-level tenant impact page should answer three questions immediately:

- Which tenant is burning budget fastest?
- Which SLO is the burn coming from?
- Is the issue a service failure, a policy denial, or a queue or backlog
  problem?

#### Layout

| Section | Panels | Primary data source |
| --- | --- | --- |
| Header | active burn state, current objective, remaining budget, top paging alert | recorded SLO ratios |
| Tenant ranking | top tenants by fast burn, slow burn, request volume, and recent delta | `sum by (tenant)` over recorded SLO ratios |
| Ingest | request volume, service failure ratio, validation-error ratio, rate-limit pressure | `attune_ingest_total`, `attune_ingest_rate_limit_total` |
| Enrichment | p95, success-under-target ratio, terminal failures, queue pressure | `attune_enrich_duration_seconds`, `attune_enrichment_terminal_failures_total`, `attune_claim_contention_total` |
| Outbox | delivery failures, lag, dead rows, retryable status mix | `attune_outbound_delivery_attempts_total`, `attune_notify_failures_total`, `attune_outbox_lag_seconds`, `attune_outbox_dead_rows` |
| MCP | tool calls, error mix, latency by tool | new `attune_mcp_*` metrics |
| GDPR | request state, completion latency, overdue work | new `attune_gdpr_*` metrics |
| Auth / API key | denials by class, rate-limit pressure, usage | existing `attune_authz_*` and `attune_apikey_*` counters |
| Drill-down footer | runbook links, owning area, last alert time, console link | dashboard metadata / alert annotations |

#### Data Source Rules

- Prometheus remains the quantitative source of truth.
- Dashboard metadata such as owner, escalation path, and runbook link can live
  in the dashboard generator or a small SLO registry file.
- Console should read the same recorded ratios, not duplicate alert math in
  TypeScript.
- Panels that lack tenant labels should stay cluster-wide instead of faking
  tenant filters.

### 6. Console Summary

Add a small reliability card or page in Console:

- current active burn state;
- top impacted tenant;
- top burning SLO;
- links to the matching Grafana dashboard and runbook.

Keep Console as the decision-entry surface; keep Grafana as the detailed
operator tool.

## Alternatives Considered

| Alternative | Why it was not chosen |
| --- | --- |
| Keep only threshold alerts | Good for symptoms, but not enough to reason about error budgets or tenant impact. |
| Put every metric on one dashboard | Too noisy; world-class observability favors focused overview plus drill-down. |
| Use `internal/pkg/slo/slo.go` as the source of truth | It is an in-memory tracker, not the production telemetry contract. |
| Adopt a full Jsonnet or Grafonnet stack now | Powerful, but more machinery than Attune needs for a small, curated dashboard suite. |
| Count validation errors and policy denials as reliability burns by default | That mixes user behavior with service reliability and makes the error budget noisy. |

## Risks / Tradeoffs

- Low-traffic tenants can produce noisy ratios; every burn-rate alert needs a
  minimum-volume guard.
- Some existing metrics are cluster-wide, so tenant attribution will be partial
  in a few panels.
- MCP and GDPR need new base metrics before they can have first-class SLOs.
- If we use policy denials as burn-budget inputs, the error budget will reflect
  customer misuse as much as service health.
- Any new metric family must stay bounded and label-safe; no raw tenant IDs,
  subjects, URLs, or error text in labels.

## Implementation Plan

1. Add or confirm the minimal missing metrics for MCP and GDPR, plus any
   denominator counters needed for auth/API-key reliability.
2. Add burn-rate recording rules and MWMB alerts in a generated
   `observability/rules/attune-slo.yml`, and copy the same file into the Helm
   chart.
3. Keep the dedicated SLO file catalog-driven from `internal/tools/observabilitydash`
   so the observability and Helm copies stay aligned.
4. Add the reliability dashboard family to `internal/tools/observabilitydash`.
5. Add the Console reliability summary backed by the same recorded ratios. The
   Console slice, MCP / GDPR telemetry, policy-exception stance, and replay
   comparison worksheet are implemented in this patch.
6. Extend dashboard and rule drift tests so the new SLO surface stays synchronized.

## Verification

- `go test ./...` for generator and coverage drift.
- `go vet ./...` for any Go changes.
- `make ci-check` once implementation starts, because the repo treats
  observability drift as a first-class regression.
- `promtool test rules` or the repository's existing rule tests for the new
  burn-rate expressions.
- Browser validation of the new dashboard pages after they are generated.

## References

- Issue: [#156](https://github.com/Phixsura/attune/issues/156)
- Google SRE workbook and Google Cloud burn-rate alert guidance
- Grafana SLO documentation
- Datadog SLO burn-rate guidance
- Nobl9 service health dashboards
- AWS CloudWatch Application Signals
- OpenSLO specification
- Internal precedent: `docs/proposals/2026/06/2026-06-25-ha-worker-leases.md`
- Dashboard foundation: `observability/README.md`, `observability/rules/attune-recording.yml`, `observability/rules/attune-alerts.yml`
