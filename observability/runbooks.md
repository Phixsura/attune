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

## AttuneEnrichmentTerminalFailures

Impact: feedback rows have exhausted all enrichment retries and are stranded in
the `failed` state with no further retry scheduled. Those rows never get an AI
title, classification, or downstream fan-out until an operator intervenes.

Confirm:

```promql
sum by (tenant) (increase(attune_enrichment_terminal_failures_total[15m]))
```

Inspect `Attune AI Pipeline > Enrich duration` and sample the stranded rows'
`enrichment_error` column. Determine whether the cause is a provider outage, a
prompt/schema mismatch producing parse errors, or malformed input. Fix the root
cause, then clear `enrichment_status`/`enrichment_attempts` (or re-enqueue) so
the sweeper retries the affected rows.

Recovery: no new terminal failures for 15 minutes and re-enqueued rows reach
`done`.

## AttuneIngestServiceFastBurn

Impact: ingest is consuming service error budget rapidly. Users may see backend
errors or missing feedback when the service cannot accept or process requests
reliably.

Confirm:

```promql
attune:ingest_service_failure_ratio:ratio5m / 0.001
attune:ingest_service_failure_ratio:ratio1h / 0.001
```

Inspect `Attune Tenant Impact > Burn trend` and `Tenant burn ranking`.
If one tenant dominates, inspect `Tenant intake and attribution` for the source
and result split. If the failure is broad, compare against recent deploys and
dependency errors before changing any global limit.

Recovery: both burn-rate windows are below 14.4x and remain there for at least
10 minutes.

## AttuneIngestServiceSlowBurn

Impact: ingest is consuming service error budget steadily. This usually means a
sustained backend regression, repeated client misuse, or a capacity issue that
has not yet become a hard outage.

Confirm:

```promql
attune:ingest_service_failure_ratio:ratio30m / 0.001
attune:ingest_service_failure_ratio:ratio6h / 0.001
```

Inspect the same Tenant Impact panels as the fast-burn alert, then compare the
current failure rate with recent release changes. Use the slower windows to
separate a noisy spike from a sustained regression.

Recovery: both burn-rate windows are below 6x and the trend is flattening or
falling.

## AttuneEnrichmentFastBurn

Impact: enrichment latency is consuming the 5-second latency budget rapidly.
Feedback may still complete, but the AI pipeline is falling behind and user
latency is rising.

Confirm:

```promql
(1 - attune:enrich_success_under_5s:ratio5m) / 0.05
(1 - attune:enrich_success_under_5s:ratio1h) / 0.05
```

Inspect `Attune Tenant Impact > Burn trend`, `Enrichment and auth pressure`,
and `Attune AI Pipeline > Enrich duration`. Split the latency by `dims_mode`
and result, then determine whether the issue is provider latency, queue
pressure, or a parser / prompt regression.

Recovery: both burn-rate windows are below 14.4x and the 5s success ratio is
recovering.

## AttuneEnrichmentSlowBurn

Impact: enrichment is steadily consuming latency budget. This usually means the
system is saturated or a downstream provider is trending worse, even if the
service is not failing outright.

Confirm:

```promql
(1 - attune:enrich_success_under_5s:ratio30m) / 0.05
(1 - attune:enrich_success_under_5s:ratio6h) / 0.05
```

Compare the burn trend with AI queue depth and provider error signals. If the
burn stays elevated while failures remain low, the issue is likely sustained
latency or queue pressure rather than a hard outage.

Recovery: both burn-rate windows are below 6x for at least one full slower
window.

## AttuneOutboxDeliveryFastBurn

Impact: delivery to one or more destination types is failing rapidly. Users may
miss outbound notifications or see them arrive late.

Confirm:

```promql
attune:outbox_delivery_failure_ratio:ratio5m / 0.001
attune:outbox_delivery_failure_ratio:ratio1h / 0.001
```

Inspect `Attune Tenant Impact > Delivery pressure`. Compare terminal delivery
failures with `attune_outbox_lag_seconds` and `attune_notify_failures_total` to
decide whether the worker pool is behind or the destination is rejecting.

Recovery: both burn-rate windows are below 14.4x and outbox lag stops growing.

## AttuneOutboxDeliverySlowBurn

