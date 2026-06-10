# Language-aware enrichment

| | |
|---|---|
| **Issue** | #22 |
| **Status** | Implemented |
| **Started** | 2026-06-10 22:10 CST |
| **Related** | #10 (metadata-driven Dimensions), #21 (semantic understanding layer), #24 (confidence + cost), #26 (reply draft), #27 (theme digest) |

## Problem

Issue #22 was opened when the enricher still had a Chinese-first prompt and a
fixed classification shape. English and Japanese feedback could produce Chinese
`title` / `rationale` output, which looked wrong in the console and lowered
classification quality because the model had to translate internally before
classifying.

The current `main` branch is more advanced than the issue's original context:

- Classification is now metadata-driven through per-tenant `Dimensions`, not
  hard-coded `type` / `severity` / `modules` fields.
- `sentiment` landed as a semantic Dimension in the customer-feedback pack
  rather than a dedicated SQL column.
- `semantic_extraction_runs` records extraction provenance separately from the
  fast `user_feedback.enriched_attrs` snapshot.
- The default prompt is now English-canonical and prints i18n hints for
  configured taxonomy values.

Those changes improve multilingual classification, but they do not solve the
core user-facing gap: Attune still does not detect, persist, expose, or audit the
source language of a feedback row, and it has no language-aware prompt policy.
That missing metadata will matter more as reply drafts, digests, and semantic
search build on top of the same enrichment run.

After reviewing the first browser-verified implementation, there is a second
product gap: source-language preservation alone is confusing for operators. A
Chinese-speaking team triaging English feedback should not have to read every
AI summary in English just because the customer wrote English. At the same
time, overwriting the source-language summary would lose the user's tone and
make reply workflows weaker.

The product needs two tracks:

- **Native track**: preserve what the user said, in the user's language.
- **Operator-display track**: make the console immediately readable in the
  tenant/operator language.

## Goals

- Detect the source language before the LLM classification call.
- Persist the detected language on `user_feedback.language` as a small ISO
  639-1-style code (`zh`, `en`, `ja`, `unknown`, and future extensions).
- Expose language in the console feedback list/detail API and render a compact
  language badge in the console.
- Preserve native `title` and `rationale` in the original feedback language
  whenever possible.
- Generate operator-display `title` and `rationale` in the tenant locale, so
  the console remains readable for the team doing triage.
- Keep native and operator-display summaries separately addressable in API and
  persistence.
- Select a built-in prompt variant from the operator-display language when the
  tenant has not supplied a custom prompt.
- Record language/prompt provenance in `semantic_extraction_runs` through
  prompt-version metadata.
- Keep detection cheap, deterministic, dependency-free, and observable.

## Non-goals

- Do not make language a user-configurable Dimension. Language is row metadata
  about the input and processing path, not product semantics like customer tone,
  severity, or product area.
- Do not introduce an external language-detection dependency in this PR.
- Do not add a second LLM call solely for language detection.
- Do not translate raw feedback content.
- Do not translate Dimension display labels at classification time. Dimension
  display remains owned by the existing `I18nString` metadata resolver.
- Do not build per-tenant language policy editing in the console yet.
- Do not retroactively classify historical rows in the migration.

## Proposal

Treat language as enrichment metadata and summary language as a two-track
enrichment output:

```text
Dimensions describe what the user is saying.
Source language describes how the row arrived.
Operator locale describes how the team wants to read it.
Extraction runs describe how the system produced the result.
```

Add `user_feedback.language TEXT NULL` via a new migration. New rows start with
`NULL` at ingest time. The enricher detects language after loading and claiming
the row, then persists it in the same write that marks the row enriched. Ignored
triage rows should also persist a language value so the console can display a
consistent row shape even when the LLM was skipped.

Use `tenants.locale` as the first operator-display locale. This already exists
from migration `008_tenant_locale.sql`, defaults to `zh-CN`, and is returned by
the console `/me` shape. Per-user locale can replace or refine this later when
RBAC/user profiles land; the row model should not depend on that future work.

The first detector is a small rule-based function in `internal/service/enrich`.
It uses Unicode script ratios and ASCII word signals:

- Chinese: Han characters dominate meaningful text.
- Japanese: Hiragana or Katakana present, or Han mixed with Japanese kana.
- English: mostly Latin/ASCII words with enough alphabetic signal.
- Unknown: too short, mostly symbols/URLs/code, or ambiguous mixed content.

The detector returns a typed code rather than a free string. The first supported
codes are:

| Code | Meaning | Prompt behavior |
|---|---|---|
| `zh` | Chinese | Preserve Chinese native output when detected. |
| `en` | English | Preserve English native output when detected. |
| `ja` | Japanese | Preserve Japanese native output when detected. |
| `unknown` | Ambiguous / insufficient signal | Ask the model to preserve source-language phrasing if clear from context. |

