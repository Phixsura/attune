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
| `attune_notify_failures_total` | counter | `destination_type`, `reason` | notifier push failures |
| `attune_outbox_lag_seconds` | gauge | — | age of the oldest pending outbox row (0 = empty) |
| `attune_claim_contention_total` | counter | — | enricher `tryClaim` lost to another worker |
| `attune_ingest_rate_limit_total` | counter | `tenant` | ingest requests rejected (429) by the rate limiter |
| `attune_triage_decisions_total` | counter | `tenant`, `decision` | triage-stage routing decisions |
| `attune_guard_actions_total` | counter | `tenant`, `stage`, `guard`, `entity`, `action` | guard actions applied at AI and outbound boundaries; raw findings are never labels |
| `attune_guard_blocked_total` | counter | `tenant`, `stage`, `guard`, `reason` | operations blocked by guard policies before external calls |
| `attune_llm_calls_total` | counter | `tenant`, `model`, `status` | LLM provider calls that reached a backend |
| `attune_llm_tokens_total` | counter | `tenant`, `model`, `direction` | provider-reported prompt/completion token usage |
| `attune_llm_cost_usd_total` | counter | `tenant`, `model` | estimated USD cost from the static model-price table |
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
| `attune_embedding_cache_hits_total` | counter | `tenant`, `result` | embedding cache hits vs misses (#30) |
| `attune_oidc_login_total` | counter | `result` | OIDC login attempts by result (#40) |
| `attune_oidc_login_duration_seconds` | histogram | — | end-to-end OIDC login latency (#40) |
| `attune_oidc_token_exchange_duration_seconds` | histogram | — | OIDC token exchange latency (#40) |
| `attune_oidc_role_mapping_total` | counter | `role` | OIDC role mappings by assigned role (#40) |
| `attune_authz_denied_total` | counter | `role`, `required` | Authorization denials by user role and required role (#38) |
| `attune_audit_rows_written_total` | counter | `action` | immutable audit-log rows written by action (#39) |
| `attune_audit_rows_pruned_total` | counter | — | immutable audit-log rows pruned by retention policy (#39) |
| `attune_audit_prune_duration_seconds` | histogram | — | audit-log retention prune latency (#39) |

Label values:

- `source` — one of `api`, `lark-group`, `lark-bitable`, `lark-approval`,
  `lark-helpdesk`, `lark-form`, `email`, `web`, `other`; or `invalid` when a
  request's source failed validation.
- ingest `result` — `ok` · `validate_err` · `auth_err` · `internal_err`.
- enrich `dims_mode` — `freeform` · `constrained` (set per the tenant's `DimensionSet`: `constrained` when at least one dim has a non-empty taxonomy).
- enrich `dim` — the stable `Dimension.Name` (e.g. `type`, `severity`, `labels`).
- enrich `result` — `ok` · `llm_err` · `parse_err` · `other_err` · `db_err`.
- `destination_type` — `lark-pool` · `lark-radar` · `raw-webhook`.
- `reason` — `transport` · `terminal`.
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
- embedding cache `result` — `hit` · `miss`.

The registered set is drift-guarded by `internal/infra/metrics/metrics_test.go` —
it must match this table.

## Add a dashboard

Drop `dashboards/<name>.json` here. Prefix the name with the service to avoid
clashing on a shared Grafana. Keep panels datasource-less so they stay portable.

## Add a scrape target

For external multi-instance setups, add entries to `targets.yaml` and point your
Prometheus/VictoriaMetrics `file_sd_configs` at it.
