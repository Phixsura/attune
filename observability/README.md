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

The registered set is drift-guarded by `internal/infra/metrics/metrics_test.go` —
it must match this table.

## Add a dashboard

Drop `dashboards/<name>.json` here. Prefix the name with the service to avoid
clashing on a shared Grafana. Keep panels datasource-less so they stay portable.

## Add a scrape target

For external multi-instance setups, add entries to `targets.yaml` and point your
Prometheus/VictoriaMetrics `file_sd_configs` at it.
