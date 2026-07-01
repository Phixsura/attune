# Console Accessibility Component Contracts

This document defines the Console accessibility contracts introduced for issue
[#171](https://github.com/Phixsura/attune/issues/171). These are engineering
contracts for Attune Console features. They are not a product accessibility
certification.

## Scope

These contracts apply to Console workbenches under `/console/**`, especially
feedback triage, terminal failures, audit log, API keys, MCP clients, GDPR, and
dead deliveries.

## Route Contract

- Every routed workbench sets a route-specific document title in the form
  `<page> - Attune Console`.
- Every routed workbench exposes exactly one visible level-one heading for the
  page task.
- The app shell provides a skip link to `#main-content`.
- The main content landmark has a stable `id="main-content"`.
- Desktop and mobile route states must not create document-level horizontal
  overflow. Dense regions may scroll inside their own container.
- Critical routes must tolerate forced-colors mode, WCAG text-spacing overrides,
  and 200% text sizing without document-level horizontal overflow.
- Browser E2E route sweeps must fail on unhandled Console API mocks, browser
  console errors, serious axe violations, and document overflow.

## Dialog And Sheet Contract

- Dialogs and sheets have an accessible title.
- Dialogs and sheets have an accessible description, or the implementation
  records why a description is intentionally omitted.
- The opening control has a stable accessible name.
- Initial focus is intentional and covered by a focused test for sensitive
  flows.
- Tab and Shift+Tab remain inside modal surfaces.
- Escape closes ordinary modal surfaces unless the workflow explicitly requires
  a blocking confirmation.
- Closing restores focus to the opener, or to a documented fallback when the
  opener no longer exists.
- Dialog content rendered through portals is included in axe checks by running
  the check against `document.body` when needed.

## Table Contract

- Use native table semantics for ordinary data tables.
- Do not use `role="grid"` unless the component implements the full grid
  keyboard interaction model.
- A table has a caption or nearby accessible context that names the data set.
- Row actions are real buttons or links with role/name coverage.
- A row must not be pointer-only. If row selection is supported, provide a real
  focusable control or a fully modeled keyboard activation path.
- Truncated identifiers keep full values available through accessible text,
  titles, or adjacent detail views.

## Filter Contract

- Filter controls expose their current state through native selected state,
  `aria-pressed`, or a semantically equivalent control.
- Icon-only filter controls have accessible names.
- Select triggers that rely on placeholder text also provide a durable label or
  `aria-label`.
- Filter changes do not move focus unexpectedly.
- Active filters can be removed by keyboard and announce their purpose through
  the remove control name.

## Disclosure Contract

- Disclosure triggers are real buttons.
- Expanded state is exposed with `aria-expanded`.
- `aria-controls` points to the same controlled region in both collapsed and
  expanded states when the region exists in the DOM.
- The controlled region has a stable id and is hidden, not renamed, while
  collapsed.
- Collapsing a region does not drop focus into removed content.

## Icon Button Contract

- Icon-only buttons have a stable accessible name through visible text,
  `aria-label`, or labelled content.
- Destructive icon actions expose the affected object in the accessible name
  when the object is visible in the current row or card.
- Icon buttons keep visible focus rings.
- Disabled icon buttons do not hide the reason for disabled state when that
  reason is essential to completing the workflow.

## Loading And Status Contract

- User-relevant loading text uses `role="status"` with polite live semantics.
- Mutation success and failure states are visible and available to assistive
  technology.
- Toasts use color pairs that meet WCAG AA contrast for normal text.
- Status messages do not rely on color alone.
- Long-running jobs keep a persistent page-level status when the result is
  security or compliance sensitive.

## Shortcut Contract

- Shortcut handlers ignore text inputs, textareas, selects, comboboxes, and
  editable regions.
- Visible shortcut affordances match implemented behavior.
- `aria-keyshortcuts` is used only when it accurately describes a supported
  shortcut.
- Shortcut behavior has tests for focus conflicts and Escape behavior.

## Mobile Contract

- Page grids and flex rows that contain dense content use `min-w-0`.
- Tables and code snippets scroll inside bounded containers.
- Touch targets used in high-frequency workflows are at least 24 by 24 CSS
  pixels, or have equivalent spacing.
- Mobile routes are part of the browser E2E accessibility gate.