### Output model

Keep existing `enriched_title` and `enriched_rationale` as the native/source
track:

- `enriched_title`: summary in the source feedback language when the source
  language is clear.
- `enriched_rationale`: rationale in the source feedback language when the
  source language is clear.
- `language`: detected source language.

Add operator-display fields:

- `enriched_display_title TEXT`
- `enriched_display_rationale TEXT`
- `enriched_display_locale TEXT` (the tenant locale used for display, such
  as `zh-CN`)

For a Chinese tenant triaging English feedback:

```json
{
  "language": "en",
  "enriched_title": "Payment submit fails",
  "enriched_rationale": "Checkout flow is blocked",
  "enriched_display_locale": "zh-CN",
  "enriched_display_title": "付款提交失败",
  "enriched_display_rationale": "结账流程受阻"
}
```

For same-language rows, the display fields may duplicate the native fields or be
left empty and resolved by the API as fallback. The storage contract should
prefer explicit values for freshly enriched rows so historical console rendering
does not change if tenant locale changes later.

Prompt selection should be conservative:

1. If `ClassifyConfig.PromptTemplate` is set, use the tenant custom prompt.
   Custom prompt authors own their language policy.
2. Otherwise, choose the built-in prompt variant from the operator-display
   language derived from `tenants.locale`.
3. Unsupported display codes fall back to the English built-in prompt.

The built-in prompt should ask for both tracks:

- `title` and `rationale`: source-language/native track.
- `display_title` and `display_rationale`: operator-locale track.

When source language and operator locale are effectively the same, the model may
return identical strings. When they differ, the display fields must be localized
for the operator. The LLM should not translate raw content; it should summarize
the same facts into the operator language.

Japanese can initially share the English prompt because the key instruction is
"preserve native output and generate operator-display output"; a native Japanese
prompt can follow if corpus results show quality issues.

Record prompt provenance as:

- `tenant_custom` when a tenant prompt is used.
- `default:zh` or `default:en` for the currently supported built-in prompt
  languages. Japanese display currently uses the English prompt while still
  requesting Japanese display output.

Store additional language metadata in the semantic run without changing the
table schema by using existing JSON columns:

- `guard_summary.language`: detected code and detector name/version.
- `rationale.language`: detected source code, display locale, native summary,
  and display summary.

Expose language in the proto contract by appending fields to
`Feedback` and `FeedbackDetail`, and expose display fields as append-only proto
fields too. Regenerate Go, TypeScript, and OpenAPI via `make proto`.

The console should default to the operator-display title/rationale when present,
with a compact source-language badge beside the row. The detail sheet should
show the display summary first, then make the native/source summary available in
a clearly labeled secondary block when it differs. This keeps triage fast while
preserving source-language evidence for replies and audit.

## Alternatives Considered

### Model language as a Dimension

Rejected. This would make language filterable through the existing
`enriched_attrs` JSONB path, but it mixes processing metadata with tenant-owned
semantic taxonomy. Language also needs to exist for ignored rows and future
reply/digest pipelines that may run without ordinary classification attrs.

### Ask the LLM to detect language in the same classification JSON

Rejected for the first implementation. It would avoid writing a detector but
would make language unavailable before prompt selection and would trust the LLM
for metadata that can be determined cheaply. It also makes ignored triage rows
language-less.

### Only preserve source-language summaries

Rejected after browser review. This is useful for reply fidelity but confusing
for operators who cannot read the customer's language. A language badge without
localized operator output explains the problem but does not solve the triage
workflow.

### Only store operator-language summaries

Rejected. It optimizes the console but loses customer-language tone and weakens
reply generation, outbound integrations, and audit. The source/native track
must remain available.

### Add a second LLM call for language detection

Rejected. It increases cost and latency for a task that can be handled
deterministically well enough for the first supported languages.

### Use a third-party language detection library

Deferred. Libraries can improve coverage for many languages, but they add supply
chain and binary-size cost. Start dependency-free, measure failures, and add a
library only if corpus results justify it.

### Keep one English-canonical prompt only

Rejected. The current prompt is already much better than the old Chinese-only
prompt, but it still does not persist language, expose it in the console, or
make language preservation an auditable part of the extraction policy.

## Risks / Tradeoffs

- Short feedback can be hard to classify by language. Mitigate by returning
  `unknown` instead of overconfident guesses.
- Mixed-language feedback can be ambiguous. Prefer the dominant natural
  language when one is obvious; otherwise use `unknown` and let the prompt ask
  the model to preserve source-language phrasing.