Impact: outbound delivery is steadily consuming error budget. This is often a
destination-side health or retry pressure issue that will become visible to
users if it continues.

Confirm:

```promql
attune:outbox_delivery_failure_ratio:ratio30m / 0.001
attune:outbox_delivery_failure_ratio:ratio6h / 0.001
```

Open the same delivery panel, then group by `destination_type` and
`reason`. Separate destination rejection from worker saturation before replaying
failed deliveries.

Recovery: both burn-rate windows are below 6x and outbox lag is stable or
falling.

## AttuneOIDCLoginFastBurn

Impact: sign-in is consuming error budget rapidly. Admins may be unable to
access the Console or complete auth flows.

Confirm:

```promql
attune:oidc_login_failure_ratio:ratio5m / 0.001
attune:oidc_login_failure_ratio:ratio1h / 0.001
```

Inspect `Attune Tenant Impact > Auth pressure`, then compare login outcomes
with recent IdP status, callback errors, and auth or cookie changes. If the
failures are mixed, split by result before changing configuration.

Recovery: both burn-rate windows are below 14.4x and successful sign-ins have
resumed.

## AttuneOIDCLoginSlowBurn

Impact: OIDC sign-in is steadily consuming budget. This often points to a
sustained IdP regression or a local auth configuration drift that has not yet
fully broken access.

Confirm:

```promql
attune:oidc_login_failure_ratio:ratio30m / 0.001
attune:oidc_login_failure_ratio:ratio6h / 0.001
```

Compare the slower burn with IdP health and the recent auth change history. Do
not change SSO settings until you know whether the failure is on the provider
side or in Attune.

Recovery: both burn-rate windows are below 6x and the login failure trend is
moving down.

## AttuneAPIKeyAccessFastBurn

Impact: API key access is failing rapidly. Users may be blocked by key
expiration, IP allowlists, or per-key throttling.

Confirm:

```promql
attune:apikey_access_denial_ratio:ratio5m / 0.05
attune:apikey_access_denial_ratio:ratio1h / 0.05
```

Inspect `Attune Tenant Impact > Deep dive`, then compare `API key denial %`
with `Attune Security & Compliance > API key access denials` and `API key
usage`. If the security page shows scope denials as well, treat that as a
separate authorization issue instead of an access failure.

Recovery: both burn-rate windows are below 14.4x and access-denial rate is back
below the budget target.

## AttuneAPIKeyAccessSlowBurn

Impact: API key access is steadily consuming budget. This usually means a
systemic expiry, allowlist, or rate-limit issue rather than a single malformed
request.

Confirm:

```promql
attune:apikey_access_denial_ratio:ratio30m / 0.05
attune:apikey_access_denial_ratio:ratio6h / 0.05
```

Inspect the same dashboard panels as the fast-burn alert, then split the
Security & Compliance page by denial class. Separate expiration, IP rejection,
and rate limiting before changing key policy or rotating credentials.

Recovery: both burn-rate windows are below 6x and the denial trend is falling.

## AttuneMCPToolFastBurn

Impact: an MCP tool is failing quickly. Users may see tool-call errors, bad
request handling, or a downstream adapter/service outage.

Confirm:

```promql
attune:mcp_tool_error_ratio:ratio5m
attune:mcp_tool_error_ratio:ratio1h
```

Inspect `Attune Tenant Impact > MCP`, then split `MCP tool mix` by `tool` and
`result`. If `client_error` dominates, validate the tool input contract and the
JSON-RPC method mapping first. If `internal_error` dominates, inspect the
adapter, downstream service, and recent deploys before changing policy.

Recovery: both burn-rate windows are below 14.4x for 10 minutes and the error
mix returns to normal.

## AttuneMCPToolSlowBurn

Impact: an MCP tool is steadily consuming budget. This usually means a tool
contract drift, repeated bad input, or a slow downstream dependency.

Confirm:

```promql
attune:mcp_tool_error_ratio:ratio30m
attune:mcp_tool_error_ratio:ratio6h
```

Use the same MCP panels, then compare `MCP latency by tool` with the error mix.
If latency rises without a matching error spike, look for a slow adapter or
backend. If errors rise with flat latency, inspect the policy layer and
call-shape drift.

Recovery: both burn-rate windows are below 6x and the tool mix is stable.

## AttuneGDPRJobFastBurn

