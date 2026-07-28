# attune observability

attune ships its own observability **contract** so it can deploy self-contained.
Prometheus/Grafana is just one reference stack — the stable surface is the metrics
exposition plus the portable assets in this directory.

## Layers

- **Exposition contract** — attune serves Prometheus/OpenMetrics at
  `:8090/metrics` (no app-level auth; restrict via your proxy / internal network).
  Any compatible backend scrapes it: Prometheus, VictoriaMetrics (`vmagent`),
  Grafana Agent, the OpenTelemetry Collector (`prometheusreceiver`), Datadog's
  OpenMetrics check. Metric names + labels are a stable contract — renaming one is
  a breaking change.
- **Portable assets (this dir)** — backend-agnostic:
  - `dashboards/*.json` — Grafana dashboards with **no hardcoded datasource**, so
    they render against whatever default Prometheus-compatible datasource you
    provision (our bundled Prometheus, your VictoriaMetrics, …).
  - `rules/*.yml` — Prometheus recording and alert rules for Attune SLI, latency,
    backlog, provider, inbound, and security signals.
  - `targets.yaml` — a `file_sd_configs` target list for an **external**
    Prometheus/VictoriaMetrics reading `127.0.0.1:8090` (the standalone-host case).
- **Reference runtime** — `../deploy/docker-compose.obs.yml` bundles Prometheus +
  Grafana and auto-provisions the datasource + dashboards. Its `prometheus.yml`
  targets the compose service `attune:8090` (not `targets.yaml`). Bring your own
  backend instead by pointing it at `/metrics`.

