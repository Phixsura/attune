# Attune SLO Policy Reference

Use this worksheet as the starting policy for a new service-owned SLO. It keeps objective, burn windows, traffic guards, budget exceptions, and escalation metadata aligned with the generated reliability catalog.

## Shared defaults

- Each catalog entry keeps its own objective, but the burn policy stays consistent across the surface.
- Fast burn pages at 14.4x on 5m and 1h.
- Slow burn warns at 6x on 30m and 6h.
- Minimum traffic floor: > 0.01 over the label set chosen for each SLO.
- Diagnostic-only signals stay out of the burn denominator unless the catalog says otherwise.

## Catalog reference

| SLO | Recommended start | Traffic guard | Budget exception | Replay lens | Runbook |
| --- | --- | --- | --- | --- | --- |
| Ingest burn x | Start at 99.9% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | tenant traffic + rate-limit pressure | Maintenance windows only | tenant / source / result | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneingestservicefastburn) |
| Enrich burn x | Start at 95.0% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | tenant enrichment request volume | No standing exclusions | tenant / dims_mode / result | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneenrichmentfastburn) |
| Outbox burn x | Start at 99.9% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | destination_type delivery traffic | Destination-maintenance only | destination_type / reason | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneoutboxdeliveryfastburn) |
| OIDC burn x | Start at 99.9% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | login attempt traffic | IdP-maintenance only | result / auth flow | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneoidcloginfastburn) |
| API key burn x | Start at 95.0% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | API-key usage + denial traffic | No standing exclusions | tenant / denial class | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attuneapikeyaccessfastburn) |
| MCP burn x | Start at 99.9% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | tenant/tool call traffic | Tool-migration only | tenant / tool / result | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attunemcptoolfastburn) |
| GDPR burn x | Start at 99.9% objective; page at 14.4x on 5m and 1h; warn at 6x on 30m and 6h; keep traffic floor > 0.01. | tenant/request_type started jobs | Denominator already excludes cancellations | tenant / request_type / result | [Open runbook](https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#attunegdprjobfastburn) |

## Policy notes

- Ingest burn x: Keep validation failures diagnostic and fold rate-limit pressure into the service-owned failure ratio.
- Enrich burn x: Measure end-to-end completion within 5s so the SLI matches the user-visible latency experience.
- Outbox burn x: Pair the failure ratio with lag and dead rows to distinguish destination rejection from worker pressure.
- OIDC burn x: Treat failed sign-ins as a service-owned reliability signal while keeping IdP outages visible.
- API key burn x: Keep API-key access denials separate from role-based authorization denials so governance changes stay explainable.
- MCP burn x: Use tool mix and latency alongside burn rate to tell policy pressure from tool regressions.
- GDPR burn x: Keep cancelled and revoked jobs out of the completion denominator so the burn reflects active requests.

## Budget exceptions

- Ingest burn x: Use only for approved deploy or maintenance windows; validation failures and rate-limit pressure stay in burn.
- Enrich burn x: Transient provider slowness stays in burn; use a time-boxed exception only for planned model or provider migrations.
- Outbox burn x: Only exclude destination-side maintenance with owner approval; worker lag and dead rows stay in burn.
- OIDC burn x: Controlled IdP changes may be excluded when they are approved and time-boxed; implementation regressions remain in burn.
- API key burn x: Policy-driven denials are diagnostic signals, not error-budget events, so do not file budget exceptions for them.
- MCP burn x: Use a time-boxed exclusion only when a tool or adapter is being intentionally rotated; policy denials remain in burn.
- GDPR burn x: Cancelled and revoked jobs are already excluded from the burn denominator; file a budget exception only if the legal workflow changes.

## Operational guidance

- Keep the alerting label set stable so traffic floors stay meaningful.
- Re-run the policy reference whenever the catalog changes so dashboards, OpenSLO export, and Console cards stay aligned.
- Budget exceptions must stay time-boxed, owner-approved, and linked to the change or incident that justifies them.
- Use the replay comparison worksheet to validate that a historical outage would still page under the current policy.