Impact: GDPR export or delete jobs are not completing quickly. Customer data
requests may pile up, and operators may need to watch for backlog growth.

Confirm:

```promql
1 - attune:gdpr_job_completion_ratio:ratio5m
1 - attune:gdpr_job_completion_ratio:ratio1h
```

Inspect `Attune Tenant Impact > GDPR`, then compare `GDPR state mix`,
`GDPR latency by request`, and `GDPR backlog`. If `failed` dominates, inspect
worker logs and storage errors. If `started` dominates, the queue is backing up
or workers are not keeping up.

Recovery: both burn-rate windows are below 14.4x and the backlog delta is
flattening or falling.

## AttuneGDPRJobSlowBurn

Impact: GDPR job completion is steadily consuming budget. This usually means
worker throughput is lagging or the storage path is trending worse.

Confirm:

```promql
1 - attune:gdpr_job_completion_ratio:ratio30m
1 - attune:gdpr_job_completion_ratio:ratio6h
```

Use the same GDPR panels, then compare the request mix with latency. If
`cancelled` or `revoked` grows, confirm whether the work was intentionally
stopped or whether an operator is masking a backlog.

Recovery: both burn-rate windows are below 6x and the backlog trend is stable
or falling.

## AttuneWorkerPanics

Impact: a supervised background worker (outbox, enrichment, embedding,
reply-draft, digest, GDPR export, audit pruner, queue/lag refreshers) is hitting
an unhandled panic. The `safego` / enrich-runner supervisor recovers it and
restarts with capped backoff, so the process stays up — but a worker stuck in a
panic→restart loop silently stops making progress on its subsystem.

Confirm:

```promql
sum by (worker) (increase(attune_worker_panics_total[10m]))
```

Identify the panicking worker from the `worker` label, then grep logs for the
recovered-panic stack (`worker panicked — recovered`, same `worker` value). A
single panic may be a transient poison input; a sustained count means a
persistent bug (nil-deref, malformed upstream payload). Fix the root cause; if a
specific row/message is poison, quarantine or drop it.

Recovery: no new panics for the worker over 10 minutes.

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

## AttuneOutboxDeadRowsHigh

Impact: one or more deliveries exhausted their retries (or hit a terminal 4xx)
and are parked in the `dead` queue. The destination is not receiving those
notifications until an operator intervenes. A common cause is a webhook that was
deleted or rotated on the destination side — e.g. a removed Discord/Slack
channel webhook returns 404 (terminal), or a revoked token returns 403.

Confirm:

```promql
attune_outbox_dead_rows
```

Open `Console > Dead deliveries`. For each row, inspect `destination_type` and
`last_error` (the URL is redacted to scheme://host — the host tells you which
integration). Fix or remove the offending notify target, then use the per-row
manual retry. If the target is permanently gone, delete it so new feedback stops
queuing failures.

Recovery: the dead-row gauge returns to zero after the targets are fixed and the
parked rows are retried or cleared.

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

## AttuneMigrationsPending

Impact: the binary has unapplied migrations that were not applied at startup.
Schema changes are not yet in effect, which may cause runtime errors or missing
features.

Confirm:

```promql
attune_migration_pending
```

Inspect the migration runner logs for errors. Common causes: a migration SQL
syntax error, database connectivity issues, lock contention from long-running
transactions, or insufficient privileges. If the migration requires manual
intervention (data backfill, constraint addition on large tables), apply it
out-of-band and restart the process.

Recovery: pending count is zero after the migrations are applied or the
deployment is rolled back.

## AttuneMigrationChecksumDrift

Impact: a migration file was edited after being applied to the database. This is
a release hygiene violation — the schema may differ from what the source code
expects, or different binary builds may have different schema expectations.

Confirm:

```promql
increase(attune_migration_checksum_drift_total[1h])
```

Compare the embedded checksums in the running binary (`attune migrate list`)
with the `checksum` column in `schema_migrations`. Identify which migration
drifted and why. If the edit was intentional (a post-apply fix), create a
remediation migration that either applies the fix idempotently or records the
new checksum. If the edit was accidental, restore the original file content and
rebuild.

Recovery: no new checksum drift events and the alert is inactive for at least
one 1-hour evaluation window.

## AttuneRestoreDrillStale

