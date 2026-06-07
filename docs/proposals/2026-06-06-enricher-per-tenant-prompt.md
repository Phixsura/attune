# Proposal — tenant-configurable enrich prompt + module whitelist (with multi-protocol LLM client)

| | |
|---|---|
| **Issue** | #10 |
| **Status** | Implemented |
| **Started** | 2026-06-06 22:53 CST |
| **Related** | #7 (deploy docs: "how to configure modules"), CLAUDE.md §5 (layering), §7 (observability), §8 (deps), §11 (proto IDL) |

## Problem

The enricher uses ONE hardcoded zh-CN prompt for every tenant
([`enricher.go:26`](../../internal/service/enrich/enricher.go), rendered via
`fmt.Sprintf(enrichPromptTmpl, content)` at
[`:177`](../../internal/service/enrich/enricher.go)) and lets the LLM invent
`modules` freely ([`:31`](../../internal/service/enrich/enricher.go)).

`modules` is not a decorative field — it is the **grouping axis** consumed by:

- the **weekly digest "top modules"** line, which counts module strings by
  exact match (`TopModulesByTenant` →
  [`feedback_console_stats.go:96`](../../internal/repo/feedback/feedback_console_stats.go),
  called from [`digest_weekly.go:125`](../../internal/service/outbox/digest_weekly.go));
- the console feedback detail "相关模块" chips
  ([`detail-sheet.tsx:68`](../../console/src/features/feedback/components/detail-sheet.tsx));
- every notification card — Lark
  ([`lark_card.go:70`](../../internal/notify/adapter/larkwebhook/lark_card.go)),
  GitHub issue body ([`github_issue.go:207`](../../internal/notify/adapter/githubissue/github_issue.go)),
  and the customer-facing raw-webhook **outbox envelope**
  ([`enricher_outbox.go:152`](../../internal/service/enrich/enricher_outbox.go));
- the eval CLI's module-quality metric `ModuleSumIoU` (Jaccard,
  [`eval.go:197`](../../internal/service/eval/eval.go)).

Because the LLM free-forms labels, the same concept yields unstable strings
("购物车" / "购物车模块" / "cart"), so the exact-match aggregation (digest top
modules, per-module filtering) is unreliable, and the single zh-CN prompt
produces generic or wrong modules for non-Chinese / different-domain products.
For most OSS tenants the field is, in the issue's words, "meaningless".

**Value:** make `modules` a stable, queryable dimension per tenant — turning an
*already-shipped but unreliable* feature (digest top-modules, per-module views)
into a trustworthy one — and let any product (not just the origin one) tailor
classification to its own module vocabulary and language.

## Goals

- Per-tenant **module whitelist**: when a tenant declares its module list, the
  enricher's `modules` output is **guaranteed** to be a subset of that list
  (canonical spelling), independent of which LLM provider/gateway is used.
- Per-tenant **prompt template** override (optional, advanced), rendered
  **safely** (no template injection).
- **Backward compatible**: both unset → byte-for-byte today's behavior
  (default prompt, free-form modules). Zero regression for existing tenants.
- **Recall safety**: feedback about an undeclared module is never silently
  mis-tagged; it is surfaced as a "suggested module" signal.
- **Multi-protocol LLM client** (bundled per issue decision): the LLM layer
  supports **OpenAI Chat Completions, OpenAI Responses, Gemini, and Anthropic
  Messages**, each with structured-output support used to enforce the whitelist
  at the source.
- **Console UI** to view/edit/preview the per-tenant config.
- Tests for every path; CHANGELOG `### Added`.

## Non-goals

The first three are deliberate scope cuts that will each get their own
follow-up issue **after #10 is resolved**:

- Per-module **routing** (e.g. checkout → payments webhook). Follow-up issue.
- Per-tenant customization of `kind` / `severity` taxonomies — this issue is
  prompt + modules only. Follow-up issue.
- A full human-review **UI** for suggested modules — v1 emits the signal
  (metric + log) only; a review queue is a follow-up issue.

The last is a hard scope line:

- **Only the four named LLM protocols are supported** (OpenAI Chat / OpenAI
  Responses / Gemini / Anthropic Messages). Other OpenAI-compatible gateways
  are not our concern — users who want them can implement an additional
  backend behind `LLMClient`. We do **not** ship generic
  auto-detection/fallback for unknown endpoints; the always-on post-filter
  (gate ②) keeps stored output clean regardless of provider behavior.

## Proposal

### 1. Schema — migration `012_enricher_per_tenant_prompt.sql`