## Metrics reference

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `attune_ingest_total` | counter | `tenant`, `source`, `result` | ingest API requests |
| `attune_enrich_duration_seconds` | histogram | `tenant`, `dims_mode`, `result` | end-to-end AI enrichment latency |
| `attune_enrich_attrs_dropped_total` | counter | `tenant`, `dim` | per-dim attr values removed by whitelist filter (#10 → E3) |
| `attune_enrich_suggested_attrs_total` | counter | `tenant`, `dim` | enrich rows with off-list attr suggestions per dim (#10 → E3) |
| `attune_enrich_attrs_size_bytes` | histogram | `tenant` | serialized enriched_attrs JSONB size, per tenant (#10 → E3) |
| `attune_enrich_attrs_rejected_total` | counter | `tenant` | rows refused because enriched_attrs exceeded `MaxAttrsBytes` (32 KiB) |
| `attune_enrich_queue_depth` | gauge | — | current in-process enrichment queue depth |
| `attune_enrich_queue_full_total` | counter | — | non-blocking enrichment queue submit rejections caused by a full queue |
| `attune_enrich_batch_size` | histogram | — | actual jobs executed per enrichment processor batch |
| `attune_enrich_sweep_submitted_total` | counter | — | pending DB rows successfully resubmitted by the enrich sweeper |
| `attune_enrichment_terminal_failures_total` | counter | `tenant` | feedback rows that exhausted enrichment retries and stopped in `failed` (#64) |
| `attune_enrichment_terminal_failures_by_reason_total` | counter | `tenant`, `reason_class` | terminal enrichment failures split by stable reason class |
| `attune_classification_quality_drift_score` | gauge | `tenant`, `dimension` | latest classification value-distribution drift score by dimension |
| `attune_classification_quality_low_confidence_ratio` | gauge | `tenant` | latest share of classification events below the dashboard confidence threshold |
| `attune_classification_quality_off_list_ratio` | gauge | `tenant` | latest share of classification events with off-list value suggestions |
| `attune_classification_quality_parse_failure_ratio` | gauge | `tenant` | latest share of classification attempts that failed while parsing provider output |
| `attune_classification_quality_terminal_failure_ratio` | gauge | `tenant` | latest share of classification attempts that reached terminal enrichment failure |
| `attune_classification_quality_warning_active` | gauge | `tenant`, `reason`, `severity` | active classification-quality dashboard warnings by reason and severity |
| `attune_notify_failures_total` | counter | `destination_type`, `reason` | notifier push failures |
| `attune_outbound_delivery_attempts_total` | counter | `destination_type`, `result`, `status` | outbound provider delivery attempts, including retryable and terminal responses |
| `attune_outbound_delivery_duration_seconds` | histogram | `destination_type`, `result` | end-to-end outbound provider delivery duration, including retry waits |
| `attune_outbound_retry_after_total` | counter | `destination_type` | retryable outbound provider responses that supplied `Retry-After` |
| `attune_outbox_lag_seconds` | gauge | — | age of the oldest pending outbox row (0 = empty) |
| `attune_outbox_dead_rows` | gauge | — | notify_outbox rows in the terminal `dead` state (dead-letter depth) |
| `attune_claim_contention_total` | counter | — | enricher `tryClaim` lost to another worker |
| `attune_ingest_rate_limit_total` | counter | `tenant` | ingest requests rejected (429) by the rate limiter |
| `attune_triage_decisions_total` | counter | `tenant`, `decision` | triage-stage routing decisions |
| `attune_guard_actions_total` | counter | `tenant`, `stage`, `guard`, `entity`, `action` | guard actions applied at AI and outbound boundaries; raw findings are never labels |
| `attune_guard_blocked_total` | counter | `tenant`, `stage`, `guard`, `reason` | operations blocked by guard policies before external calls |
| `attune_llm_calls_total` | counter | `tenant`, `model`, `status` | LLM provider calls that reached a backend |
| `attune_llm_tokens_total` | counter | `tenant`, `model`, `direction` | provider-reported prompt/completion token usage |
| `attune_llm_cost_usd_total` | counter | `tenant`, `model` | estimated USD cost from the static model-price table |
| `attune_llm_rate_limit_wait_seconds` | histogram | — | time spent waiting for the local outbound LLM rate limiter |
| `attune_embed_cluster_assignments_total` | counter | `tenant`, `cluster_type` | feedback items assigned to clusters (#25) |
| `attune_embed_errors_total` | counter | `tenant`, `error_type` | embedding processing errors by type (#25) |
| `attune_embed_duration_seconds` | histogram | `tenant` | end-to-end embedding and clustering latency per row (#25) |
| `attune_embed_queue_depth` | gauge | `tenant` | number of pending embedding tasks per tenant (#25) |
| `attune_reply_draft_generated_total` | counter | `tenant` | reply drafts successfully generated and stored (#26) |
| `attune_reply_draft_errors_total` | counter | `tenant`, `error_type` | reply-draft generation errors by type (#26) |
| `attune_reply_draft_duration_seconds` | histogram | `tenant` | end-to-end reply-draft generation latency per row (#26) |
| `attune_reply_draft_queue_depth` | gauge | `tenant` | number of pending reply-draft tasks per tenant (#26) |
| `attune_digest_runs_total` | counter | `tenant`, `status` | daily digest runs by outcome — `sent` / `skipped_empty` / `failed` (#27) |
| `attune_digest_duration_seconds` | histogram | `tenant` | end-to-end digest aggregation + delivery latency per run (#27) |
| `attune_digest_clustering_fallback_total` | counter | `tenant`, `reason` | cluster-based theme extraction fallbacks to naive path (#27) |
| `attune_digest_cluster_count` | histogram | `tenant` | number of clusters found per digest run (#27) |
| `attune_external_sync_runs_total` | counter | `provider`, `object_type`, `result` | external sync run outcomes by provider and object type (#214) |
| `attune_external_sync_records_total` | counter | `provider`, `object_type`, `operation`, `result` | external sync record-level operations and outcomes (#214) |
| `attune_external_sync_run_duration_seconds` | histogram | `provider`, `object_type`, `result` | external sync run latency by provider, object type, and outcome (#214) |
| `attune_external_sync_lag_seconds` | gauge | `provider`, `object_type` | age of the latest successful external sync cursor by provider and object type (#214) |
| `attune_external_sync_conflicts_total` | counter | `provider`, `object_type`, `resolution` | external sync conflicts resolved or ignored by resolution (#214) |
| `attune_external_sync_dead_runs` | gauge | `provider`, `object_type` | external sync runs exhausted after retries and requiring operator attention (#214) |
| `attune_cohort_sync_webhook_requests_total` | counter | `provider`, `status` | cohort sync webhook requests by provider and outcome (#233) |
| `attune_cohort_sync_members_changed_total` | counter | `provider`, `action` | cohort membership changes by provider and add/remove action (#233) |
| `attune_cohort_sync_runs_total` | counter | `provider`, `trigger`, `status` | cohort sync runs by provider, trigger, and status (#233) |
| `attune_cohort_sync_active_members` | gauge | `provider` | active cohort members by provider (#233) |
| `attune_cohort_sync_stale_members_cleaned_total` | counter | — | stale cohort memberships cleaned up by TTL (#233) |
| `attune_cohort_sync_run_duration_seconds` | histogram | `provider`, `trigger` | cohort sync run latency (#233) |
| `attune_workflow_transitions_total` | counter | `tenant`, `result` | workflow state transitions by outcome — `success` / `invalid` / `error` (#29) |
| `attune_workflow_batch_size` | histogram | — | number of feedback items per batch-transition call (#29) |
| `attune_batch_jobs_claimed_total` | counter | `tenant` | async batch jobs claimed by workers (#30) |
| `attune_batch_jobs_completed_total` | counter | `tenant`, `status` | async batch jobs completed by outcome (#30) |
| `attune_batch_job_duration_seconds` | histogram | `tenant` | async batch job processing latency (#30) |
| `attune_batch_jobs_recovered_total` | counter | — | stuck batch jobs recovered and requeued (#30) |
| `attune_batch_operations_total` | counter | `tenant`, `operation`, `status` | batch operations by type and outcome (#30) |
| `attune_batch_operation_items_total` | counter | `tenant`, `operation`, `result` | items processed in batch operations (#30) |
| `attune_batch_operation_duration_seconds` | histogram | `tenant`, `operation`, `mode` | batch operation latency by mode (#30) |
| `attune_idempotency_key_usage_total` | counter | `tenant`, `outcome` | idempotency key usage by outcome (#30) |
| `attune_search_queries_total` | counter | `tenant`, `type` | search queries by type (#30) |
| `attune_search_query_duration_seconds` | histogram | `tenant`, `type` | search query latency by type (#30) |
| `attune_search_results_count` | histogram | `tenant` | number of search results returned (#30) |
| `attune_search_fallback_reasons_total` | counter | `tenant`, `reason` | degraded search paths by stable fallback reason (#162) |
| `attune_search_embedding_coverage_ratio` | gauge | `tenant`, `model` | share of live feedback rows with embeddings for search (#162) |
| `attune_embedding_cache_hits_total` | counter | `tenant`, `result` | embedding cache hits vs misses (#30) |
| `attune_oidc_login_total` | counter | `result` | OIDC login attempts by result (#40) |
| `attune_oidc_login_duration_seconds` | histogram | — | end-to-end OIDC login latency (#40) |
| `attune_oidc_token_exchange_duration_seconds` | histogram | — | OIDC token exchange latency (#40) |
| `attune_oidc_role_mapping_total` | counter | `role` | OIDC role mappings by assigned role (#40) |
| `attune_authz_denied_total` | counter | `role`, `required` | Authorization denials by user role and required role (#38) |
| `attune_apikey_scope_denied_total` | counter | `scope` | API key scope enforcement denials by required scope (#41) |
| `attune_apikey_expired_total` | counter | — | API key requests denied due to key expiration |
| `attune_apikey_ip_denied_total` | counter | — | API key requests denied due to IP not in allowlist |
| `attune_apikey_rate_limited_total` | counter | `tenant` | requests rejected (429) by the per-key rate limiter (key's `rate_limit_rpm`) (#41) |
| `attune_apikey_usage_total` | counter | `tenant`, `key_prefix` | Successful API key authentications by key prefix |
| `attune_mcp_tool_calls_total` | counter | `tenant`, `tool`, `result` | MCP tool-call outcomes by tenant, tool, and result |
| `attune_mcp_tool_latency_seconds` | histogram | `tenant`, `tool` | MCP tool-call latency by tenant and tool |
| `attune_gdpr_job_total` | counter | `tenant`, `request_type`, `result` | GDPR job lifecycle events by tenant, request type, and result |
| `attune_gdpr_job_duration_seconds` | histogram | `tenant`, `request_type` | GDPR job duration by tenant and request type |
| `attune_audit_rows_written_total` | counter | `action` | immutable audit-log rows written by action (#39) |
| `attune_audit_rows_pruned_total` | counter | — | immutable audit-log rows pruned by retention policy (#39) |
| `attune_audit_prune_duration_seconds` | histogram | — | audit-log retention prune latency (#39) |
| `attune_worker_panics_total` | counter | `worker` | recovered panics in supervised background workers (#64) |
| `attune_worker_drain_total` | counter | `worker`, `status` | graceful shutdown drain events by worker and outcome (#155) |
| `attune_worker_in_flight` | gauge | `worker` | items currently being processed by each worker type (#155) |
| `attune_worker_stale_claims_recovered_total` | counter | `worker` | stale claims recovered on worker boot (#155) |
| `attune_worker_heartbeat_total` | counter | `worker`, `outcome` | heartbeat refresh attempts by worker (#155) |
| `attune_advisory_lock_total` | counter | `lock`, `outcome` | advisory lock acquire attempts (#155) |
| `attune_circuit_breaker_results_total` | counter | `name`, `result` | circuit breaker call outcomes (#155) |
| `attune_circuit_breaker_rejected_total` | counter | `name` | requests rejected by open circuit breaker (#155) |
| `attune_circuit_breaker_transitions_total` | counter | `name`, `from`, `to` | circuit breaker state transitions (#155) |
| `attune_inbound_total` | counter | `channel`, `tenant`, `source_slug`, `result` | channel-agnostic inbound events by source (#66) |
| `attune_inbound_latency_seconds` | histogram | `channel`, `tenant`, `source_slug` | end-to-end inbound processing latency (#66) |
| `attune_inbound_source_state` | gauge | `channel`, `tenant`, `source_slug`, `state` | inbound source state, 1 when active (#66) |
| `attune_inbound_poll_lag_seconds` | gauge | `channel`, `tenant`, `source_slug` | seconds since last successful poll for poll-mode sources (#66) |
| `attune_migration_applied_total` | counter | `version`, `filename` | migrations applied by the startup runner (#149) |
| `attune_migration_apply_duration_seconds` | histogram | `version` | migration apply latency per version (#149) |
| `attune_migration_pending` | gauge | — | number of unapplied migrations at startup (#149) |
| `attune_migration_checksum_drift_total` | counter | — | migration checksum mismatches detected during verification (#149) |
| `attune_dependency_health_check_total` | counter | `dependency`, `result` | dependency health check outcomes (#155) |
| `attune_dependency_health_check_duration_seconds` | histogram | `dependency` | dependency health check latency (#155) |
| `attune_restore_drill_last_success_timestamp_seconds` | gauge | — | unix timestamp of the most recent non-failing restore drill, derived from `restore_drill_runs` at scrape time (#151) |
| `attune_restore_drill_runs_total` | counter | `status` | restore drills recorded, by status (#151) |
| `attune_restore_drill_last_rto_seconds` | gauge | — | measured restore duration (RTO) of the most recent drill that recorded one (#151) |
| `attune_audit_evidence_exports_total` | counter | `tenant`, `status` | audit evidence export jobs by outcome (#152) |
| `attune_audit_evidence_export_duration_seconds` | histogram | `tenant` | audit evidence export processing latency (#152) |
| `attune_audit_evidence_export_size_bytes` | histogram | `tenant` | audit evidence archive size in bytes (#152) |

Label values:

- `source` — one of `api`, `lark-group`, `lark-bitable`, `lark-approval`,
  `lark-helpdesk`, `lark-form`, `email`, `web`, `other`; or `invalid` when a
  request's source failed validation.
- ingest `result` — `ok` · `validate_err` · `auth_err` · `internal_err`.
- enrich `dims_mode` — `freeform` · `constrained` (set per the tenant's `DimensionSet`: `constrained` when at least one dim has a non-empty taxonomy).
- enrich `dim` — the stable `Dimension.Name` (e.g. `type`, `severity`, `labels`).
- enrich `result` — `ok` · `llm_err` · `parse_err` · `other_err` · `db_err`.
- `destination_type` — `raw-webhook` · `slack` · `lark` · `discord` · `github-issue`.
- `reason` — `transport` · `terminal`.
- outbound `result` — `success` · `retryable` · `terminal` · `exhausted` · `canceled`.
- outbound `status` — HTTP status code, or `0` when no response was received.
- mcp `result` — `ok` · `client_error` · `denied` · `rate_limited` · `internal_error`.
- mcp `tool` — bounded tool names from the MCP dispatcher; uncategorized JSON-RPC requests use `unknown`.
- gdpr `request_type` — `export` · `delete`.
- gdpr `result` — `started` · `completed` · `failed` · `cancelled` · `revoked`.
- `decision` — `ignore` · `fast` · `full`.
- guard `stage` — `llm_input` · `llm_output` · `outbound` · `tool_call`.
- guard `action` — `audit` · `redact` · `hash` · `tokenize` · `block`.
- guard `entity` — bounded detector-owned names such as `email`, `phone`,
  `cn_mobile`, `cn_id`, `credit_card`.
- LLM `status` — `ok` · `error`.
- LLM token `direction` — `prompt` · `completion`.
- LLM `model` — configured model id; keep private gateway aliases bounded.
- embed `cluster_type` — `new` · `existing`.
- embed `error_type` — `embed_api` · `find_similar` · `update_embedding`.
- batch `operation` — `tag` · `workflow` · `delete`.
- batch `status` — `success` · `error` · `rate_limited`.
- batch `result` — `succeeded` · `skipped` · `failed`.
- batch `mode` — `sync` · `async`.
- batch job `status` — `completed` · `failed`.
- idempotency `outcome` — `new` · `cache_hit` · `conflict` · `in_progress` · `failed`.
- OIDC `result` — `success` · `state_invalid` · `state_expired` · `state_mismatch` ·
  `idp_error` · `missing_code` · `token_exchange_failed` · `no_id_token` · `id_token_invalid` ·
  `nonce_mismatch` · `claims_invalid` · `group_denied` · `user_sync_failed` · `session_failed`.
- OIDC `role` — `admin` · `member` or custom roles from `role_mapping` config.
- search `type` — `semantic` · `keyword_fallback` · `hybrid`.
- search fallback `reason` — `semantic_unavailable` · `embedding_check_failed` ·
  `embedding_stats_failed` · `no_embeddings` · `embedding_generation_failed` ·
  `embedding_model_mismatch` · `semantic_search_failed` · `no_semantic_matches`.
- search embedding `model` — configured embedding model id; empty coverage is
  reported as `none`.
- embedding cache `result` — `hit` · `miss`.
- inbound `channel` — bounded adapter channel names such as `email` or `webhook`.
- inbound `source_slug` — operator-defined source slug from `inbound_sources`.
- inbound `state` — bounded source state labels such as `enabled`.
- inbound `result` — bounded adapter result labels such as `ok` or `error`.

The registered set is drift-guarded by `internal/infra/metrics/metrics_test.go` —
and `internal/tools/observabilitydash` — it must match this table and first-party
dashboard coverage.

## Dashboard suite

First-party Grafana dashboards are generated from
`internal/tools/observabilitydash` and committed in two distribution locations:

- `observability/dashboards/*.json` for Docker Compose and external monitoring
  stacks.
- `deploy/helm/attune/dashboards/*.json` for the Helm chart's dashboard
  ConfigMap.

Dashboards:

- `Attune Overview` — landing page for traffic, latency, rate limits, triage,
  backlog, delivery, and top-level risk signals.
- `Attune Tenant Impact` — burn-rate overview, impacted-tenant ranking, and
  ingest/enrichment/outbox/auth/API-key/MCP/GDPR drilldowns for SLO pages.
- `Attune Inbound` — channel/source volume, latency, source state, and poll lag.
- `Attune AI Pipeline` — enrichment, triage, guardrails, embedding, reply draft,
  and digest health.
- `Attune Operations` — workflow, batch operations, search, idempotency, outbox,
  notify, and worker contention.
- `Attune Security & Compliance` — OIDC, authorization, audit, and guard policy
  activity.
- `Attune LLM Cost` — LLM calls, tokens, provider errors, and cost.

## How to read the dashboards

Start with `Attune Overview`. It is organized around the SRE golden-signal / RED
shape:

- **Traffic** answers whether feedback is arriving at the expected rate.
- **Validation error %** separates client/schema issues from backend failures.
- **Rate-limited** shows whether tenants are being protected from bursts or are
  blocked by a too-low limit.
- **Needs AI** explains cost and latency pressure by showing how much feedback
  reaches full LLM enrichment.
- **Enrich p95** is the primary user-facing AI latency signal.
- **Outbox lag** is the delivery backlog signal; rising lag with flat traffic
  means worker or destination pressure.

Then drill down:

- Traffic down: open `Attune Inbound`, inspect source state and poll lag.
- Validation errors up: inspect `Traffic by source/result` and the affected
  source/client before changing server code.
- Rate-limited up: inspect `Top tenants by load`; decide whether it is customer
  burst behavior, abuse, or an undersized tenant limit.
- Needs AI or Enrich p95 up: open `Attune AI Pipeline`, then compare triage mix,
  guard blocks, provider errors, and queue depth.
- Outbox lag up: open `Attune Operations`; compare lag with notification
  failures and worker claim contention.
- Auth, audit, or guard signals up: open `Attune Security & Compliance` and
  inspect the role/action/reason breakdown before treating it as noise.
- Cost up: open `Attune LLM Cost`; compare calls, model mix, token direction,
  and provider errors.

If a burn-rate alert fires, open `Attune Tenant Impact` first. It centers the
service-owned SLOs, then ranks tenant and destination pressure so you can tell
whether the issue is a backend regression, a destination outage, or a noisy
tenant.

Regenerate dashboards with:

```bash
make observability-dashboards
```

Do not edit generated dashboard JSON by hand. The generator tests verify metric
coverage, datasource portability, generated-output drift, and Helm copy sync.

## Recording and alert rules

Prometheus rules live in `observability/rules/` and are copied into the Helm
chart under `deploy/helm/attune/rules/`.

Files:

- `attune-recording.yml` — precomputes operational SLI series such as ingest
  validation error ratio, inbound availability/freshness, inbound p95, enrichment
  p95, outbox lag, LLM provider error ratio, notification failures, combined
  AI queue depth, and SLO burn-rate ratios for ingest, enrichment, outbox, OIDC,
  API-key access, MCP, and GDPR.
- `attune-alerts.yml` — alert rules aligned with the dashboard lenses:
  validation errors, sustained rate limiting, inbound availability/latency/stale
  sources, enrichment latency, AI queue backlog, LLM provider errors, outbox lag,
  notification failures, authorization denials, API-key access burn-rate,
  suspicious missing audit writes, and MWMB burn-rate alerts for the
  service-owned SLOs.
- `runbooks.md` — alert response guides. Every first-party alert annotation
  includes `dashboard`, `dashboard_url`, `runbook_url`, and `action` so
  Alertmanager and the Prometheus UI can point operators to the right view and
  first diagnostic step.

The Docker Compose observability overlay loads these rules automatically through
`deploy/prometheus.yml`. For Kubernetes, enable the optional Prometheus Operator
resource:

```yaml
serviceMonitor:
  enabled: true
prometheusRule:
  enabled: true
```

Rule thresholds intentionally match the dashboard targets first. Tune labels,
severity routing, and `for:` durations in your production Alertmanager stack
after you know normal tenant traffic patterns.

Alert annotations are part of the observability contract. Keep them actionable:

- `summary` — one-line symptom.
- `description` — current value, affected label set, and dashboard panel to
  inspect.
- `dashboard` / `dashboard_url` — the Grafana entry point with scoped variables
  when available.
- `runbook_url` — the matching section in `observability/runbooks.md`.
- `action` — the first response step, written as an operator action rather than a
  generic explanation.

Validate the reference Prometheus config and rules with:

```bash
make observability-rules
```

## Load and data validation

Use the load E2E script when changing metrics or dashboards. It sends mixed
real traffic through `/v1/feedback/ingest`, waits for scrape windows, and checks
the exposed metrics, recording rules, alert rule groups, and, optionally,
Grafana's datasource proxy.

```bash
API_KEY=fbk_live_... \
BASE_URL=http://127.0.0.1:18090 \
PROM_URL=http://127.0.0.1:19090 \
GRAFANA_URL=http://127.0.0.1:13000 \
GRAFANA_USER=admin \
GRAFANA_PASSWORD=... \
REQUESTS=240 \
CONCURRENCY=24 \
RATE_LIMIT_WARMUP_REQUESTS=60 \
RATE_LIMIT_REFILL_SECONDS=40 \
make observability-load-e2e
```

Expected behavior under load:

- `attune_ingest_total{result="ok"}` increases for accepted requests.
- `attune_ingest_total{result="validate_err"}` increases for malformed payloads.
- `attune_ingest_rate_limit_total` increases when the configured limiter rejects
  bursts.
- `attune_triage_decisions_total` increases after the enrichment worker handles
  accepted rows.
- Overview range panels should become non-empty after at least two scrapes for
  the same label set. Counter families that first appear during a burst need a
  baseline scrape before `increase()` can show a non-zero range value; the load
  script primes ingest, validation, and rate-limit label sets before measured
  traffic for this reason.

## Add a dashboard

Drop `dashboards/<name>.json` here. Prefix the name with the service to avoid
clashing on a shared Grafana. Keep panels datasource-less so they stay portable.

## Add a scrape target

For external multi-instance setups, add entries to `targets.yaml` and point your
Prometheus/VictoriaMetrics `file_sd_configs` at it.