Impact: the most recent passing backup/restore drill is more than 8 days old, so
the recoverability of the latest backup — including the decryptability of managed
secrets — is unverified. This is a process lapse, not a live outage.

Confirm:

```promql
time() - max(attune_restore_drill_last_success_timestamp_seconds)
```

Run a drill against a restored throwaway database and record it (the DSNs omit
the password — supply it via `PGPASSWORD` or `~/.pgpass`):

```bash
attune --config ./config.yaml restore-drill run \
  --target "postgres://attune@restore-target:5432/attune?sslmode=disable" \
  --baseline-url "postgres://attune@attune-postgres:5432/attune?sslmode=disable" \
  --record
```

If drills are meant to run in-cluster, check the `restoreDrill` CronJob
(`restoreDrill.enabled=true` in the Helm values) and its last Job's logs. See
the "Restore drills" section in `docs/private-deploy.md`. A drill that has never
run surfaces as a warning on the Console System Readiness page
(`backup:restore_drill`) rather than firing this alert.

Recovery: a fresh `attune restore-drill run --record` completes with status
`pass` (or `warn`), advancing
`attune_restore_drill_last_success_timestamp_seconds`.

## AttuneWorkerHeartbeatLost

Impact: workers are losing task leases before completing processing. This causes
wasted work (partial LLM calls, partial deliveries) and increased task latency
as tasks are re-processed by other workers.

Confirm:

```promql
sum by (worker) (increase(attune_worker_heartbeat_total{outcome="lost"}[10m]))
```

Check if task processing time regularly exceeds the stale claim threshold (5
minutes). Common causes:

1. **Long LLM calls** — complex enrichment or embedding prompts taking > 5m
2. **Network issues** — database connectivity problems preventing heartbeat refresh
3. **Resource contention** — worker CPU/memory pressure slowing processing
4. **Destination timeouts** — slow webhook endpoints delaying outbox delivery

Recovery: heartbeat lost rate returns to near-zero for 10 minutes. If the cause
is long processing, consider:
- Breaking large tasks into smaller units
- Increasing the stale claim threshold (staleDuration) if appropriate
- Adding more worker replicas to reduce per-task latency

## AttuneWorkerDrainTimeout

Impact: worker shutdown did not complete gracefully within 30 seconds. In-flight
tasks may have been abandoned mid-processing and will be re-claimed after the
stale claim threshold.

Confirm:

```promql
sum by (worker) (increase(attune_worker_drain_total{outcome="timeout"}[30m]))
```

Check for stuck tasks by querying queue tables for rows with `status='processing'`
and `claimed_at` older than the stale threshold. Common causes:

1. **Stuck LLM calls** — provider not responding, no timeout configured
2. **Stuck database queries** — lock contention or slow queries
3. **Many in-flight tasks** — drain timeout too short for batch size

Recovery: drain timeout rate returns to zero. For persistent issues:
- Add context timeouts to long-running operations
- Reduce batch size to lower in-flight count at shutdown
- Increase drain timeout if 30s is genuinely insufficient

## AttuneWorkerStaleClaimsHigh

Impact: workers are crashing or failing to heartbeat, leaving claimed tasks
stranded until the next worker boot recovers them. This adds latency equal to
the stale claim threshold (5 minutes) plus recovery poll interval.

Confirm:

```promql
sum by (worker) (increase(attune_worker_stale_claims_recovered_total[1h]))
```

Check for worker OOM kills, panics, or network partitions. The stale claim
recovery log shows which tasks were recovered:

```bash
kubectl logs -l app=attune | grep "reset stale claims"
```

Common causes:

1. **Worker crashes** — panics, OOM, SIGKILL without graceful shutdown
2. **Network partition** — worker alive but can't reach database for heartbeat
3. **Clock skew** — significant time drift between workers and database

Recovery: stale claim recovery rate returns to near-zero for 1 hour. Address the
underlying cause (memory limits, panic bugs, network stability).

## AttuneGlobalEnrichmentLatencyHigh

Impact: global enrichment latency is taking longer than expected, which delays
feedback processing and may cause visible delays in the user-facing workflow.

Confirm:

```promql
histogram_quantile(0.95, sum by (le) (rate(attune_enrich_duration_seconds_bucket[5m])))
```

Check LLM provider status pages. Compare latency by model in Attune AI Pipeline >
Enrichment latency. If one model is slow, check its capacity and error rate.

