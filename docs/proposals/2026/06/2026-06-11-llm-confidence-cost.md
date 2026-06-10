# LLM confidence and cost observability

| | |
|---|---|
| **Issue** | #24 |
| **Status** | Implemented |
| **Started** | 2026-06-11 00:10 CST |
| **Related** | #10 (metadata-driven Dimensions), #20 (LLM guard policies), #21 (semantic understanding layer), #22 (language-aware enrichment) |

## Problem

Operators need two production signals that are currently missing from attune's
LLM enrichment path:

- Confidence: downstream automation treats each classification as definitive,
  but some model outputs are uncertain and should be reviewed before they drive
  routing or support action.
- Cost: LLM calls are the largest variable expense for high-volume tenants, but
  attune only exposes feedback volume, not per-tenant token and dollar burn.

The latest enrichment stack already separates the fast `user_feedback` snapshot
from append-only semantic evidence in `semantic_extraction_runs`. The #24 work
should extend that shape instead of bolting cost and quality fields onto the
wrong layer.

## Goals

- Ask the classifier for an overall self-rated confidence in `[0.0, 1.0]`, with
  prompt text that defines `0.5` as "ambiguous enough for human review".
- Persist `user_feedback.classification_confidence FLOAT NULL` for fast console
  list rendering, filtering, and low-confidence visual treatment.
- Record confidence evidence in `semantic_extraction_runs.confidence` so future
  eval/recompute workflows can compare model versions and prompt versions.
- Record one `llm_audit` row for every LLM `Complete` call with tenant, model,
  purpose, token counts, cost, status, latency, and error summary.
- Calculate cost from a vendored LiteLLM model-price catalog in
  `internal/infra/llmclient/model_prices_and_context_window.json`; unknown
  models remain observable with zero cost rather than failing the user
  workflow.
- Expose `GET /fb/v1/console/llm-usage` with week/month/day grouping over
  `llm_audit`, and add a Grafana dashboard JSON for tenant/model cost trends.
- Keep provider backends small: pricing and audit should be implemented as a
  wrapper around the provider-agnostic `llmclient.LLMClient` interface.

## Non-goals

- Do not introduce budgets, alerts, or tenant-specific pricing overrides in
  this PR.
- Do not treat model self-confidence as a calibrated probability. It is a
  review signal and dashboard dimension, not a correctness guarantee.
- Do not make low-confidence rows automatically urgent or block downstream
  dispatch. Routing remains controlled by Dimensions and notify-target policy.
- Do not replace the existing `/fb/v1/console/usage` ingest-volume endpoint.

## Proposal

Add `classification_confidence` to `domain.Enriched`, the classifier prompt,
the structured-output schema, and `parseEnrichJSON`. The parser accepts numeric
and numeric-string values, clamps valid out-of-range model output into the
closed interval `[0, 1]`, and leaves missing or unparseable values as nil. That
keeps old custom prompts compatible while making default prompts produce the
field.

Persist the value in `user_feedback.classification_confidence` through both
`MarkDone` and `MarkDoneTx`. Console list/detail responses expose the optional
number, and the feedback list renders a compact green/yellow/red confidence
indicator. The default threshold for "review" is `0.5`; the component accepts a
threshold prop so a future tenant setting can feed it without another rewrite.

Also write the value into `semantic_extraction_runs.confidence` as:

```json
{"overall": 0.72, "source": "llm_self_report"}
```

Cost is recorded at the LLM client boundary. Add
`internal/service/llmaudit.Client`, which implements `llmclient.LLMClient`,
delegates to the next client, measures latency, calculates cost with
`llmclient.PriceUsage`, and best-effort inserts an `llm_audit` row. It records
success and error calls. Audit insert failures are logged but do not replace the
LLM result, because observability should not turn a successful model call into
a failed user workflow.

`llm_audit` stores raw facts, not pre-aggregates:

- tenant_id
- model_id
- purpose
- prompt_tokens
- completion_tokens
- cost_usd
- status
- error
- latency_ms
- created_at

`GET /fb/v1/console/llm-usage` reads `llm_audit` and groups by tenant, model,
and a caller-selected granularity (`day`, `week`, `month`) over a
Grafana-style range expression (`now-7d`, `now-30d`, `now-90d`, `now/M`).
Console users see the authenticated tenant's cost. Admin/global cost
exploration can be added later with a scoped admin endpoint rather than
overloading this tenant route.

## Alternatives Considered

### Put audit writes in each provider backend

Rejected. OpenAI-compatible, OpenAI Responses, Anthropic, and Gemini already
normalize token usage into `llmclient.CompletionResponse`. A wrapper keeps audit
behavior consistent and avoids four drift-prone SQL call sites.

### Store only semantic run confidence

Rejected. `semantic_extraction_runs` is the right evidence layer, but the
console list needs fast access without latest-run joins. The business snapshot
should carry the current overall confidence just like it carries current attrs
and urgent status.

### Fail classification when audit insert fails

Rejected. Audit integrity matters, but the DB transaction that persists the
final enrichment still protects the user-visible state. If the separate audit
insert fails, logs and metrics should reveal it without discarding a successful
LLM response.

### Use live provider pricing APIs

Rejected for the first implementation. The vendored LiteLLM catalog keeps
startup simple, works offline, preserves a reviewable source of truth, and
matches the repository's "updateable via PR" acceptance criteria. The lookup is
shaped so NewAPI or tenant/model overrides can be layered on later.

## Risks / Tradeoffs

- Self-rated confidence can be poorly calibrated. The UI must describe it as a
  review signal and tests should cover missing values so operators do not infer
  false precision.
- Unknown models will show token usage with `$0.00` cost. That is preferable to
  dropping the call, but operators using private gateways may need a follow-up
  pricing override.
- Audit is best-effort after the LLM response. A process crash between provider
  response and audit insert can lose one audit row. Synchronous in-transaction
  audit is not possible because LLM calls are not part of the feedback DB
  transaction; the wrapper is still the narrowest reliable boundary.

## Implementation Plan

1. Add migrations for `user_feedback.classification_confidence` and
   `llm_audit`.
2. Extend domain, repo persistence, prompt, schema, parser, semantic run
   evidence, and tests for confidence.
3. Add LiteLLM-backed `llmclient` pricing and a `service/llmaudit` wrapper
   wired in server startup.
4. Add llm-audit repository aggregation plus `GET /fb/v1/console/llm-usage`
   proto, handler, router, generated Go/TS/OpenAPI.
5. Add console confidence indicator and an LLM usage dashboard surface.
6. Add Grafana JSON and changelog entries.

## Verification

- Go unit tests:
  - prompt contains `classification_confidence` and the 0.5 review definition
  - parse accepts `0.0`, `1.0`, missing, string numbers, and clamps out of range
  - pricing math matches known model/token references
  - audit wrapper inserts one row on success and one on provider error
  - llm usage query sums match per-row sums
- Console tests:
  - low-confidence indicator renders green/yellow/red boundaries
  - llm usage API client builds expected query params
- Integration tests:
  - migration smoke includes both new schema objects
- Gates:
  - `make proto`
  - focused `go test` packages touched by the backend
  - focused `pnpm vitest` tests for console components/API

## References

- Langfuse, LangSmith, Phoenix, Datadog, MLflow, Weave, Helicone, New Relic,
  OpenTelemetry GenAI, and LiteLLM all converge on call-level token/cost audit
  plus score/evaluation records rather than mixing every observability concern
  into the business entity.
- LiteLLM model prices:
  <https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json>.
