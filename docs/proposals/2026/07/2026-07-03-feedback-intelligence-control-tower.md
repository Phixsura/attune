# Feedback Intelligence Control Tower

| Field | Value |
|---|---|
| Issue | [#162](https://github.com/Phixsura/attune/issues/162) |
| Status | Implemented |
| Started | 2026-07-03T00:26:00+08:00 |
| Related | [#207](https://github.com/Phixsura/attune/pull/207), [semantic search quality platform](./2026-07-02-semantic-search-quality-platform.md), [classification quality dashboard](./2026-07-02-classification-quality-dashboard.md) |

## Problem

Attune has strong operating surfaces for feedback triage, classification quality,
search quality, workflow, audit, and deployment health. The product still lacks
a first screen that makes the whole system feel coherent. Users can inspect each
subsystem, but they must mentally assemble the answer to three basic questions:

1. Is the feedback intelligence loop healthy?
2. Which quality risk needs attention first?
3. What evidence explains that risk?

That gap makes the project feel like a set of well-engineered modules rather than
an opinionated feedback intelligence product.

## Goals

- Make the authenticated landing page an operational overview instead of a raw
  queue.
- Synthesize classification quality, semantic search quality, and index coverage
  into one low-noise status model.
- Show a bounded attention queue with direct links into the existing deep-dive
  pages.
- Persist operator action state so quality risks can move from detected to
  acknowledged, resolved, or dismissed without losing the evidence trail.
- Provide a deterministic demo workspace that shows the full feedback
  intelligence loop on a fresh local install.
- Preserve the existing feature ownership boundaries: routes compose feature
  APIs, feature packages do not import each other.

## Non-goals

- Replace the feedback queue, classification dashboard, or search-quality
  dashboard.
- Add a new cross-domain aggregate API or rollup table in this change.
- Add model-generated remediation text. The control tower uses deterministic
  thresholds, deterministic recommendation keys, and links to traceable
  evidence.
- Add owner assignment, due dates, SLA policy, or escalation routing to quality
  actions.

## Proposal

Add a Console Control Tower at `/control-tower` and make it the authenticated
default destination. The page composes the existing classification-quality and
search-quality APIs, then overlays a small quality-action ledger for operator
state. The page presents four layers:

1. **Operating lanes** for understanding quality, retrieval quality, and index
   coverage.
2. **Action queue** for the most important current quality risks, capped to a
   small list, linked to the relevant analytics surface, and backed by persisted
   action status.
3. **Resolution controls** that let operators acknowledge a risk, mark it
   resolved after verification, or dismiss it with the same deterministic action
   key that generated the recommendation.
4. **Proof trail** with the active classification warning count, highest
   zero-result query, search engagement, and ranking version.

The Control Tower keeps aggregate judgment in route-private composition code
under `console/src/routes/`. This keeps the Console dependency graph clean:
route files may compose multiple features, while feature packages remain
isolated.

The quality-action ledger is intentionally narrow. It stores the action key,
signal, severity, target path, metric label/value, recommendation key, bounded
evidence JSON, actor, and status. It does not duplicate source metrics or become
the authority for classification/search health. The Console upserts the latest
operator state for deterministic action keys such as
`control_tower.zero_result`.

The demo workspace is a CLI command, `attune demo seed`, that creates or
refreshes a demo tenant, seeds realistic enriched feedback, semantic extraction
events, search telemetry, and an acknowledged quality action. This gives local
evaluators a reproducible path from a fresh install to a populated Control Tower
without requiring live LLM calls.

## Alternatives considered

- **Add another analytics page.** Rejected because the missing product shape is a
  default operating overview, not another deep-dive report.
- **Build a backend aggregate endpoint.** Rejected because the required source
  signals already exist, and front-end composition avoids creating a second
  metrics authority. A backend aggregate endpoint remains useful when multiple
  clients need the same synthesized health model.
- **Store quality action state in browser storage.** Rejected because action
  state should follow the tenant and operator workflow, not a single device.
- **Model a full incident-management system.** Rejected because owner, due date,
  policy, and escalation semantics are larger than the current quality loop.
- **Keep `/feedback` as the default route.** Rejected because a raw queue answers
  "what can I triage" before it answers "what needs attention."

## Risks / tradeoffs

- Thresholds are deterministic and simple. They should be calibrated against real
  tenant traffic as operators use the surface.
- The page issues three bounded API requests on load. The two metrics endpoints
  already exist, and the action ledger query is limited by tenant and status.
- Quality actions store operator state, not metric snapshots. If source metrics
  change, the current risk severity still comes from the live quality APIs.
- The demo seed writes realistic synthetic data into a real tenant. The command
  uses stable idempotency keys and clears only telemetry that carries the demo
  seed marker.
- The control tower can only expose evidence that current APIs return. Richer
  action recommendations require explicit support and auditability.

## Implementation plan

1. Add `/control-tower` as a typed TanStack route with `usage:view` access.
2. Compose `classificationQualityQuery(defaultClassificationQualityFilters)`,
   `searchQualityQuery(defaultSearchQualityFilters)`, and
   `qualityActionsQuery({ status: "all" })` in the route loader.
3. Add a route-private page that computes overall severity, operating lanes,
   bounded action cards, recommendation keys, status controls, and proof-trail
   rows.
4. Add a quality-action proto, migration, repository, console handler, generated
   OpenAPI/Go/TypeScript bindings, and router wiring.
5. Add `attune demo seed` to create a deterministic demo tenant with enriched
   feedback, semantic extraction events, search telemetry, and quality-action
   state.
6. Add an Overview navigation group and make `/` redirect to `/control-tower`.
7. Add i18n strings, API tests, route/component tests, and runtime-smoke
   coverage for the Control Tower path and action table.

## Verification

- `make proto`
- `go test ./internal/repo/feedback ./internal/handlers/console/feedback ./internal/handlers/console ./cmd/attune`
- `cd console && pnpm --ignore-workspace tsc -b --noEmit`
- `cd console && pnpm --ignore-workspace vitest run src/routes/_authed.control-tower.test.tsx src/features/quality-actions/api/quality-actions.test.tsx`
- `make ci-check`

## References

- [Semantic search quality platform](./2026-07-02-semantic-search-quality-platform.md)
- [Classification quality dashboard](./2026-07-02-classification-quality-dashboard.md)
- [Console IA layout overhaul](../06/2026-06-21-console-ia-layout-overhaul.md)