```sql
ALTER TABLE tenants
  ADD COLUMN IF NOT EXISTS enrich_prompt_template TEXT,    -- NULL  = default template
  ADD COLUMN IF NOT EXISTS enrich_modules         TEXT[];  -- NULL/empty = free-form
```
Next sequential number (current max is `011`); follows the
`ADD COLUMN IF NOT EXISTS` pattern of `008_tenant_locale.sql`.

### 2. Module constraint — three gates (the whitelist is enforced by gate ②)

1. **Gate ① — prompt guidance.** The rendered prompt names the allowed list
   ("modules MUST be chosen only from: …").
2. **Gate ③ — structured output (source-level).** The request carries a schema
   pinning `modules.items.enum` to the tenant list (and `kind`/`severity` to
   fixed enums), so a supporting provider cannot emit an off-list value. Built
   per request from the tenant's list. Implemented across all four protocols by
   the new LLM client (§5).
3. **Gate ② — post-parse filter (the guarantee, always on).** After parsing,
   normalize (`trim`+`lower`), keep only configured modules (emitting the
   **canonical configured spelling**), dedupe. This is provider-independent, so
   correctness never depends on whether a gateway honored gate ③.

**Recall escape.** `modules` may be empty. When constrained and the model emits
labels not on the list, gate ② drops them from the stored tags **but** records
them as a **suggested-module** signal (metric + logged phrase), so the tenant
can later promote a new module. This matches the industry pattern (closed-set
classify + offline, human-gated discovery) and turns the "module drift" cost
into an observable signal rather than silent data loss.

**Mode.** `module_mode ∈ {freeform, constrained}` derived from whether the list
is non-empty; surfaced as a metric label so operators can see how many tenants
remain ungoverned and nudge them to configure.

### 3. Prompt rendering — fixed-token substitution (SSTI-safe)

Render with stdlib `strings.Replacer` over a **closed token set** — `{{content}}`
and `{{modules}}` — **not** `fmt.Sprintf` (breaks on literal `%` in user text)
and **not** `text/template`/`html/template` (their documented model is "template
authors are trusted"; a tenant admin is not — SSTI / method-call / resource
risk). This is the in-process equivalent of the logic-less `{{var}}` model used
by prompt platforms (Langfuse/Helicone) and adds **no dependency** (§8).

- Replacement is single-pass: a `{{content}}` *inside* the feedback data cannot
  trigger further substitution.
- The default template constant is rewritten to use `{{content}}` (replacing
  `%s`) and a `{{modules}}` slot; the renderer fills `{{modules}}` with the
  allow-list clause when constrained, or the free-form clause when not.
- **Validation on save:** the custom template must contain `{{content}}`;
  enforce a length cap; the JSON-output scaffolding stays server-side and
  non-overridable (tenants edit wording, not the output contract).

### 4. Config read path — JOIN, keep `Classify` pure

`EnrichInput` gains `PromptTemplate *string` + `Modules []string`;
`LoadForEnrich` ([`feedback.go:95`](../../internal/repo/feedback/feedback.go))
adds `LEFT JOIN tenants` so the per-row config arrives in the **same query** the
enricher already runs — the enricher needs **no new dependency** on
`tenant.TenantRepo`.

`Classify` stays **pure and side-effect-free** (consumed by the eval CLI without
DB access, [`eval.go:90`](../../internal/service/eval/eval.go)): its signature
becomes `Classify(ctx, content string, cfg EnrichConfig)` — config is *passed
in*, never fetched inside. The eval CLI fetches each sampled row's tenant config
(it already has `TenantID`) so its `ModuleSumIoU` metric reflects real
production behavior.

### 5. Multi-protocol LLM client + structured output

Today `LLMClient` is a single OpenAI-Chat backend:
`Chat(ctx, userID, model, prompt string, temperature, maxTokens) (string,error)`
([`client.go:22`](../../internal/infra/llmclient/client.go)) with a request body
carrying only `temperature`+`max_tokens`
([`openai_backend.go:70`](../../internal/infra/llmclient/openai_backend.go)) —
no way to request structured output.

#### Approach (decision: X — thin in-process shim over official SDKs)

Considered three paths to support the four protocols + structured output (see
"Alternatives considered"). Choosing the **thin-shim** path:

- Reuse the existing **hand-rolled HTTP backend** for OpenAI Chat Completions
  (covers OpenAI / Azure / vLLM / ollama / oneapi / any OpenAI-compatible
  endpoint with one transport).
