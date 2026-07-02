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
- Preserve the existing feature ownership boundaries: routes compose feature
  APIs, feature packages do not import each other.
- Avoid new backend storage until the product semantics prove stable in Console.

## Non-goals

- Replace the feedback queue, classification dashboard, or search-quality
  dashboard.
- Add a new aggregate API or rollup table in this change.
- Add model-generated remediation text. The first control tower uses deterministic
  thresholds and links to traceable evidence.

## Proposal

Add a Console Control Tower at `/control-tower` and make it the authenticated
default destination. The page composes the existing classification-quality and
search-quality APIs and presents three layers:

1. **Operating lanes** for understanding quality, retrieval quality, and index
   coverage.
2. **Attention queue** for the most important current quality risks, capped to a
   small list and linked to the relevant analytics surface.
3. **Proof trail** with the active classification warning count, highest
   zero-result query, search engagement, and ranking version.

The control tower is route-private composition code under `console/src/routes/`.
This keeps the console dependency graph clean: route files may compose multiple
features, while feature packages remain isolated.

## Alternatives considered

- **Add another analytics page.** Rejected because the missing product shape is a
  default operating overview, not another deep-dive report.
- **Build a backend aggregate endpoint first.** Rejected for the first slice
  because the required signals already exist and the front-end composition lets
  us validate the product model with less schema churn.
- **Keep `/feedback` as the default route.** Rejected because a raw queue answers
  "what can I triage" before it answers "what needs attention."

## Risks / tradeoffs

- Thresholds are deterministic and simple. They should be calibrated against real
  tenant traffic as operators use the surface.
- The page issues two API requests on load. Both endpoints already exist and use
  bounded windows, so the added cost is acceptable.
- The control tower can only expose evidence that current APIs return. Richer
  action recommendations require explicit backend support and auditability.

## Implementation plan

1. Add `/control-tower` as a typed TanStack route with `usage:view` access.
2. Compose `classificationQualityQuery(defaultClassificationQualityFilters)` and
   `searchQualityQuery(defaultSearchQualityFilters)` in the route loader.
3. Add a route-private page that computes overall severity, operating lanes,
   bounded risk cards, and proof-trail rows.
4. Add an Overview navigation group and make `/` redirect to `/control-tower`.
5. Add i18n strings and route/component tests.

## Verification

- `console/node_modules/.bin/tsc -b --noEmit`
- `console/node_modules/.bin/vite build`
- `console/node_modules/.bin/vitest run src/routes/_authed.control-tower.test.tsx src/lib/console-ia.test.ts`

## References

- [Semantic search quality platform](./2026-07-02-semantic-search-quality-platform.md)
- [Classification quality dashboard](./2026-07-02-classification-quality-dashboard.md)
- [Console IA layout overhaul](../06/2026-06-21-console-ia-layout-overhaul.md)
