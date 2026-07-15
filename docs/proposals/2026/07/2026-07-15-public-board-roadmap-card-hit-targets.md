<!-- markdownlint-disable MD013 -->

# Public Board and Roadmap Card Hit Targets

| Field | Value |
|---|---|
| Issue | [#222](https://github.com/Phixsura/attune/issues/222) |
| Status | Implemented |
| Started | 2026-07-15T00:00:00+08:00 |
| Related | [Public Roadmap From Workflow States](./2026-07-14-public-roadmap-from-workflow-states.md), [Public Voting Board](./2026-07-13-public-voting-board-mvp.md) |

## Problem

The public board and public roadmap render request cards with a visible title
link, but the rest of the card surface is not a reliable click target. In real
browser use, that makes the interface feel narrower than it looks: users have
to aim for the title text instead of being able to open a request from the card
itself.

That is a small interaction gap, but it matters on a public-facing product. A
world-class board should make the primary navigation affordance obvious,
generous, and consistent across desktop and mobile.

## Goals

- Make public request cards open their detail pages from the full card surface.
- Keep vote and comment controls independently clickable.
- Preserve the existing visible title link for keyboard and screen-reader use.
- Surface a subtle freshness tag on each public request card, while still
  respecting the timestamp-hiding policy.
- Add browser coverage that clicks a non-title area of the card and verifies the
  detail page opens.
- Keep the change localized to the shared portal card templates.

## Non-goals

- Do not redesign the request detail page.
- Do not change public routing or query persistence.
- Do not alter vote, comment, or moderation behavior.
- Do not introduce a new client-side routing layer.

## Proposal

Add an invisible full-surface overlay link to each request card in the public
board, public roadmap, and similar-request sections. Keep the visible title
anchor in place, but let the whole card behave like a large hit target while
buttons and other interactive controls stay above the overlay.

At the same time, surface a compact freshness tag near the title using the
public request timestamp projection. The visible label stays short for quick
scan, while the exact timestamp remains in tooltip and aria text. This gives
the board and roadmap a more informative at-a-glance scan while still hiding
timestamps when the tenant policy forbids them. The rendered element should
also carry machine-readable `time` metadata so the timestamp remains useful to
assistive tech, crawlers, and future client-side enhancements.

Pair the larger hit target with light hover and focus treatment so the card
reads as a deliberate navigation surface instead of a static container.

Extend the browser smoke test to click the card body, not just the title text,
so the desired interaction stays protected by a real browser run.

## Alternatives considered

- Leave the card title as the only link.
  - Rejected because the effective hit target is too small for a public
    browsing surface.
- Wrap the entire card in an anchor.
  - Rejected because the cards already contain vote buttons and other
    interactive controls.
- Add a JavaScript click handler on the whole card.
  - Rejected because the shared portal already has a stable server-rendered
    navigation model and the overlay link is simpler and more accessible.

## Risks / tradeoffs

- The overlay must sit below buttons and other interactive controls or it will
  steal clicks from vote and comment actions.
- Browser smoke tests need to click a genuine non-title area so they do not
  accidentally keep passing if the overlay stops working.

## Implementation plan

1. Add the overlay anchor and card-level hover/focus treatment in the shared
   portal board template.
2. Mirror the same behavior in the public roadmap template and the similar
   request cards on the detail page.
3. Update the portal browser smoke to click the card body on desktop.
4. Add render coverage for the new overlay markup.

## Verification

- Browser smoke against the public board and public roadmap pages.
- Portal render tests for the request and roadmap HTML templates.
- `pnpm exec vite build` for the console bundle.
- `go test ./internal/handlers/portal/...` or the targeted portal test subset.

## References

- [#222](https://github.com/Phixsura/attune/issues/222)
- [Public Roadmap From Workflow States](./2026-07-14-public-roadmap-from-workflow-states.md)
- [Public Voting Board](./2026-07-13-public-voting-board-mvp.md)