- Take direct deps on the **three official SDKs** for the other three protocols
  (one per protocol — no multi-provider framework). Each backend is ~50–150 LOC
  and maps `OutputSchema` to that provider's native structured-output mechanism.
- Total new code in attune: ~300 LOC across four backends + interface.

This rejects vendoring `cloudwego/eino-ext` (would copy ~5–6k LOC of stale-as-
of-vendor-time code; eino's OpenAI Chat path currently routes through a
third-party `meguminnnnnnnnn/go-openai` fork the upstream itself says they
plan to replace) and rejects adding a sidecar gateway (LiteLLM / Portkey /
Helicone — would add a Python/Node/Rust process to a project whose value
proposition is "single distroless binary + Postgres"; see §8 deps policy and
`deploy/docker-compose.yml`).

#### Interface

Introduce a provider-agnostic request with an optional output schema:

```go
type LLMClient interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

type CompletionRequest struct {
    UserID       string
    Model        string
    System       string
    Prompt       string
    Temperature  float64
    MaxTokens    int32
    OutputSchema *OutputSchema // when set → request structured output
}

type OutputSchema struct {
    Name   string
    Schema map[string]any // provider-agnostic JSON Schema (object + enums)
}
```

#### Backends

Four backends, selected by `llm_protocol` config:

