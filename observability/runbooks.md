# Attune alert runbooks

These runbooks are intentionally short and operational. They assume the first
reader is on-call, looking at an alert, and needs to decide whether users are
affected, where to look next, and what not to change prematurely.

## Triage order

1. Open the alert's `dashboard_url` annotation and keep the same time range as
   the alert.
2. Confirm the alert with the listed PromQL query in Prometheus.
3. Attribute impact by `tenant`, `channel`, `source_slug`, `model`,
   `destination_type`, or `reason` before changing global limits.
4. Check whether the alert is caused by a traffic spike, upstream dependency,
   worker saturation, customer configuration, or code/config regression.
5. Resolve only when the alert expression is healthy for at least one full
   `for:` window and the dashboard trend agrees.

## AttuneIngestValidationErrorsHigh

Impact: malformed ingest payloads are being rejected. Users may see missing
feedback if a client or source adapter changed its payload shape.

Confirm:

```promql
attune:ingest_validation_error_ratio:ratio5m
```

Inspect `Attune Overview > Traffic by source/result` and `Top tenants by volume`.
If only one tenant or source is affected, treat it as client/source drift first.
If all tenants spike together, inspect recent API contract, validation, or
deployment changes.

Recovery: validation error ratio is back below 2% for 10 minutes, and rejected
payload samples match expected behavior.

## AttuneIngestRateLimited

Impact: Attune is protecting itself by rejecting ingest bursts with 429.
Legitimate customer traffic may be delayed if clients retry politely; aggressive
retry loops can amplify the issue.

Confirm:

```promql
attune:ingest_rate_limit:rate5m
```

Inspect `Attune Overview > Top tenants by volume`. Decide whether the tenant is
bursting normally, retrying too aggressively, or configured with an undersized
limit. Avoid raising limits until you know whether backend workers and downstream
queues can absorb the traffic.

Recovery: rate-limit rate is near zero outside expected bursts and successful
ingest traffic remains normal.

## AttuneInboundAvailabilityLow

Impact: inbound adapters are receiving events but a meaningful share is failing.
Users may lose or delay feedback from the affected channel.

Confirm:

```promql
attune:inbound_availability:ratio5m
```

Inspect `Attune Inbound > Events by channel/result`, then `Top sources by
errors`. For one source, verify credentials, upstream API status, and payload
shape. For a whole channel, inspect adapter logs and recent deploys.

Recovery: availability is at or above 99% for 10 minutes and error source
breakdown no longer shows an active driver.

## AttuneInboundLatencyHigh

Impact: source intake is slow. Webhook callers may time out, and poll-mode
sources may fall behind.

Confirm:

```promql
attune:inbound_p95_seconds:5m
```

Compare `Inbound latency` with `Events by channel/result`. If traffic rose,
check worker capacity and database pressure. If traffic is flat, inspect upstream
source latency, adapter retries, and recent code/config changes.

Recovery: p95 is below 2s for 10 minutes and event throughput has not collapsed.

## AttuneInboundSourcesStale

Impact: one or more poll-mode sources have not refreshed for over 15 minutes.
Feedback from those sources may be missing.

Confirm:

```promql
attune:inbound_stale_sources:count
```

Inspect `Attune Inbound > Poll lag by source`. Verify worker scheduling, source
credentials, upstream API quota, and whether the source was intentionally
disabled.

Recovery: stale source count is zero for 15 minutes, or disabled sources are
confirmed intentional and no enabled source remains stale.

## AttuneEnrichmentLatencyHigh

Impact: AI enrichment is slow, delaying user-visible classification, routing, or
draft generation.

Confirm:

```promql
attune:enrich_p95_seconds:5m
```

Inspect `Attune AI Pipeline > Enrich duration`, then compare `Triage decisions`,
`AI queues`, and `Attune LLM Cost > Error calls by model`. High p95 with high
queue depth suggests worker saturation; high provider errors suggests dependency
failure; high full-AI share explains cost and latency pressure.

Recovery: p95 is below 5s for 10 minutes and queue depth drains or provider
errors stop.

## AttuneAIQueueBacklog

Impact: AI background jobs are accumulating. Users may see delayed embeddings,
search quality updates, or reply drafts.

Confirm:

```promql
attune:ai_queue_depth:sum
```

Inspect `Attune AI Pipeline > Downstream AI jobs`. Separate embedding queue from
reply-draft queue, then check worker liveness, provider latency, and database
claim contention before increasing concurrency.

Recovery: combined queue depth drains below 10 for 10 minutes and job completion
rates remain positive.

## AttuneLLMProviderErrors

Impact: LLM calls are reaching a provider but failing. AI enrichment, guardrail
flows, or reply draft generation may be incomplete.

Confirm:

```promql
attune:llm_provider_error_ratio:ratio5m
```

Inspect `Attune LLM Cost > Error calls by model` and model-specific traffic.
Check provider status, credentials, model availability, gateway routing, and
recent config changes. Compare with `Attune AI Pipeline > Enrich duration` before
changing retry behavior.

Recovery: provider error ratio is below 1% for 10 minutes and affected model
traffic has resumed normal success volume.

## AttuneOutboxLagHigh

Impact: notification delivery is delayed. Users may not receive outbound
notifications promptly even though ingest and enrichment succeeded.

Confirm:

```promql
attune:outbox_lag_seconds:max
```

Inspect `Attune Operations > Delivery and contention`. If lag rises with
notification failures, investigate destination health. If lag rises with low
failures and high contention, investigate worker capacity, lock contention, or
database pressure.

Recovery: oldest pending delivery age is below 60s for 10 minutes and delivery
failure rate is stable.

## AttuneNotifyFailures

Impact: outbound notifications are failing. Some integrations may not receive
updates.

Confirm:

```promql
sum(attune:notify_failures:rate5m)
```

Inspect `Attune Operations > Delivery and contention`, grouped by
`destination_type` and `reason`. Treat terminal failures differently from
transport failures. Verify customer webhook endpoints, Lark targets, secrets,
and outbound network health before replaying.

Recovery: failure rate is zero for 10 minutes and outbox lag is not increasing.

## AttuneAuthorizationDenialsHigh

Impact: users are being denied console actions. This can be normal access
control, role mapping drift, or suspicious probing.

Confirm:

```promql
sum(rate(attune_authz_denied_total[5m]))
```

Inspect `Attune Security & Compliance > Authorization denials`, split by role
and required permission. Compare with OIDC role mapping and recent admin or RBAC
changes.

Recovery: denial rate returns to expected baseline and affected users/roles are
understood.

## AttuneAuditRowsMissing

Impact: compliance evidence may be missing for authorization-denial paths. Treat
this as security-sensitive until proven otherwise.

Confirm:

```promql
sum(rate(attune_audit_rows_written_total[30m])) == 0
and
sum(rate(attune_authz_denied_total[30m])) > 0
```

Inspect `Attune Security & Compliance > Audit log`. Verify audit writer health,
database inserts, migrations, and recent authorization paths. Do not suppress
the alert until you know why denials occurred without audit writes.

Recovery: audit rows are written again for relevant actions and the alert is
inactive for at least one 30-minute evaluation window.
