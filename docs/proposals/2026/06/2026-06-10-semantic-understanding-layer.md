# Semantic understanding layer and customer tone

| | |
|---|---|
| **Issue** | #21 |
| **Status** | Implemented |
| **Started** | 2026-06-10 18:30 CST |
| **Related** | #10 (metadata-driven Dimensions), #20 (LLM guard policies), #12 (PostgreSQL integration tier) |

## Problem

Issue #21 originally asked for a hard-coded `sentiment` field on the LLM
classification output plus a new `feedback.sentiment` SQL column. That made
sense before #10 landed, when attune still looked like a fixed customer-feedback
classifier. Current `main` is different: enrichment is driven by per-tenant
Dimensions stored in `tenants.enrich_dimensions`, row output is stored in
`user_feedback.enriched_attrs`, and the console already filters arbitrary
dimension query params through JSONB containment.

Adding another hard-coded column now would undermine the system we just built.
The valuable product idea behind #21 is still right: support and product teams
need to distinguish a neutral report from a frustrated user at the same
severity. But the deeper platform value is broader than customer sentiment:
attune should become a governed semantic understanding layer that can extract
domain-specific meaning from raw data without rewriting core schema for every
vertical.

## Goals

- Ship customer tone as a default semantic dimension for the customer-feedback
  pack.
- Keep the core schema vertical-agnostic: no `sentiment` SQL column, no
  sentiment field on `domain.Enriched`, and no sentiment-specific API response.
- Make Dimensions richer enough for high-quality extraction: descriptions,
  examples, and taxonomy-level definitions should guide the model.
- Add renderer metadata so the console can present important semantic fields
  with a domain-pack-controlled visual treatment without hard-coding field names.
- Validate renderer metadata as part of the Dimension grammar so a pack cannot
  persist unsupported renderer kinds, icons, tones, or taxonomy targets.
- Persist extraction evidence separately from the current row snapshot:
  model/schema/prompt identity, attrs, rationale, structured dropped-attr
  diagnostics, and room for confidence/guard summaries.
- Preserve the existing fast query path: `user_feedback.enriched_attrs` remains
  the current snapshot used by list/detail/stats and JSONB filters.

## Non-goals

- Do not build a full domain-pack installer in this PR.
- Do not add a general filter grammar yet; the existing `?<dim>=<value>` query
  contract remains the console path.
- Do not make `frustrated` automatically urgent. Urgency remains operator
  policy via `urgent_set`.
- Do not expose every advanced Dimension field in the Settings editor yet. The
  editor preserves unknown/generated fields and can grow an advanced pane later.

## Proposal

Keep the core boundary:

```text
Core owns the grammar.
Packs own the vocabulary.
Runs own the evidence.
Workflows own the action.
```

Extend the Dimension grammar with:

- `description`: i18n explanation of what the dimension means.
- `examples`: short extraction examples.
- taxonomy `description` and `examples`: definitions for each allowed value.
- `extraction_hint`: concise model-facing instruction for tricky boundaries.
- `renderer`: generic presentation metadata such as `enum_badge` with icon/tone
  hints per taxonomy value.

Add a small typed semantic-pack registry in Go. The first entry is
`customer_feedback.v1`, which exposes the same default DimensionSet seeded by
the migration. The registry is not yet an installer; it is a stable home for
pack identity and defaults so eval, tests, and future pack lifecycle code stop
depending on scattered constants.

Seed the customer-feedback default pack with `sentiment` as the stable machine
key and `Customer tone` as the display label. The taxonomy is:

- `positive`: praise, appreciation, delight, or successful outcome.
- `neutral`: factual report/request with little emotional charge.
- `negative`: dissatisfaction, complaint, disappointment, or criticism.
- `frustrated`: repeated failure, blocked task, refund/cancel/escalation
  language, or patience running out.

The machine key stays `sentiment` so `GET /fb/v1/console/feedback?sentiment=frustrated`
matches #21 and remains easy to type. The product surface can call it Customer
tone.

Add `semantic_extraction_runs` as an append-only evidence table. Each full LLM
classification writes one run in the same transaction as the `user_feedback`
snapshot update and outbox rows. The table stores subject identity, pack/schema
identity, model, attrs, rationale, dropped-attr diagnostics, and placeholders
for confidence and guard summaries. This is deliberately separate from
`user_feedback.enriched_attrs`: the row keeps the fast current-state snapshot;
the run table keeps history and provenance for future recompute, evaluation,
and audit.

## Alternatives Considered

### Add `user_feedback.sentiment`

Rejected. It is the fastest path for #21 alone but breaks the metadata-driven
Dimensions direction. Every later vertical would repeat the mistake with
`buyer_intent`, `contract_risk`, `blast_radius`, and similar columns.

### Rename the dimension to `customer_tone`

Good product language, but less compatible with the issue and existing filter
expectation. Keep `sentiment` as the stable machine key and use `Customer tone`
as the display name.

### Store only extraction runs and remove `enriched_attrs`

Rejected for now. JSONB filtering and console list rendering already use
`enriched_attrs` effectively. Removing the snapshot would make every list query
depend on latest-run selection and complicate indexes before the product needs
it.

## Risks / Tradeoffs

- Existing tenants with heavily customized dimensions might not want a new
  default. The migration only appends `sentiment` to tenants that still have the
  current default dimension trio (`type`, `severity`, `labels`) and lack
  `sentiment`.
- Renderer metadata is intentionally narrow. It solves enum badges now and can
  grow new renderer kinds later. Unknown renderer metadata is rejected rather
  than silently persisted.
- Dropped diagnostics are bounded summaries, not raw replay logs. This makes
  them useful for audit/eval without turning the run table into an unbounded
  sensitive-payload sink.
- Extraction runs add write volume. One row per LLM classification is acceptable
  at current scale and buys provenance early while the schema is still cheap to
  evolve.

## Implementation Plan

1. Extend domain/proto Dimension and Taxonomy metadata.
2. Regenerate Go, TS, and OpenAPI artifacts from proto.
3. Update prompt rendering so descriptions, examples, and extraction hints guide
   the LLM.
4. Add migration `019_semantic_understanding.sql` with the customer-tone seed
   and `semantic_extraction_runs`.
5. Persist a semantic extraction run for each full LLM classification in the
   same transaction as `MarkDone` and outbox queueing.
6. Render `enum_badge` metadata in the console's generic `DimensionChips`.
7. Validate renderer metadata in `domain.Dimension.Validate`.
8. Add bounded `FilterAttrsWithDiagnostics` output and persist it in
   `semantic_extraction_runs.dropped_attrs`.
9. Introduce the `customer_feedback.v1` semantic pack fixture.
10. Cover prompt, proto round-trip, repo integration, console rendering, and
    browser smoke behavior.

## Verification

- Unit tests for prompt rendering, proto round-trip, and renderer-preserving
  dimension conversion.
- PostgreSQL integration tests for the extraction-run table and transaction
  path.
- Console tests for renderer badge output.
- Browser smoke test against a temporary Postgres + attune server + Vite
  console: login, Settings classification view, prompt preview with customer
  tone semantics, and Feedback list enum-badge rendering.
- Existing Go/console quality gates.

## References

- `docs/proposals/2026/06/2026-06-07-flat-labels.md`
- `docs/proposals/2026/06/2026-06-10-llm-guard-policies.md`
- `docs/testing.md`
