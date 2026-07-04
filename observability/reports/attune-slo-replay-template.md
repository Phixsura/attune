# Attune SLO Replay / Backfill Report Template

Use this worksheet to compare a historical outage against the current SLO policy. Fill the incident header first, then use the generated comparison matrix to capture burn history, remaining budget, routing metadata, and the most likely replay lens.

## Incident header

| Field | Value |
| --- | --- |
| Incident title | `{{ incident_title }}` |
| Incident window | `{{ incident_window }}` |
| Primary tenant | `{{ primary_tenant }}` |
| Primary SLO | `{{ primary_slo }}` |
| Likely dependency | `{{ likely_dependency }}` |
| Owner | `{{ owner }}` |
| Escalation | `{{ escalation }}` |
| Runbook | [Open runbook]({{ runbook_url }}) |
| Dashboard | [Open tenant impact dashboard]({{ dashboard_url }}) |

## Comparison matrix

Use the generated policy columns as the control. Fill the historical observation and verdict fields from the incident window.

| SLO | Current policy | Replay lens | Budget exception | Historical observation | Verdict | Runbook |
| --- | --- | --- | --- | --- | --- | --- |
| Ingest burn x | Start at 99.9% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | tenant / source / result | Maintenance windows only | `{{ ingest_service_observation }}` | `{{ ingest_service_verdict }}` | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneingestservicefastburn) |
| Enrich burn x | Start at 95.0% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | tenant / dims_mode / result | No standing exclusions | `{{ enrichment_latency_observation }}` | `{{ enrichment_latency_verdict }}` | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneenrichmentfastburn) |
| Outbox burn x | Start at 99.9% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | destination_type / reason | Destination-maintenance only | `{{ outbox_delivery_observation }}` | `{{ outbox_delivery_verdict }}` | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneoutboxdeliveryfastburn) |
| OIDC burn x | Start at 99.9% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | result / auth flow | IdP-maintenance only | `{{ oidc_login_observation }}` | `{{ oidc_login_verdict }}` | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneoidcloginfastburn) |
| API key burn x | Start at 95.0% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | tenant / denial class | No standing exclusions | `{{ apikey_access_observation }}` | `{{ apikey_access_verdict }}` | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneapikeyaccessfastburn) |
| MCP burn x | Start at 99.9% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | tenant / tool / result | Tool-migration only | `{{ mcp_tool_observation }}` | `{{ mcp_tool_verdict }}` | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attunemcptoolfastburn) |
| GDPR burn x | Start at 99.9% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | tenant / request_type / result | Denominator already excludes cancellations | `{{ gdpr_job_observation }}` | `{{ gdpr_job_verdict }}` | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attunegdprjobfastburn) |

## Replay checklist

- Open the incident window in Grafana and keep the same time range on Burn trend, Burn history, and Remaining budget.
- Record the dominant tenant, source/result, dependency, or destination type that explains the spike, then fill the comparison matrix verdict column.
- Compare the current 5m / 1h burn with the 7d / 30d averages to separate a spike from a sustained regression.
- Copy owner, escalation, and runbook metadata from the routing table into the incident review.
- Decide whether the historical event would still page under the current fast-burn and slow-burn thresholds.

## SLO catalog reference

| SLO | Owner | Escalation | Scope | Objective | Replay lens | Budget exception | Runbook |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Ingest burn x | Ingest | Ingest on-call | tenant | 99.9% | tenant / source / result | Maintenance windows only | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneingestservicefastburn) |
| Enrich burn x | AI Pipeline | AI Pipeline on-call | tenant | 95.0% | tenant / dims_mode / result | No standing exclusions | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneenrichmentfastburn) |
| Outbox burn x | Delivery | Delivery on-call | destination type | 99.9% | destination_type / reason | Destination-maintenance only | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneoutboxdeliveryfastburn) |
| OIDC burn x | Auth | Auth on-call | global | 99.9% | result / auth flow | IdP-maintenance only | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneoidcloginfastburn) |
| API key burn x | Security | Security on-call | global | 95.0% | tenant / denial class | No standing exclusions | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneapikeyaccessfastburn) |
| MCP burn x | MCP | MCP on-call | tenant | 99.9% | tenant / tool / result | Tool-migration only | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attunemcptoolfastburn) |
| GDPR burn x | Compliance | Compliance on-call | tenant | 99.9% | tenant / request_type / result | Denominator already excludes cancellations | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attunegdprjobfastburn) |

## Report notes

- Current policy thresholds: fast burn 14.4x on 5m and 1h; slow burn 6x on 30m and 6h.
- Use the same owner / escalation / runbook metadata that appears in the routing table and comparison matrix.
- Attach the final report to the incident review or backfill ticket so the replay stays traceable.

