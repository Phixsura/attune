<!-- markdownlint-disable MD013 -->

# Unique Empty-State Filter Links for Public Board and Roadmap

| Field | Value |
|---|---|
| Issue | [#222](https://github.com/Phixsura/attune/issues/222) |
| Status | Implemented |
| Started | 2026-07-16T01:57:29+08:00 |
| Related | [Public Board and Roadmap Card Hit Targets](./2026-07-15-public-board-roadmap-card-hit-targets.md), [Public Roadmap From Workflow States](./2026-07-14-public-roadmap-from-workflow-states.md) |

## Problem

The public board and public roadmap both render a filterable search form plus
an empty-state recovery link when the current filters return no results. In the
filtered-empty state, that recovery link currently reuses the same visible text
as the primary filter-reset link in the form. The result is two identical
accessible names on the page.

That duplication is harmless functionally, but it makes the interface less
polished than it should be. It also makes browser automation and assistive
technology harder than necessary because the page no longer has a single,
obvious target for the recovery action.

## Goals

- Give the filtered-empty recovery link a unique, descriptive label.
- Keep the visual treatment and destination unchanged.
- Preserve the existing search-row "Clear filters" affordance.
- Update browser and render coverage so the new label stays stable.

## Non-goals

- Do not change the filter semantics or query-string behavior.
- Do not redesign the search form or empty-state layout.
- Do not remove the top-level reset affordance from either page.

## Proposal

Rename the empty-state recovery link on the public board to "Show all requests"
and the empty-state recovery link on the public roadmap to "Show all roadmap
items". Both links still navigate to the same unfiltered surface as before.

This keeps the recovery action obvious, avoids duplicate accessible names, and
makes the browser smoke more precise because it can target the empty-state
action without colliding with the search-row filter reset link.

## Alternatives considered

- Keep the duplicate labels.
  - Rejected because it keeps the page less accessible and makes automation
    brittle.
- Add `aria-label` only.
  - Rejected because the visible copy would still be ambiguous when scanned in a
    browser or by sighted keyboard users.
- Remove the empty-state link entirely.
  - Rejected because the recovery path is useful when a filter turns up no
    results.

## Risks / tradeoffs

- Test fixtures and browser assertions that look for the old label need to be
  updated together or they will fail.
- The new copy should stay concise enough to fit comfortably on small screens.

## Implementation plan

1. Update the public board empty-state link text.
2. Update the public roadmap empty-state link text.
3. Refresh the portal render tests and browser smoke assertions that expect the
   previous label.
4. Re-run targeted Go and console/browser verification.

## Verification

- `go test ./internal/handlers/portal/...`
- `node console/scripts/public-board-smoke.mjs`
- Manual browser check on the filtered-empty board and roadmap states

## References

- [#222](https://github.com/Phixsura/attune/issues/222)
- [Public Board and Roadmap Card Hit Targets](./2026-07-15-public-board-roadmap-card-hit-targets.md)
- [Public Roadmap From Workflow States](./2026-07-14-public-roadmap-from-workflow-states.md)
