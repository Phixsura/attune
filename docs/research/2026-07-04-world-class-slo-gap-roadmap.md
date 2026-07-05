# World-Class SLO Platform Gap Analysis and Roadmap

| | |
|---|---|
| **Started** | 2026-07-04 |
| **Status** | Reference (roadmap input for follow-up SLO platform work) |
| **Scope** | Attune's SLO model, burn-rate alerts, tenant-impact triage, reporting, and interoperability |
| **Related** | [#156](https://github.com/Phixsura/attune/issues/156) baseline, [SLO Burn-Rate Alerts and Tenant Impact Dashboard](../proposals/2026/07/2026-07-04-slo-burn-rate-tenant-impact-dashboard.md) |

> Evidence strength: this note uses vendor docs and product docs as capability evidence.
> It compares features and operating models, not market share or benchmark numbers.

## 1. Current Attune baseline

Attune already has the reliability core that most teams struggle to build:

- recorded SLI series and burn-rate inputs in Prometheus;
- multi-window multi-burn-rate alerts for service-owned reliability signals;
- a generated `Attune Tenant Impact` dashboard with burn overview, tenant ranking, and drilldowns;
- a generated `Attune Tenant Impact` dashboard with burn history and remaining-budget views, so the operator can tell spike from sustained regression and see budget runway;
- a generated `Attune Tenant Impact` dashboard with a replay comparison worksheet tied to the current policy and routing metadata, plus a Console download and replay workspace that pre-fill the same worksheet with the current tenant context;
- a generated policy reference report and Console policy cards that expose the recommended objective, burn windows, low-traffic guardrails, and budget-exception stance;
- a portable OpenSLO export/import bundle for the reliability catalog, with round-trip tests that keep the vendor-neutral view in sync;
- a generated `Attune Tenant Impact` dashboard with routing metadata that ties the catalog to owner, escalation, and runbook links;
- a Console reliability entry point that links operators into the same surface;
- generator and drift tests so dashboards and rules stay synchronized.

That baseline is strong. The remaining gap is the maintenance layer above it:

- keeping the generated replay comparison worksheet aligned with policy and routing changes.

## 2. What top-tier projects converge on

### Google SRE

The Google SRE guidance makes burn-rate alerting actionable when it is tied to
significant error-budget spend, with multi-window logic to balance precision and
reset time. The key lesson is that the alert should represent an event worth
attention, not every transient spike.

### Grafana SLO

Grafana emphasizes simple, attainable SLOs, event-based SLIs, label discipline,
and minimum-failure protection for low-traffic services. Its SLO workflow also
keeps alert routing and labels close to the SLO object.

### Datadog SLO

Datadog makes the SLO itself a first-class object and attaches burn-rate alerts
to it with long and short windows. It also supports grouped alerts, API-based
management, and guardrails around impossible thresholds.

### AWS Application Signals

AWS centers SLOs around services, operations, dependencies, and composite SLOs.
Its dashboard and triage flow are built to answer "what service is unhealthy"
and "which dependency or operation is responsible" without leaving the SLO
context.

### Nobl9

Nobl9 adds the operating layer that many stacks miss: SLI analysis, replay,
budget adjustments, reports, and a service-health dashboard that rolls multiple
SLOs into one view.

### OpenSLO

OpenSLO contributes the portability layer: a vendor-neutral schema with
`Service`, `SLO`, `SLI`, `AlertPolicy`, `AlertCondition`, and
`AlertNotificationTarget`.

## 3. Gap matrix

| Capability | World-class pattern | Attune today | Gap priority |
|---|---|---|---|
| Canonical SLO object model | SLOs are declared once and reused across alerting, dashboards, and reports | Recorded ratios and generated dashboards exist; the burn surface now uses a typed SLO catalog, but alert/rules metadata and broader platform modeling still repeat some PromQL | P0 |
| Burn-rate alerting | Fast-burn and slow-burn windows with low-noise guards | Implemented for the main reliability signals | P0 complete |
| Tenant impact triage | The operator sees the highest-burn tenant and the responsible SLO immediately | Implemented in the tenant-impact dashboard and Console entry point | P0 complete |
| Service/dependency triage | Operators can pivot from an unhealthy SLO to its owning service, operation, or dependency | Dependency health and routing metadata now stay visible on the tenant-impact surface | P1 complete |
| Historical reporting | Roll-ups, budget trends, and replay/backfill are part of the product | Burn history, remaining-budget views, and a generated replay comparison worksheet now exist | P1 complete |
| Portable definitions | Export/import to a vendor-neutral SLO spec | Implemented via a generated OpenSLO bundle and round-trip drift tests | P2 complete |
| Recommendations | The platform suggests thresholds, windows, low-traffic safeguards, and explicit exception stance | Implemented via a generated policy reference report, catalog annotations, and Console policy cards | P2 complete |
| Low-traffic policy | Minimum failures and synthetic guidance reduce noisy alerts | Implemented via traffic-guard annotations and MWMB alert expressions | P2 complete |

## 4. Prioritized Backlog

### P0: Canonicalize the SLO catalog

Goal: make the SLO definition a product object, not a repeated PromQL pattern.

Exit criteria:

- every service-owned SLO appears in a single catalog;
- burn-rate math remains generated from that catalog;
- dashboard and rule drift tests continue to pass from one source of truth.

### P1: Historical reporting

Status: implemented.

Delivered:

- burn history and remaining-budget views on the tenant-impact surface;
- a generated replay comparison worksheet that compares a historical outage against the current SLO policy;
- catalog-driven owner, escalation, runbook, and replay-lens metadata in the report.

## 5. Residual watchpoints

- Keep the replay comparison worksheet aligned with the catalog when policy or routing changes land.

This keeps the current burn-rate investment intact while leaving future
platform work isolated from the core reliability surface.

## 6. Risks and tradeoffs

- A registry that is too generic becomes another configuration language with no
  operational value.
- A service/dependency graph that is too broad can become harder to read than
  the current focused tenant-impact view.
- OpenSLO compatibility should not force the rest of the product into a lowest-
  common-denominator schema.
- Recommendation logic is only useful if it is transparent enough for operators
  to trust and override.

## 7. References

- Google SRE Workbook, Alerting on SLOs: https://sre.google/workbook/alerting-on-slos/
- Grafana SLO best practices: https://grafana.com/docs/grafana-cloud/alerting-and-irm/slo/best-practices/
- Datadog SLO burn-rate alerts: https://docs.datadoghq.com/service_management/service_level_objectives/burn_rate/
- AWS CloudWatch SLOs: https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch-ServiceLevelObjectives.html
- Nobl9 documentation: https://docs.nobl9.com/
- OpenSLO specification: https://github.com/OpenSLO/OpenSLO

- [Attune observability README](../../observability/README.md)
- [Tenant impact proposal](../proposals/2026/07/2026-07-04-slo-burn-rate-tenant-impact-dashboard.md)
- [Reliability generator](../../internal/tools/observabilitydash/reliability.go)
- [Console reliability surface](../../console/src/features/reliability/components/reliability-page.tsx)
