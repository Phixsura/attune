# Enrichment runtime operator experience for a world-class Console

| Field | Value |
| --- | --- |
| **Issue** | [#80](https://github.com/Phixsura/attune/issues/80) |
| **Status** | Implemented |
| **Started** | 2026-06-18 03:30 CST |
| **Related** | [2026-06-17-enrichment-runtime-control-plane.md](./2026-06-17-enrichment-runtime-control-plane.md) |

## Problem

The control plane for #80 now exists and is functional, but the current Console
 experience still behaves too much like an internal debug surface:

- it exposes opaque identifiers as primary content
- it mixes live decision-making state with historical noise
- it explains knobs weakly relative to the operational risk they carry
- it forces the operator to translate implementation detail into action

That is not enough for a production-grade operator experience. A world-class
runtime Console must let an operator answer four questions quickly:

1. Why does this page matter right now?
2. Which nodes are currently relevant?
3. What will this knob change operationally?
4. How do I act safely and recover fast?

## Goals / Non-goals

### Goals

- Make the enrichment runtime page readable by an operator who did not build the
  subsystem.
- Keep live decision signals primary and historical/debug signals secondary.
- Explain each runtime control in operational language, not implementation
  language alone.
- Make rollback, convergence, and safety posture legible without reading source
  code or backend logs.

### Non-goals

- Do not replace backend convergence semantics in this iteration.
- Do not invent a fully new design system for Settings pages.
- Do not hide all technical detail; move it behind progressive disclosure.
- Do not build a custom charting/telemetry layer before the data model supports
  it cleanly.

## Proposal

### 1. Structure the page around operator flow

The page should read in this order:

1. **Value and use-case framing**
2. **Current deployment health**
3. **Recommended operator workflow**
4. **Editable policy with field-level explanations**
5. **Live nodes relevant to the current decision**
6. **Historical revisions and secondary node details**

This preserves the existing control-plane feature set while making the page
feel like a real operational workstation instead of a settings dump.

### 2. Keep active nodes primary, age out inactive nodes visually

The main instances surface should focus on currently relevant nodes. Historical,
expired, or previously active nodes should not dominate the main table when the
deployment has already converged on a smaller active set.

Initial UI rule:

- show a primary **active instances** table
- move the remaining rows into a separate **historical / inactive instances**
  section with lower visual weight
- keep raw instance and boot identifiers inside a technical-details affordance

Longer-term backend follow-up:

- expose explicit frontend-consumable active/stale/expired classification per
  instance so the UI does not need to infer presentation priority

### 3. Translate controls into operator intent

Every mutable field should answer “what happens if I change this?” directly in
the form itself.

Examples:

- queue capacity: absorb larger bursts before backpressure
- worker concurrency: increase parallel enrichment throughput
- batch size/window: trade latency for provider efficiency
- sweep interval: control backlog recovery cadence
- QPS / burst: cap LLM provider pressure and spend

This guidance belongs inline, not only in a separate proposal or audit doc.

### 4. Localize runtime semantics into product language

The page should prefer terms like:

- 已应用 / 收敛中 / 已退化
- 当前在线节点 / 历史节点
- 目标版本 / 当前观测 / 最近更新

instead of exposing implementation-shaped strings like:

- `applied`
- `observed v12`
- opaque actor IDs
- raw runtime instance identifiers

Technical detail still matters, but it should become secondary context.

### 5. Reviewable experience standards

The page should be reviewable against explicit UX bars:

- **Decision clarity:** the first screen tells the operator whether action is
  needed.
- **Action clarity:** every knob has an operational explanation.
- **Risk clarity:** dangerous actions show safe next steps and rollback paths.
- **Noise control:** stale data does not crowd live operational state.
- **Audit clarity:** mutation history remains legible without exposing internal
  identifiers as primary UI.

## Alternatives considered

### Keep the current page and only polish copy

Rejected. Copy helps, but it does not solve information hierarchy or the
active-vs-historical state problem.

### Hide all technical details completely

Rejected. Operators still need access to concrete IDs when debugging; the right
solution is progressive disclosure, not deletion.

### Build a separate “advanced mode” page

Rejected for now. The current Settings information architecture is already good
enough if we improve prioritization inside the page itself.

## Risks / tradeoffs

- If the frontend infers “active” rows from the current summary count, the
  presentation may be directionally correct but not perfect for edge cases.
- Additional inline guidance increases page length; this is acceptable because
  the page is a work surface, not a compact summary tile.
- More productized language can drift from backend terminology if we do not keep
  helper mappings explicit and tested.

## Implementation plan

1. Add a dedicated proposal for operator experience quality on top of the
   existing control-plane proposal.
2. Refine the runtime page so raw IDs are secondary, not primary.
3. Add field-level operator guidance to the editable policy form.
4. Split live-relevant instance presentation from historical/inactive nodes.
5. Localize status, condition, and history metadata into operator-facing copy.
6. Re-run frontend tests, type-checking, and browser verification.

## Verification

- `pnpm vitest run src/features/settings/components/enrichment-runtime-page.test.ts src/features/settings/api/enrichment-runtime.test.ts`
- `pnpm tsc -b --noEmit`
- browser verification on `/settings?section=enrichment_runtime`
  - confirms value framing is visible
  - confirms raw IDs are no longer primary UI content
  - confirms active runtime state is easier to distinguish from historical rows

## Review

### Current strengths after implementation

- The page now explains value, typical use cases, and operator flow.
- Primary UI no longer exposes opaque actor IDs directly.
- Technical details remain accessible without dominating the page.
- The runtime controls are substantially closer to an operator workstation than
  an internal admin form.

### Remaining gaps

- Backend should eventually expose explicit active/stale/expired row semantics
  for first-class UI partitioning.
- The page still relies on textual summaries instead of richer trend visuals.
- Configuration summaries and revision history can be further normalized into
  more human-readable operational language.

## References

- [Issue #80](https://github.com/Phixsura/attune/issues/80)
- [2026-06-17-enrichment-runtime-control-plane.md](./2026-06-17-enrichment-runtime-control-plane.md)