| Backend | File | How it calls the provider | Structured-output mechanism |
|---|---|---|---|
| OpenAI-Compatible Chat | `openai_compat_backend.go` (extend existing) | hand-rolled `net/http` (today's code) — keeps any OpenAI-compatible endpoint (vLLM/ollama/oneapi) reachable through one transport | `response_format:{type:"json_schema",strict:true,…}` |
| OpenAI Responses | `openai_responses_backend.go` | `github.com/openai/openai-go/v3` → `client.Responses.New` | `text.format:{type:"json_schema",…}` via `responses.ResponseFormatTextConfigParamOfJSONSchema` |
| Gemini | `gemini_backend.go` | `google.golang.org/genai` → `client.Models.GenerateContent` | `GenerateContentConfig.ResponseMIMEType="application/json"` + `ResponseSchema` |
| Anthropic Messages | `anthropic_backend.go` | `github.com/anthropics/anthropic-sdk-go` → `client.Messages.New` | forced **tool use**: one tool, `input_schema`=Schema, `ToolChoice = ToolChoiceParamOfTool(name)` |

All four accept a user-supplied `*http.Client`, so each is constructed with
`&http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}` per
§7 — outbound spans uniform across providers.

Callers (`enricher`, `eval`) migrate from `Chat` to `Complete`. Because gate ②
(post-filter) is always on, a provider that ignores the schema still yields
clean stored output — no elaborate per-gateway feature-detection needed.

#### Dependency cost (per §8 disclosure)

| Dep | License | Why | Note |
|---|---|---|---|
| `github.com/openai/openai-go/v3` | Apache-2.0 | OpenAI Responses (Chat path stays on our hand-rolled client) | Pulls Azure SDK transitively (used for Azure-OpenAI auth that we don't call) |
| `github.com/anthropics/anthropic-sdk-go` | MIT | Anthropic Messages | Heaviest: pulls aws-sdk-go-v2 (Bedrock) + `cloud.google.com/go/auth` (Vertex) as indirect deps even though we use neither |
| `google.golang.org/genai` | Apache-2.0 | Gemini | Leanest of the three; one direct dep on `cloud.google.com/go/auth` |

These are all *official* / vendor-maintained SDKs; the Anthropic indirect-dep
weight is the one wart, documented here so a future maintainer doesn't have to
re-discover why three cloud SDKs sit in `go.sum`. Net: trade 3 widely-used
SDKs for ~300 LOC of attune-owned mapping code that we'd otherwise have to
hand-roll over `net/http` for the same four providers.

### 6. Console (proto-contracted, one endpoint per PR — §11)

New `proto/attune/v1/enrich_config.proto` →
`EnrichConfigService`:

- `GetEnrichConfig` — returns template (or default), modules, module_mode.
- `UpdateEnrichConfig` — validates + saves; restore-to-default = clear template.
- `PreviewEnrichPrompt(sample_content)` — renders **server-side via the same
  `renderPrompt`** so preview == what runs (single source of truth).

`make proto` regenerates Go/TS/OpenAPI (never hand-edited). Frontend adds a
`features/settings` route: prompt textarea + default/restore, module tag-input,
and a preview pane.

### 7. Observability (§7)

- `attune_enrich_modules_dropped_total{tenant}` — modules removed by gate ②.
- a suggested-module counter / structured log line carrying the phrase.
- `module_mode` label on the existing enrich metrics.

## Alternatives considered

- **Modules: prompt-only (no post-filter).** Rejected — LLM next-token
  randomness means a prompt alone cannot *guarantee* the subset property
  (acceptance ①). Best practice is prompt **+** post-filter; we add structured
  output on top where the provider supports it.
  ([W&B](https://wandb.ai/gladiator/LLMs-as-classifiers/reports/LLMs-are-machine-learning-classifiers--VmlldzoxMTEwNzU1MjQ),
  [Tryolabs](https://tryolabs.com/blog/strategies-and-tools-for-controlling-responses))
- **Custom prompt via `text/template` (my first instinct).** Rejected on
  security grounds: Go's `text/template` assumes trusted authors (SSTI →
  file-read/RCE via methods on the data); jinja2 sandboxes have been escaped
  repeatedly (CVE-2024-56326, CVE-2025-27516); even mustache had an
  attribute-access advisory in LangChain. Fixed-token substitution has no
  expression-evaluation surface.
  ([text/template](https://pkg.go.dev/text/template),
  [LangChain GHSA-6qv9-48xg-fc7f](https://github.com/langchain-ai/langchain/security/advisories/GHSA-6qv9-48xg-fc7f))
- **Adding a mustache/jinja dependency.** Rejected — two scalar slots need no
  logic; mustache's HTML-escaping is a hazard for prompts, and it fails the §8
  "no new dep without justification" bar.
- **Closed-set with an `"other"` catch-all label.** Rejected as the *primary*
  fallback — zero-shot LLMs collapse on catch-all/"unknown" classes (reported
  F1 ≈ 0.03–0.09). Instead: allow empty + a structured suggested-module signal.
  ([open-set recognition](https://arxiv.org/pdf/2403.05700))
- **Enricher gains a `TenantRepo` dependency.** Rejected — `LoadForEnrich`
  already loads the row; a `LEFT JOIN` returns config with no new edge.
- **Separate issue for the multi-protocol LLM client.** Considered (it is reusable
  infra); per issue decision it is **bundled** into #10, sequenced as its own PRs.
- **Vendor a slice of `cloudwego/eino` + `eino-ext`.** Apache-2.0, technically
  covers all four protocols including Anthropic forced tool-use, and the per-
  provider `go.mod` layout *would* let us carve out just chat. **Rejected**:
  (i) the chat-only vendor slice is ~5–6 kLOC versus ~300 LOC of attune-owned
  shim; (ii) eino's OpenAI Chat path goes through a third-party fork
  `meguminnnnnnnnn/go-openai` that upstream itself says they plan to replace,
  so a 1:1 copy bakes in transitional code; (iii) eino files were touched
  four times in the two weeks before this proposal — vendoring a moving
  target means we either fall behind on upstream fixes or follow them
  manually. The thin shim is smaller *and* gives us direct control over the
  four mappings.
  ([cloudwego/eino](https://github.com/cloudwego/eino),
  [cloudwego/eino-ext](https://github.com/cloudwego/eino-ext))
- **Standalone gateway sidecar** (LiteLLM Proxy / Portkey / Helicone / new-api).
  Rejected — each adds a Python/Node/Rust process to a project whose deploy
  story is "single distroless binary + Postgres". LiteLLM Proxy is the
  strongest at structured-output translation but ships a ~371 MB Python image
  and (optionally) a second Postgres; Helicone's gateway currently does not
  translate `response_format` to Anthropic
  ([Helicone#5639](https://github.com/Helicone/helicone/issues/5639));
  `new-api` is AGPL-3.0, which clashes with attune's Apache-2.0 distribution
  posture. Users who *want* a gateway can still place one in front of
  attune's existing OpenAI-Compatible backend — we do not internalize that
  operational cost.
- **`tmc/langchaingo`.** Rejected — covers OpenAI Chat structured output but
  is missing **three of the four** we need: no `/v1/responses`, no Gemini
  `responseSchema` (uses legacy `generative-ai-go`), and the internal
  Anthropic client has no `tool_choice` field at all. Last `main` push 5
  months stale at proposal time.
  ([tmc/langchaingo](https://github.com/tmc/langchaingo))
- **Industry alignment.** Closed-set classify + offline human-gated discovery is
  what Linear, Sentry, Zendesk, Intercom, Cresta, Dovetail, Productboard, and
  Enterpret converged on.
  ([Zendesk intent suggestions](https://support.zendesk.com/hc/en-us/articles/9484697389210),
  [Enterpret Adaptive Taxonomy](https://www.enterpret.com/platform/adaptive-taxonomy))

## Risks / tradeoffs

- **Customer-facing envelope behavior change.** The outbox envelope is a
  customer contract (canonical JSON). Whitelisting changes `modules` *values*
  (not shape) for existing raw-webhook consumers → call out in CHANGELOG;
  empty-list (default) keeps today's values, so existing tenants are unaffected
  until they opt in.
- **Multi-protocol client is a large surface.** Four providers, four output
  mechanisms (Anthropic via tool-use differs most). Mitigation: one provider-
  agnostic schema, per-backend mapping, per-backend tests, sequenced PRs, and
  gate ② as a universal backstop.
- **Module drift.** A tenant's list goes stale. Mitigation: the suggested-module
  signal (DN2) makes gaps observable.
- **No new deps target.** Gemini/Anthropic are called over raw HTTP, not SDKs.

## Implementation plan (PRs within #10)

1. **PR1 — migration** `012` (standalone).
2. **PR2 — enricher core**: repo (`GetEnrichConfig`/`UpdateEnrichConfig`,
   `LoadForEnrich` JOIN) + service (`renderPrompt` via `strings.Replacer`,
   `filterModules`, `module_mode`, suggested signal) + default-template rewrite
   + eval wiring + tests + CHANGELOG. Uses the existing OpenAI client with gates
   ①+②. **Delivers core value + acceptance ①②④.**
3. **PR3 — multi-protocol LLM client**: `Complete` + `OutputSchema`, four
   backends, `llm_protocol` config, migrate `enricher`/`eval`; per-backend tests.
4. **PR4 — wire gate ③**: build the per-tenant schema, pass `OutputSchema` from
   the enricher; structured output across all four protocols.
5. **PR5 — proto `GetEnrichConfig`** + handler + frontend read view.
6. **PR6 — proto `UpdateEnrichConfig`** + frontend edit / restore-default.
7. **PR7 — proto `PreviewEnrichPrompt`** + frontend preview. **Acceptance ③.**

## Verification

- Unit: `renderPrompt` (token fill / missing `{{content}}` / literal `%` /
  length cap), `filterModules` (case-insensitive match / canonical spelling /
  dedupe / empty-list passthrough / all-dropped → suggested signal),
  `LoadForEnrich` JOIN, repo Get/Update, each LLM backend's schema mapping.
- Acceptance ① tenant `[cart,shipping]` → output ⊆ set (table-driven; optional
  live-LLM smoke). ② unset → default render byte-identical (golden test).
  ③ preview endpoint output == enricher's rendered prompt (same fn). ④ both
  paths covered. ⑤ CHANGELOG `### Added`.
- eval: compare `ModuleSumIoU` with whitelist on vs off to quantify the gain.
- Gates: `go vet`, `go build`, `go test -short`, lizard CCN/NLOC, jscpd,
  `scripts/lint-slog.sh`, `buf generate` (proto-sync).

## References

- Issue #10; related #7.
- Architecture data-flow diagram: `docs/proposals/assets/2026-06-06-enricher-flow.svg`.
- Structured outputs:
  [OpenAI](https://developers.openai.com/api/docs/guides/structured-outputs),
  [Azure](https://learn.microsoft.com/en-us/azure/foundry/openai/how-to/structured-outputs),
  [vLLM](https://docs.vllm.ai/en/latest/features/structured_outputs.html),
  [Gemini structured output](https://ai.google.dev/gemini-api/docs/structured-output),
  [Anthropic tool use](https://docs.anthropic.com/en/docs/build-with-claude/tool-use).
- Safe templating:
  [Go text/template](https://pkg.go.dev/text/template),
  [Langfuse variables](https://langfuse.com/docs/prompt-management/features/variables),
  [LangChain template-injection advisory](https://github.com/langchain-ai/langchain/security/advisories/GHSA-6qv9-48xg-fc7f).
- Taxonomy product patterns:
  [Linear Triage Intelligence](https://linear.app/now/how-we-built-triage-intelligence),
  [Zendesk intent suggestions](https://support.zendesk.com/hc/en-us/articles/9484697389210),
  [Enterpret Adaptive Taxonomy](https://www.enterpret.com/platform/adaptive-taxonomy).
- CLAUDE.md §5 / §7 / §8 / §11.
