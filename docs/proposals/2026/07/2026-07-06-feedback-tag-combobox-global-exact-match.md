<!-- markdownlint-disable MD013 -->

# Feedback tag combobox global exact-match guard

| Field | Value |
| --- | --- |
| **Issue** | [#62](https://github.com/Phixsura/attune/issues/62) |
| **Status** | Implemented |
| **Started** | 2026-07-06T14:15:00+08:00 |
| **Related** | [feedback tag UX improvements](./2026-06-14-feedback-tag-ux-improvements.md), [feedback manual tags](./2026-06-14-feedback-manual-tags.md), [browser E2E notes](../../../testing.md) |

## Problem

The feedback detail sheet intentionally hides tags that are already assigned to
the current feedback. That keeps the picker focused on available choices.

The combobox currently decides whether to show the inline "Create" action by
checking only the selectable list. When the user searches for a tag that exists
globally but is already assigned, the selectable list is empty and the combobox
offers to create a duplicate-looking tag name. The backend no-ops the second
assignment, so the UI promise is misleading.

## Goals

- Keep already-assigned tags out of the selectable list.
- Suppress inline creation when the search text exactly matches any known tag.
- Show a clearer "already added" empty state when the exact match is assigned to
  the current feedback.
- Keep batch add/remove combobox behavior unchanged.
- Cover the regression with a unit test and a browser recheck.

## Non-goals

- Change the tag assignment backend.
- Change the tag create endpoint contract.
- Add a new tag-management surface inside the combobox.

## Proposal

Teach `TagCombobox` to distinguish between the list of selectable tags and the
full set of known tags. The selectable list still drives the visible options,
but the full list drives the exact-match check that decides whether the create
action is allowed.

The feedback detail sheet passes the full tenant tag catalog for the exact-match
check while still filtering out already assigned tags from the selectable list.
Other combobox call sites can keep the default behavior by omitting the new
prop.

## Alternatives considered

### Show the assigned tag as a disabled option

Rejected. It complicates the generic combobox and changes the current detail
sheet flow more than necessary.

### Let the backend reject the duplicate assignment

Rejected. The backend already resolves tag names and dedupes assignments. The
problem is the misleading UI affordance, not backend correctness.

### Reinsert assigned tags into the selectable list

Rejected. The current filter-out behavior is intentional and keeps the picker
focused on actionable choices.

## Risks / tradeoffs

- The combobox still shows "no results" for an exact-match tag that is already
  assigned unless the exact-match helper is present. The explicit state keeps the
  user from interpreting an assigned tag as something they still need to create.
- The new prop adds a small amount of API surface to a shared component.

## Implementation plan

1. Add an optional full-catalog prop to `TagCombobox`.
2. Use the full catalog for exact-match create suppression.
3. Pass the full tag list from the feedback detail sheet.
4. Add a combobox test that searches an already assigned tag name and verifies
   no create action appears and the UI explains that the tag is already added.
5. Re-run the browser feedback flow and confirm the duplicate-looking create
   action is gone.

## Verification

- Unit tests cover the new exact-match guard.
- Browser E2E in
  [`console/e2e/accessibility/console-accessibility.spec.ts`](../../../console/e2e/accessibility/console-accessibility.spec.ts)
  confirms a searched, already assigned tag no longer offers duplicate
  creation, the empty state explains the tag is already added, and the add/remove
  roundtrip still works against the live detail sheet.

## References

- [`console/src/components/tag/tag-combobox.tsx`](../../../console/src/components/tag/tag-combobox.tsx)
- [`console/src/features/feedback/components/feedback-tags.tsx`](../../../console/src/features/feedback/components/feedback-tags.tsx)
- [`console/src/features/feedback/components/detail-sheet.tsx`](../../../console/src/features/feedback/components/detail-sheet.tsx)
- [`console/e2e/accessibility/console-accessibility.spec.ts`](../../../console/e2e/accessibility/console-accessibility.spec.ts)