- Chinese and Japanese both use Han characters. Kana presence should strongly
  indicate Japanese; Han-only Japanese text may be detected as Chinese or
  unknown. This is acceptable for the first dependency-free detector and should
  be tracked in corpus tests.
- Tenant custom prompts can still produce the wrong output language. This is an
  intentional tradeoff in Phase 1: custom prompt authors own the wording
  contract. In Phase 2, custom prompt validation should either require the
  display fields or explicitly mark the tenant as using a legacy custom prompt.
- Adding display fields increases prompt and response complexity. Mitigate by
  keeping the output contract flat (`title`, `rationale`, `display_title`,
  `display_rationale`) and by falling back to native fields if display fields
  are empty.
- Tenant locale changes can make existing display summaries stale. Store
  `enriched_display_locale` per row and treat re-localization as a future
  re-enrichment/recompute workflow rather than silently changing historical
  rows.
- The outbox envelope now includes `feedback.language` as an additive optional
  field. Consumers that ignore unknown JSON fields remain compatible, while
  workflow systems can route or render by detected language.
  Native and display summaries are also added inside `feedback.enriched`.

## Implementation Plan

### Phase 1 — source-language foundation

1. Add this proposal and update its status as the PR lands.
2. Add migration `020_feedback_language.sql` with
   `ALTER TABLE user_feedback ADD COLUMN IF NOT EXISTS language TEXT`.
3. Introduce a typed detector in `internal/service/enrich` with a table-driven
   corpus test covering Chinese, English, Japanese, mixed text, URLs/code, and
   too-short content.
4. Thread language through `feedback.EnrichInput`, `domain.Enriched` or a small
   persist metadata parameter, `domain.Snapshot`, and `MarkDone` / `MarkDoneTx`.
5. Update `runFullEnrich`, ignored triage, and fast-path triage to detect and
   persist language consistently.
6. Split built-in prompt rendering into language-aware variants while preserving
   tenant custom prompt behavior.
7. Update semantic run construction so prompt version and JSON metadata include
   detected language.
8. Append language fields to `proto/attune/v1/ingest.proto`, then run
   `make proto`.
9. Update repo console projections and handlers to scan and emit language.
10. Render language badges in the feedback list and detail sheet; add i18n
    strings for badge labels.
11. Add unit, integration, and console tests.
12. Add a `CHANGELOG.md` `### Added` entry in the implementation PR.

Phase 1 has been implemented as the foundation.

### Phase 2 — operator-localized display

1. Add a migration for `enriched_display_title`,
   `enriched_display_rationale`, and `enriched_display_locale`.
2. Extend `feedback.EnrichInput` with tenant locale by joining `tenants.locale`
   in `LoadForEnrich`.
3. Extend the built-in prompt and structured schema to request
   `display_title` and `display_rationale` in the tenant locale.
4. Extend parsing so native `title` / `rationale` stay out of `Attrs`, and
   display fields persist through enrichment metadata.
5. Persist display fields in `MarkDone` / `MarkDoneTx`.
6. Append display fields to `Feedback` / `FeedbackDetail` proto shapes and
   regenerate artifacts.
7. Update console list/detail to default to display title/rationale, with a
   source-language badge and a secondary native/source-language section.
8. Update outbox envelope additively so consumers can choose native or display
   summaries.
9. Add tests for cross-language rows, same-language fallback, and legacy custom
   prompts.

Phase 2 has been implemented in the same issue branch.

## Verification

- Detector unit tests report per-code sample counts and keep a simple
  precision/recall summary in test failure output.
- Prompt-rendering tests prove `zh`, `en`, `ja`, and `unknown` choose the
  expected built-in prompt behavior, and tenant custom prompts override the
  built-in language selection.
- Parse/classify tests prove non-English `title` and `rationale` survive
  parsing and persistence unchanged.
- Cross-language classify tests prove English source feedback under a `zh-CN`
  tenant produces English native fields and Chinese display fields.
- PostgreSQL integration test covers ingest -> enrich -> persisted
  `user_feedback.language` plus display fields -> console projection.
- Console Vitest covers feedback list/detail language badge rendering and
  operator-display summary rendering.
- Proto sync check passes after `make proto`.
- Existing Go and console gates pass.

## References

- `docs/proposals/2026/06/2026-06-07-flat-labels.md`
- `docs/proposals/2026/06/2026-06-10-semantic-understanding-layer.md`
- `docs/proposals/2026-06-06-enricher-per-tenant-prompt.md`
- `proto/attune/v1/ingest.proto`
- `internal/service/enrich/enricher.go`
- `internal/repo/feedback/feedback.go`