Recovery: enrichment p95 returns below 30s for 10 minutes.

## AttuneSearchLatencyHigh

Impact: search queries are slow, causing visible delays in the feedback search UI.

Confirm:

```promql
histogram_quantile(0.95, sum by (le, type) (rate(attune_search_query_duration_seconds_bucket[5m])))
```

Check pg_stat_statements for slow queries:

```sql
SELECT query, mean_exec_time, calls
FROM pg_stat_statements
WHERE query LIKE '%user_feedback%' OR query LIKE '%embedding%'
ORDER BY mean_exec_time DESC LIMIT 10;
```

Consider VACUUM ANALYZE on large tables. Verify embedding indexes are current.

Recovery: search p95 returns below 1s for 10 minutes.

## AttuneCircuitBreakerOpen

Impact: requests to the affected upstream are fast-failing. This protects the system
but means the upstream service is degraded.

Confirm:

```promql
sum by (name) (increase(attune_circuit_breaker_transitions_total{to="open"}[10m]))
```

Check the upstream provider's status page. The circuit will automatically transition
to half-open after the configured open duration (default 30s) and test with a single
request. If that succeeds, it closes.

Recovery: circuit transitions to closed. If repeatedly opening, address the root cause.

## AttuneEmbeddingQueueDepthHigh

Impact: embedding tasks are backing up, which delays cluster assignments and
semantic search availability for new feedback.

Confirm:

```promql
max(attune_embed_queue_depth)
```

Check embedding worker logs for errors. Verify provider connectivity. If workers
are healthy but queue is growing, scale horizontally.

Recovery: queue depth returns to near zero for 10 minutes.

---

## AttuneCohortSyncSourceError

**Summary:** A cohort sync source has been failing for 30+ minutes.

**Investigation:**

1. Open Console → Integrations → Cohort Sync.
2. Identify the source with error status.
3. Check the last error message in the source detail.
4. Verify the provider webhook configuration and credentials.

Confirm:

```promql
attune_cohort_sync_runs_total{status="failed"}
```

Recovery: source status returns to "active" after a successful sync run.

## AttuneCohortSyncWebhookErrors

**Summary:** Cohort sync webhook error rate is elevated.

**Investigation:**

1. Check webhook handler logs for auth failures or parse errors.
2. Verify the provider is sending valid payloads.
3. Check if the source credentials are still valid.

Confirm:

```promql
rate(attune_cohort_sync_webhook_requests_total{status="error"}[10m])
```

Recovery: webhook error rate drops below 0.1/s for 15 minutes.

## AttuneAnomalyWorkerLagHigh

The anomaly & spike detection worker (#237) has settled feedback-volume
buckets older than 2 days that no worker instance has judged.

1. Confirm the worker is alive: `attune_worker_panics_total{worker="anomaly"}` stays flat
   and the process logs for `service.anomaly.Worker` errors.
2. Look for stuck runs: `SELECT * FROM anomaly_detection_runs WHERE status
   IN ('failed','running') ORDER BY bucket_date` — failed runs re-claim
   automatically each tick, so persistent failures point at a recurring
   error (the `error` column has the last message).
3. Check rollup latency on the AI Pipeline dashboard (panel 25). Slow
   recomputes starve detection; a tenant with a pathological custom slice
   condition is the usual cause — disable it in Console > Configuration >
   Anomaly detection.
4. After downtime the worker catches up at most 14 days per tick; longer
   gaps clear over successive hourly ticks. Lag should fall monotonically —
   alert only if it does not.

## AttuneAnomalyNotifyFailures

Anomaly notifications (immediate mode) are failing to deliver. Detection
itself is unaffected — events are recorded and visible in Console — but
operators relying on webhooks/chat are blind.

1. Identify the tenant: `sum by (tenant) (rate(attune_anomaly_notify_failures_total[15m]))`.
2. Verify the tenant's radar-audience notify target (Console >
   Integrations > Notify targets): URL reachable, secret current.
3. lark-bot and slack-bot destinations REJECT non-native payload shapes;
   if the URL is actually a raw webhook (or vice versa) fix the
   destination type.
4. Deliveries are at-least-once: once the target recovers, unnotified
   open events are re-sent on the next hourly tick automatically.
