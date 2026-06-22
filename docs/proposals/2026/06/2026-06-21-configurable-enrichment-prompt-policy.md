# Configurable enrichment prompt policy

| Field | Value |
| --- | --- |
| **Issue** | [#107](https://github.com/Phixsura/attune/issues/107) |
| **Status** | Implemented |
| **Started** | 2026-06-21 12:09 CST |
| **Related** | [#22](https://github.com/Phixsura/attune/issues/22), [#106](https://github.com/Phixsura/attune/pull/106), [2026-06-10-language-aware-enrichment.md](./2026-06-10-language-aware-enrichment.md), [2026-06-06-enricher-per-tenant-prompt.md](../../2026-06-06-enricher-per-tenant-prompt.md) |

## Problem

Attune's enrichment prompt behavior has grown from a single built-in prompt into
a language-aware prompt path with tenant custom prompt support. The current
implementation works, but the policy is still encoded as service behavior in
`internal/service/enrich/enricher_prompt.go`:

- built-in English and Chinese prompt bodies are embedded as unversioned string
  constants
- the display-locale rule is coupled to built-in template selection
- supported template variables are implicit in the render function
- `semantic_extraction_runs.prompt_version` records coarse labels such as
  `default:en`, `default:zh`, and `tenant_custom`
- tenant custom prompts are raw text, so the service cannot describe which
  policy knobs the tenant changed
- Console can show prompt text, but not the resolved language/output policy that
  operators need to reason about enrichment output

This makes the prompt path harder to audit as enrichment becomes a shared
foundation for triage, reply drafts, digests, evaluation, and semantic search.
Operators should be able to inspect how Attune will summarize and classify
feedback without reading Go code or reverse-engineering template variables.

## Goals

- Introduce a typed enrichment prompt policy model that separates policy from
  rendered prompt text.
- Keep the existing built-in prompts as versioned defaults.
- Preserve compatibility for tenants that already set
  `tenants.enrich_prompt_template`.
- Make template variables, required variables, required output fields, locale
  behavior, and summary-length guidance explicit and testable.
- Record enough resolved policy identity in `semantic_extraction_runs` to audit
  historical enrichment runs after defaults change.
- Persist immutable operator-saved prompt-policy snapshots so config changes
  can be reviewed and rolled back without reconstructing them from run metadata.
- Keep the implementation dependency-free and scoped to Attune's enrichment
  use case rather than building a general prompt-management product.
- Surface the active policy in Console so operators can understand output
  language and legacy custom prompt behavior.

## Non-goals

- Do not build a full prompt CMS with collaboration, experiments, environment
  promotion, arbitrary prompt families, or A/B rollout labels.
- Do not require a new external runtime dependency or hosted prompt registry.
- Do not automatically re-enrich historical rows when prompt policy changes.
- Do not remove raw custom prompt templates as an escape hatch.
- Do not make per-user prompt policy selection part of this issue.
- Do not change LLM provider selection or provider-specific schema behavior.
- Do not change the reply-draft prompt path in `internal/service/replydraft`;
  this policy governs enrichment classification only.

## Proposal

Model enrichment prompting as a resolved policy with a versioned identity, a
small set of typed knobs, a template contract, and an optional raw-template
override.

The policy layer should answer four questions before a request reaches an LLM:

1. Which immutable policy version is active for this tenant?
2. Which variables are valid and which ones are required?
3. Which output fields must be produced and in which language?
4. Which rendered prompt and structured schema were sent for this run?

### Policy model

Add an internal enrichment prompt policy type near `internal/service/enrich`.
The first shape can stay in service code while the public behavior stabilizes:

```go
type PromptPolicy struct {
	ID              string
	Version         int
	TemplateFormat  string
	Template        string
	Variables       []PromptVariable
	RequiredOutputs []PromptOutputField
	LocalePolicy    LocalePolicy
	SummaryGuidance  SummaryGuidance
}

type PromptVariable struct {
	Name     string
	Required bool
	Meaning  string
}

type PromptOutputField struct {
	Name     string
	Required bool
	Language OutputLanguage
}
```

The built-in policy should be identified as `enrich.default@1`. The version
increments only when the default contract changes in a way that can change model
behavior, such as prompt wording, required variables, required output fields, or
locale policy. Cosmetic Go refactors do not create a new policy version.

Use one canonical identity format everywhere:

| Field | Built-in example | Legacy custom example |
| --- | --- | --- |
| `policy_id` | `enrich.default` | `enrich.legacy_custom_template` |
| `policy_version` | `1` | `sha256:abcd1234...` |
| `prompt_version` | `enrich.default@1` | `enrich.legacy_custom_template@sha256:abcd1234...` |
| `mode` | `default` | `legacy_custom_override` |

`semantic_extraction_runs.prompt_version` stores the combined
`<policy_id>@<policy_version>` value. Existing run rows that contain
`default:en`, `default:zh`, or `tenant_custom` remain valid historical values;
readers, eval tools, and audit views must tolerate both formats. Do not backfill
old rows because the precise resolved policy contract cannot always be derived
from historical data.

The initial built-in variables are:

| Variable | Required | Meaning |
| --- | --- | --- |
| `content` | yes | Raw feedback text to classify. |
| `dimensions` | yes | Rendered Dimension contract and taxonomy hints. |
| `display_locale` | yes | Tenant display locale used for operator-facing fields. |
| `display_language` | yes | Normalized display language code. |
| `display_language_name` | yes | Human-readable display language name for the model. |

The initial required output fields are:

| Field | Language policy |
| --- | --- |
| `title` | Source feedback language when clear. |
| `rationale` | Source feedback language when clear. |
| `display_title` | Tenant display language. |
| `display_rationale` | Tenant display language. |
| `classification_confidence` | Numeric confidence. |
| Dimension fields | Per-tenant Dimension contract. |

### Built-in defaults

Keep the current English and Chinese prompt bodies, but register them through
the policy layer instead of selecting raw constants directly. The default policy
can still choose an English or Chinese template body based on display language,
but the identity should remain `enrich.default@1` with a rendered-template
language recorded separately.

This avoids treating every localization of the same contract as a separate
policy. If the English and Chinese templates later diverge semantically, they
should become separate policy versions or separate policy IDs.

### Tenant overrides and legacy compatibility

Existing tenants may have `tenants.enrich_prompt_template` populated. Preserve
that behavior by resolving it as a legacy custom override:

```text
policy_id: enrich.legacy_custom_template
policy_version: sha256(canonical_template + variable_contract_version + output_contract_version)
base_policy: enrich.default@1
mode: legacy_custom_override
```

The raw template remains the source of prompt text, but the service still
applies the known variable replacer and structured output schema. Save-time
validation should expand from "must contain `{{content}}`" to a contract-aware
validator:

- require `{{content}}`
- reject templates above the existing length cap
- preserve existing legacy templates by treating unknown variables as
  compatibility warnings in `legacy_custom_override` mode
- warn when `{{dimensions}}` is missing because the structured schema and
  post-parse filter still run, but the prompt does not explain the tenant's
  Dimension taxonomy to the model
- expose missing output-field mentions as low-priority diagnostics only; schema
  enforcement is the stronger output contract

The first API-compatible behavior can keep `{{dimensions}}` optional for custom
prompts because existing tenants may intentionally own classification guidance.
Console should label that state as "custom prompt override" and show the
specific quality risk only when a warning applies. Avoid the word "freeform" in
Console prompt-policy copy because the Dimensions UI already uses freeform vs.
constrained to describe taxonomy enforcement.

If legacy tenants still have older variables such as `{{modules}}`, keep the
template loadable and editable at first, surface an explicit warning, and decide
whether to support an alias or migration after inspecting real tenant data. The
compatibility rule is: a template that was accepted before this proposal should
not start failing on a no-op save unless it is missing `{{content}}` or exceeds
the existing length cap.

### Resolved policy and provenance

Add a small resolver:

```go
type ResolvedPromptPolicy struct {
	PolicyID                 string
	PolicyVersion            string
	BasePolicyID             string
	BasePolicyVersion         string
	Mode                     string
	TemplateLanguage          string
	DisplayLocale             string
	DisplayLanguage           string
	RequiredVariables         []string
	RequiredOutputFields      []string
}
```

`ClassifyConfig` should carry the tenant input values. The resolver should
produce `ResolvedPromptPolicy`, then rendering should consume that resolved
object. This keeps locale choice, default selection, template validation, prompt
versioning, and observability in one place.

Record provenance in `semantic_extraction_runs` as:

- `prompt_version`: resolved identity such as `enrich.default@1` or
  `enrich.legacy_custom_template@sha256:abcd1234`
- `guard_summary.prompt_policy`: compact JSON with mode, template language,
  display locale, variable contract version, output contract version, contract
  fields, and warning codes

The resolver may compute non-persisted prompt or schema hashes internally for
tests and diagnostics, but persisted provenance should avoid stable fingerprints
of tenant-authored low-entropy prompts or taxonomies. This keeps run metadata
useful for auditing without creating a durable identifier that could be compared
against guessed prompt text.

If a future API needs operator-visible fingerprints, derive them with a
tenant-scoped secret or expose them only ephemerally. The hash input must never
include raw feedback content. A safe prompt diagnostic hash would use a canonical
prompt shape:

- selected template body with the content slot preserved or replaced by a fixed
  sentinel such as `{{content}}`
- canonical rendered Dimensions clause or its hash
- variable contract version
- output contract version
- template language

Compute any schema diagnostic hash only when a structured output schema is
actually sent to the LLM client.

### Proto and API shape

Extend the proto contract append-only. `EnrichConfig` should gain a
`prompt_policy` message, and preview responses should include the same resolved
metadata used for the rendered preview:

```proto
message EnrichPromptPolicy {
  string policy_id = 1;
  string policy_version = 2;
  string prompt_version = 3;
  string mode = 4;
  string prompt_source = 5;
  string template_language = 6;
  string display_locale = 7;
  string display_language_name = 8;
  repeated EnrichPromptVariable variables = 9;
  repeated EnrichPromptOutput outputs = 10;
  repeated EnrichPromptWarning warnings = 11;
}
```

The generated Go, TypeScript, and OpenAPI output must be committed via
`make proto`. The HTTP handlers should return warning metadata from the service
rather than asking Console to infer contract health from raw prompt text.

### Console surface

Console should expose the active enrichment prompt policy as inspectable
settings:

- active policy identity and mode
- display locale and display language behavior
- built-in default policy version
- whether the tenant uses a legacy custom template
- recognized variables and required output fields
- warnings for missing `{{dimensions}}` or other degraded template quality
- staged restore-default behavior that clears the tenant raw template override
  only after the operator saves

The first Console surface should keep the primary UI compact:

| State | Primary UI behavior |
| --- | --- |
| Saved default | Show `enrich.default@1`, display language, healthy status, and the locale-resolved default body. |
| Saved custom | Show custom prompt override, active warnings if any, and the policy hash. |
| Unsaved custom | Show staged unsaved state and validate before save. |
| Unsaved restore | Show that save will clear the custom override and return to the locale-resolved default. |
| Invalid custom | Block save for missing `{{content}}` or length violations. |
| Warning-only custom | Allow save, but show quality warnings such as missing `{{dimensions}}`. |

Place policy identity, prompt source, display-language behavior, and warning
status at the top of the existing prompt card. Put variables and required
outputs inside a collapsible "Prompt contract" detail. The preview card should
show the effective rendered prompt plus the resolved policy metadata so the
operator does not see an English default while the runtime uses a Chinese
template for a Chinese display locale.

Editing structured policy knobs can be added after the typed model is in place.
The first Console surface should make the current behavior legible and reduce
operator surprise without turning the settings page into a prompt registry.

### Storage strategy

Use a small hybrid storage model:

- code-defined immutable built-in policies
- existing `tenants.enrich_prompt_template` for legacy custom overrides
- existing `tenants.enrich_dimensions` for the active Dimension contract
- `tenants.active_enrich_prompt_version_id` as the active snapshot pointer
- `tenant_enrich_prompt_versions` for immutable operator-saved snapshots
- enriched config response fields for inspectable policy metadata
- `semantic_extraction_runs` for run-level provenance

The version table is intentionally not a full prompt CMS. It stores the concrete
prompt template, Dimension JSON, resolved policy metadata, prompt version, and
creation time for each saved operator change. The hot enrichment path continues
to read the denormalized tenant row, while Console can list recent snapshots and
reactivate a previous one.

### Eval and preview behavior

Evaluation must use the same resolver as production enrichment. Update the eval
tenant-config reader path so it carries tenant locale/display policy, not only
prompt template and Dimensions. Eval fakes should include locale-aware cases for
default English and Chinese prompt resolution.

`DefaultPromptTemplate()` can remain as a compatibility helper, but Console
should not rely on it as the only default body. `GET /enrich-config` and prompt
preview should return the locale-resolved default template or resolved preview
metadata so the displayed default matches the runtime behavior.

## Alternatives considered

### Keep raw prompt templates as the only configuration

Rejected. Raw templates are flexible, but they do not make locale behavior,
output fields, variable contracts, or policy versions inspectable. Attune would
continue relying on comments and service code for behavior that operators need
to understand.

### Add more `tenants.enrich_*` columns for each knob

Rejected. A few columns would be quick to query, but they would scatter policy
identity, versioning, validation, and compatibility rules across persistence and
service code. The policy object should be the unit of reasoning even if the
first storage layer remains simple.

### Create an `enrich_prompt_policies` table immediately

Rejected for the initial implementation. Versioned tables are useful for draft
and rollback workflows, but #107 can be solved with immutable built-in policies,
legacy overrides, richer validation, and run-level provenance. Adding a table
before there are multiple tenant-owned policy versions would increase migration
and Console complexity without changing operator behavior.

### Integrate a hosted prompt registry

Rejected. Products such as LangSmith, Langfuse, PromptLayer, Humanloop, and
MLflow show strong patterns for prompt registries, but Attune should not require
an external service to classify feedback. The relevant ideas are versioned
identity, aliases or active pointers, variable contracts, and traceability.

### Replace prompts with fully structured signatures

Rejected as the sole approach. DSPy-style signatures are useful because they
make inputs and outputs explicit, but Attune still needs natural-language
instructions for classification nuance, tenant Dimensions, localization, and
provider compatibility. The policy should combine structured contracts with a
prompt template, not remove the template.

## Risks / tradeoffs

- A policy abstraction can become a prompt-management product if its scope is
  not kept tight. The implementation should support only enrichment prompting.
- Hash-based legacy versions are less readable than numeric built-in versions,
  but they preserve compatibility without inventing tenant policy history.
- Validating output fields by searching prompt text is imperfect because schema
  enforcement also contributes to behavior. Treat missing mentions as
  diagnostics, not primary warnings, unless the active policy declares them
  mandatory for custom templates.
- Persisting prompt or schema fingerprints improves drift analysis, but stable
  unsalted hashes can become tenant-prompt identifiers. The initial
  implementation keeps those hashes out of stored run metadata.
- Localized built-in templates need tests that assert the same contract across
  languages, otherwise template translations can drift.

## Implementation plan

1. Add `PromptPolicy`, variable/output contract types, and a resolver under
   `internal/service/enrich`.
2. Register `enrich.default@1` with the existing English and Chinese template
   bodies and the current locale-selection behavior.
3. Define canonical policy identity formats, typed policy knobs, legacy custom
   hash inputs, and prompt/schema fingerprints.
4. Refactor `renderPrompt` and `promptVersion` to consume the resolved policy.
5. Expand prompt template validation tests to cover known variables, required
   variables, missing `{{dimensions}}` warnings, unknown-variable compatibility
   warnings, older legacy variables such as `{{modules}}`, and
   `legacy_custom_override` mode.
6. Update eval config resolution so tenant locale/display policy reaches the
   shared resolver, then update fakes and eval tests.
7. Record resolved policy identity in `semantic_extraction_runs.prompt_version`,
   store `prompt_version_id` when an active immutable snapshot exists, and keep
   compact policy metadata/fingerprints in JSON provenance.
8. Extend the Console enrich-config and preview responses with policy metadata
   through the proto contract, then regenerate Go, TypeScript, and OpenAPI with
   `make proto`.
9. Add immutable prompt-policy snapshots for saves, return recent versions from
   enrich-config, seed/backfill a baseline active snapshot during migration, and
   expose an activate-version endpoint with unified audit logging.
10. Update the Settings UI to show active policy identity, structured policy
    knobs, language behavior, recognized variables, required outputs, staged
    restore behavior, custom prompt warnings, and version history with snapshot
   details, active-versus-target diff review, and confirmation-gated activation
   controls.
11. Add unit tests for resolver behavior, HTTP config shape, preview metadata,
    Console display states, and legacy custom template compatibility.
12. Align read/write route authorization with Console role permissions, keep
    prompt policy fields in audit before/after snapshots, and treat omitted
    `display_fields_required` as default-on while preserving explicit false.
13. Guard Console drafts against background refresh overwrites, ignore stale
    preview responses, serialize prompt-version activation in the UI, and show
    a retryable error state when enrich-config cannot be loaded.
14. Enforce same-tenant ownership for active prompt-version pointers and make
    legacy direct tenant config writes clear active snapshot provenance.
15. Keep LLM-cost eval suggestions behind an explicit POST analyze operation,
    and make promote-suggested audit failures fail the mutation like update and
    activate.
16. Expose resolved prompt and schema fingerprints in prompt policy metadata
    and version-history snapshots so Console and semantic-run provenance share a
    comparable drift key.
17. Update `CHANGELOG.md` under `[Unreleased]` and move this proposal's status
    to `Implemented` when the code lands.

## Verification

- `go test ./internal/service/enrich ./internal/handlers/console/enrichconfig ./internal/repo/feedback`
- `go test ./internal/service/eval`
- `go test ./internal/service/replydraft`
- `go vet ./...`
- `go build ./...`
- `make proto`
- `git diff --exit-code proto internal/proto console/src/proto docs/openapi`
- `cd console && pnpm tsc -b --noEmit`
- `cd console && pnpm vitest run`
- `bash scripts/lint-artifacts.sh --strict`
- `make ci-check`

Manual review should confirm:

- a tenant with no custom prompt resolves to `enrich.default@1`
- a tenant with a custom prompt resolves to a stable legacy hash identity
- existing custom prompts that contain `{{content}}` still classify feedback
- custom prompts with unknown legacy variables report warnings instead of
  failing no-op saves
- Console exposes structured output-language, length, display-field, tone, and
  domain-guidance knobs without requiring source-code knowledge
- Console restore-default remains staged until save
- Console save creates a version-history row
- Console can review active-versus-target differences, confirm activation, and
  mark a previous prompt-policy version active
- member/viewer-compatible read paths can load classification settings while
  mutating endpoints remain admin-only
- audit entries include prompt policy configuration and compact policy identity
- eval suggestions use an explicit analyze action rather than a safe/read-only
  GET, and promote-suggested mutations fail if audit logging fails
- database constraints reject cross-tenant active prompt-version pointers
- semantic extraction runs retain the prompt version id, prompt/schema
  fingerprints, and compact policy metadata needed to audit historical output
- prompt policy/version-history API responses expose the same prompt/schema
  fingerprints used by semantic extraction provenance

## References

- [Issue #107: Make enrichment prompt policy configurable](https://github.com/Phixsura/attune/issues/107)
- [LangSmith prompt management](https://docs.langchain.com/langsmith/manage-prompts)
- [Langfuse prompt management data model](https://langfuse.com/docs/prompt-management/data-model)
- [Langfuse prompt version control](https://langfuse.com/docs/prompt-management/features/prompt-version-control)
- [PromptLayer prompt editor and versioning](https://docs.promptlayer.com/features/prompt-registry/prompt-editor-versioning)
- [Humanloop prompts](https://humanloop.com/docs/explanation/prompts)
- [MLflow Prompt Registry](https://mlflow.org/docs/latest/genai/prompt-registry/)
- [Microsoft Semantic Kernel PromptTemplateConfig](https://learn.microsoft.com/en-us/dotnet/api/microsoft.semantickernel.prompttemplateconfig?view=semantic-kernel-dotnet)
- [LlamaIndex prompts](https://developers.llamaindex.ai/python/framework/module_guides/models/prompts/)
- [Haystack PromptBuilder](https://docs.haystack.deepset.ai/docs/promptbuilder)
- [DSPy documentation](https://dspy.ai/)
- [Dify LLM node prompt orchestration](https://docs.dify.ai/en/use-dify/nodes/llm)
- [Dify version control](https://docs.dify.ai/en/use-dify/build/version-control)
