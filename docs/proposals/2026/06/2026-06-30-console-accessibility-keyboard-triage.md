# Console Accessibility and Keyboard Triage

| | |
|---|---|
| **Issue** | [#171](https://github.com/Phixsura/attune/issues/171) |
| **Status** | Implemented |
| **Started** | 2026-06-30T21:41:00+08:00 |
| **Related** | [#13](https://github.com/Phixsura/attune/issues/13) (Console Vitest infrastructure), [#90](https://github.com/Phixsura/attune/issues/90) (dimensions editor focus and labeling precedent), [#144](https://github.com/Phixsura/attune/issues/144) (Console IA and app shell), [#159](https://github.com/Phixsura/attune/issues/159) (terminal failure workbench), [#169](https://github.com/Phixsura/attune/issues/169) (admin command center), [#170](https://github.com/Phixsura/attune/issues/170) (saved views), [#202](https://github.com/Phixsura/attune/issues/202) (industry gap analysis) |

---

## Problem

The Console has become an operator workbench for sensitive enterprise flows:
feedback triage, audit review, outbox recovery, MCP governance, API-key issuance,
and GDPR operations. These surfaces already pass ordinary type, lint, and unit
checks, but the current test and component coverage does not prove the
accessibility behavior that issue #171 asks for:

- keyboard users can reach every critical action
- custom workbench controls expose useful names and state
- dialogs and sheets trap focus and restore it to the opener
- dense tables and pseudo-tables remain understandable to assistive technology
- regressions are caught by automated accessibility checks

The current codebase has strong foundations. Dialog, Sheet, Select, Checkbox, and
DropdownMenu are built on Radix primitives; Button, Input, Select, and Checkbox
share visible focus styles; Biome's recommended accessibility lint rules are
enabled; and most administrative tables use native table elements.

The gap is the layer above those primitives: page-level semantics, custom row
interactions, form-control labels inside dense workbenches, and workflow tests.
These are the exact places where static lint does not see the final rendered DOM.

Code-checked examples on `main`:

| Surface | Current risk |
|---|---|
| MCP clients | The client table selects rows through [`TableRow onClick`](../../../../console/src/features/mcp-clients/components/mcp-clients-page.tsx#L381-L385), but the row is not keyboard-focusable and has no keyboard activation path. Tool-policy controls also have unlabeled [checkbox](../../../../console/src/features/mcp-clients/components/mcp-clients-page.tsx#L661-L665), [RPM input](../../../../console/src/features/mcp-clients/components/mcp-clients-page.tsx#L676-L683), and [burst input](../../../../console/src/features/mcp-clients/components/mcp-clients-page.tsx#L686-L693). |
| Feedback | The semantic-search clear button is an icon-only native button with no accessible name; the test currently queries it by an empty name ([component](../../../../console/src/features/feedback/components/semantic-search-bar.tsx#L55-L61), [test](../../../../console/src/features/feedback/components/semantic-search.test.tsx#L32-L43)). |
| Audit log | Hover-only action buttons can receive keyboard focus while still visually hidden because the action cluster only responds to pointer hover ([entry actions](../../../../console/src/features/audit-log/components/audit-log-page.tsx#L1323-L1344)). |
| API keys | The create-dialog scope preset Select has a visual label but no explicit accessible label binding, and the effective-scope disclosure does not expose expanded state or controls ([dialog controls](../../../../console/src/features/api-keys/components/dialogs.tsx#L117-L186)). |
| GDPR | Request-type filter buttons communicate the active state visually, but do not expose `aria-pressed`; the step-up dialog should get explicit focus coverage ([filters and dialog](../../../../console/src/features/gdpr/components/gdpr-page.tsx#L737-L793)). |
| Shell | The app shell lacks a skip link and a stable main-content target, so keyboard users must traverse navigation before reaching page work ([main landmark](../../../../console/src/features/session/components/authed-shell.tsx#L227-L229)). |
| Test infrastructure | There is no `axe-core`, `vitest-axe`, `jest-axe`, or shared accessibility assertion helper in `console/`. |

## Goals / Non-goals

### Goals

- Make every critical issue-#171 page pass selected automated accessibility
  checks in Vitest.
- Ensure the main triage and administration actions are reachable by keyboard.
- Lock in focus behavior for the most important Dialog and Sheet flows:
  initial focus, focus containment, Escape dismissal, and focus restoration.
- Fix missing accessible names, labels, pressed/expanded state, table context,
  and visible focus states found in the audit.
- Add a small accessibility test helper that fits the current
  `renderWithProviders` testing model.
- Document keyboard affordances where the product already exposes shortcut
  behavior, especially the audit log.
- Keep changes local to Console UI, tests, and proposal/changelog material.

### Non-goals

- Do not replace Radix/shadcn primitives with a new design-system dependency.
- Do not convert native tables into ARIA grids unless the page implements a real
  grid interaction model with arrow-key cell navigation.
- Do not add Playwright, Cypress, or a browser E2E runner as part of this issue.
- Do not redesign the visual layout or navigation hierarchy beyond accessibility
  affordances needed by this issue.
- Do not implement saved views, command palette, real-time updates, or broader
  industry-gap items tracked outside #171.

### Acceptance matrix

| Surface | Required outcome |
|---|---|
| App shell | A keyboard user can skip primary navigation and land on `#main-content`; the shell-level accessibility smoke test passes. |
| Feedback list/detail | Feedback row open/select actions are keyboard reachable; the semantic-search clear action has a non-empty accessible name; the detail Sheet has title/description wiring and restores focus on close. |
| Terminal failure workbench | Included because latest `main` ships it as the terminal slice of feedback triage. Its sample, retry, remediation, and in-page jump actions are covered by role/name queries and an accessibility smoke test. |
| Audit log | `/`, J/K or Arrow keys, Enter, and Escape behavior remains covered; entry action buttons become visible on keyboard focus; the details Sheet has focus coverage. |
| Outbox dead queue | Status filters expose active state; retry actions have accessible names; the table has a clear accessible context. |
| MCP clients | A client can be selected without pointer input; tool-policy checkbox/RPM/burst controls have tool-specific accessible names; revoke dialog focus behavior is covered. |
| API keys | Scope preset Select is labelled; effective-scope disclosure exposes expanded state and controlled region; create, secret, and revoke dialogs have focus coverage. |
| GDPR | Request-type filters expose `aria-pressed`; step-up dialog initially focuses the password field and restores focus on close; request table has a clear accessible context. |
| Automation | Selected page/component states pass the shared `axe-core` helper with no untriaged violations. |

## Industry Findings

The research converges on a simple pattern: mature systems use proven primitives
for hard interaction mechanics, then add page-specific tests around the real
workflow.

| System / pattern | Finding | Attune decision |
|---|---|---|
| WAI-ARIA Authoring Practices | Dialogs must keep focus inside, Escape should close ordinary dialogs, and focus should return to the invoking control. Grids have a distinct arrow-key model and should not be used as a label for ordinary tables. | Keep native tables for ordinary data and test Dialog/Sheet focus behavior explicitly. |
| Radix UI | Dialog primitives compose focus scope, dismissable layers, hidden outside content, and restoration hooks. | Keep Radix Dialog/Sheet and test Attune's usage instead of hand-rolling focus traps. |
| React Aria | Modal and overlay behavior is best treated as infrastructure because screen-reader and mobile assistive-technology behavior has many edge cases. | Avoid custom modal infrastructure; use component-level tests for Attune flows. |
| GitHub Primer | Dense enterprise UI uses explicit role/name queries, arrow behavior where the component claims listbox/tree semantics, and direct tests for dialog focus and restoration. | Prefer role/name based tests and keyboard-driven assertions over snapshot checks. |
| IBM Carbon | Data tables stay native where possible; sortable or interactive controls carry explicit state and descriptions. | Add table captions or accessible labels where context is ambiguous; keep row actions as real buttons. |
| Fluent UI | Focus management is validated through keyboard tests across modal variants. | Add focused user-event tests for the critical Dialog and Sheet variants we ship. |
| USWDS | Public accessibility tests validate roles, trigger wiring, hidden outside content, and restored focus for modals. | Treat modal accessibility as an acceptance criterion, not a visual QA note. |
| GOV.UK Design System | Strong visible focus, skip links, and error/focus placement are first-class product requirements. | Add an app-shell skip link and avoid hidden keyboard-only action states. |
| Shopify Polaris | Trapping focus and managing disclosure are platform primitives, while product pages define user-facing control names. | Keep shared primitives lean; fix labels and states at the page/component boundary. |
| Atlassian Design System | Teams distinguish ordinary tables from dynamic tables and review accessibility at both component and product-flow levels. | Do not overuse `role="grid"`; audit each workbench by interaction model. |

## Proposal

### 1. Add a Console accessibility test helper

Add a small dev-only accessibility test layer around `axe-core` directly.

Dependency decision:

| Package | Type | Rationale |
|---|---|---|
| `axe-core` | devDependency | Current axe engine (`4.12.1` on npm at proposal update time), no production bundle impact, no Jest-specific matcher dependency, and enough API surface for a tiny Vitest helper. |

Rejected wrappers:

- `vitest-axe`: npm currently publishes `0.1.0`; it depends on an older
  `axe-core` range and adds little over calling `axe.run` ourselves.
- `jest-axe`: actively used, but it brings Jest matcher conventions into a
  Vitest-only test stack.

Planned files:

| File | Purpose |
|---|---|
| `console/src/testing/a11y.ts` | Export `expectNoA11yViolations(container)` and any shared axe configuration. |
| `console/package.json` / `console/pnpm-lock.yaml` | Add `axe-core` as a devDependency. |

The helper should be intentionally small:

- accept a rendered container
- call `axe.run` after the component is settled
- assert no violations
- keep rule overrides rare and documented at the assertion site
- default to no global rule suppressions
- use `document.body` for Radix portal states when the rendered container does
  not include the final Dialog or Sheet subtree

This fits the existing `renderWithProviders` setup, which already wraps i18n,
TanStack Query, and TooltipProvider and shims Radix-required DOM APIs in
`setup-tests.ts`.

### 2. Cover the issue-#171 pages with accessibility smoke tests

Add smoke tests that render the real page/component with the existing MSW
fixtures and run the shared axe helper.

Minimum coverage:

| Page / component | Test target |
|---|---|
| Feedback queue and terminal failure workbench | Feedback list, row controls, terminal workbench cluster actions, detail sheet trigger path. |
| Feedback detail sheet | Dialog role, title/description wiring, Escape dismissal, focus restoration. |
| Audit log | Filter console, audit entry list, detail sheet, shortcut semantics, hover action visibility under focus. |
| Outbox dead queue | Native table, retry action names, status filters. |
| MCP clients | Client selection, governance card, tool policies table, revoke dialog. |
| API keys | Create dialog, secret dialog, revoke dialog, scope preset and effective-scope disclosure. |
| GDPR | Subject operations, request center filters/table, step-up dialog. |
| App shell | Skip link and main landmark target. |

These tests should not assert that axe proves the product is fully accessible.
They are regression tripwires for missing names, invalid ARIA relationships,
missing dialog names, and invalid table/control composition.

Terminal failure workbench coverage does not expand #171 into a separate feature.
It is included only because `/feedback/terminal-failures` now reuses the feedback
triage surface and would otherwise become an untested keyboard path inside the
same operator job.

### 3. Fix MCP clients as the first functional blocker

MCP clients has the strongest keyboard failure in the audited scope. Fix it
before polishing lower-risk details.

Recommended changes:

- Replace clickable `TableRow` selection with a real button or link inside the
  first column, or add a properly modeled row-selection control.
- If the row itself remains interactive, provide `tabIndex`, role, keyboard
  activation for Enter and Space, `aria-selected`, and visible focus styling.
  Prefer the first-column button because it keeps native table semantics clean.
- Give tool-policy controls stable labels:
  - checkbox name should include the tool name and resulting effect
  - RPM input name should include tool name plus rate-limit meaning
  - burst input name should include tool name plus burst meaning
- Add keyboard tests that select a client and toggle a tool policy without using
  the pointer.

### 4. Fix missing names, state, and focus affordances

Apply small targeted fixes across the audited pages:

| Area | Change |
|---|---|
| Semantic search | Add an accessible name to the clear button and update the test to query by that name. |
| Audit log | Reveal hover action clusters on `group-focus-within`, not only `group-hover`; keep focus rings visible. |
| API keys | Bind the scope preset Select to a label through `id`/`aria-labelledby`; add `aria-expanded` and `aria-controls` to the effective-scopes disclosure. |
| GDPR | Add `aria-pressed` to request-type filter buttons; set initial focus in the step-up dialog to the password input. |
| Feedback filters | Add explicit labels or `aria-label` to filter Select triggers that currently rely on placeholder text. |
| Loading states | Give shared loading surfaces `role="status"` and polite live-region semantics where the text represents app state. |
| App shell | Add a skip link targeting `#main-content`; give `<main>` that id and, where practical, `aria-labelledby` through page headings. |
| Tables | Add captions or accessible table labels to dense administrative tables where surrounding headings do not clearly name the table. |

### 5. Test Dialog and Sheet focus behavior directly

Add focused interaction tests for the flows with the most user impact:

- feedback detail sheet
- audit details sheet
- API key create / secret / revoke dialogs
- GDPR step-up dialog
- MCP revoke dialog

Each test should verify the relevant subset:

- opener receives focus before opening
- opening moves focus to the intended initial control or an intentional fallback
- Tab does not leave the dialog/sheet
- Escape closes ordinary dialogs/sheets
- close returns focus to the opener or a documented fallback when the opener is
  gone

This should stay in Vitest + Testing Library + user-event. The goal is to lock
the behavior of our Radix composition, not to run full browser automation.

### 6. Expose keyboard affordances without adding tutorial copy

The audit log already implements operator shortcuts:

- `/` focuses local search
- `J` / `K` or ArrowDown / ArrowUp moves the focused event
- Enter opens details
- Escape closes details or clears local search

The implementation should make those affordances accessible and testable without
adding new visible instructional copy:

- preserve existing audit-log shortcut affordances only where they already belong
  to the product surface
- add `aria-keyshortcuts` on controls where it accurately describes the existing
  keyboard behavior
- use accessible names or descriptions when compressed visual text would not be
  enough for assistive technology
- ensure global shortcut handlers ignore text-entry and command-surface targets
  consistently

Do not add new product-wide shortcuts in this issue unless they are required to
make an existing action keyboard reachable.

### 7. Keep CI and dependency impact small

This issue should add dev-only test dependencies and no runtime dependencies.

Expected gate impact:

- `pnpm vitest run --coverage` will include the new accessibility tests
- `pnpm biome check` remains the static accessibility first line
- `pnpm arch` should stay clean because test helpers remain in `src/testing`
  and production imports continue to respect feature boundaries
- production bundle size should not change from the new test dependency

## Alternatives Considered

| Alternative | Decision | Reason |
|---|---|---|
| Add Playwright accessibility checks | Reject for this issue | The repository already has broad Vitest/MSW component coverage. Issue #171 can be covered with lower CI cost by testing rendered components in jsdom plus axe. Browser E2E is a separate test tier decision. |
| Replace Radix with React Aria or another component system | Reject | The current Radix/shadcn base already provides the hard dialog/select primitives. The defects are mostly in page composition and missing tests. |
| Use `role="grid"` for feedback and MCP tables | Reject by default | APG grid semantics require an arrow-key interaction contract. Attune's pages mostly need native tables, buttons, and checkboxes. Use grid only if the component actually implements grid behavior. |
| Use `vitest-axe` or `jest-axe` | Reject | Direct `axe-core` keeps the helper explicit, avoids a stale Vitest wrapper, and avoids importing Jest matcher conventions into the Vitest stack. |
| Rely only on Biome a11y lint | Reject | Biome is useful but cannot validate final Radix portal output, accessible names after translation, focus restoration, or hidden focusable controls. |
| Fix only the known manual findings without axe | Reject | The acceptance criteria explicitly asks for automated accessibility checks on selected pages. |

## Risks / Tradeoffs

| Risk | Mitigation |
|---|---|
| Axe produces noisy findings in jsdom for Radix portals or hidden content. | Keep the helper small, scope tests to stable rendered states, and document any rule suppression at the test site. |
| Focus tests become flaky because jsdom differs from a real browser. | Test only deterministic focus contracts that Radix and Testing Library support; avoid timing-sensitive animation assertions. |
| Over-labeling creates verbose screen-reader output in dense rows. | Prefer concise names tied to the action, such as "Select MCP client vscode-agent" or "Set RPM limit for search_feedback". |
| Table captions duplicate nearby headings. | Use visually hidden captions or `aria-label` only where the table's purpose is not already clear from a close heading. |
| Shortcut handlers conflict with input or menu interactions. | Centralize the guard logic and extend tests for text-entry, buttons, menus, checkboxes, and combobox triggers. |
| New test dependency needs supply-chain justification. | Add only `axe-core` as a devDependency; cite bundle cost, package activity, and rejected wrappers in the PR description. |

## Implementation Plan

1. Add the proposal and keep status `Proposed` until scope is accepted.
2. Mark the proposal `Accepted` before or with the implementation PR once the
   scope is confirmed.
3. Add `axe-core` infrastructure and a shared helper in `console/src/testing`.
4. Add failing accessibility/keyboard tests for the most concrete findings:
   MCP client keyboard selection, MCP tool policy labels, SemanticSearch clear
   button name, Audit hover actions under focus, GDPR filter `aria-pressed`, and
   API key disclosure state.
5. Implement the targeted component fixes.
6. Add Dialog/Sheet focus tests for feedback, audit log, API keys, GDPR, and MCP.
7. Add page-level axe smoke tests for the selected critical workbenches.
8. Add the shell skip link and shared loading live-region improvement.
9. Update `CHANGELOG.md` under `[Unreleased]` because the implementation changes
   user-facing Console behavior.
10. Move this proposal to `Implemented` once the local implementation and
    verification pass.

## Verification

Run the focused checks while iterating:

```sh
cd console
pnpm vitest run \
  src/routes/_authed.feedback.test.tsx \
  src/features/feedback/components/detail-sheet.test.tsx \
  src/features/feedback/components/semantic-search.test.tsx \
  src/features/feedback/components/terminal-failure-workbench.test.tsx \
  src/features/audit-log/components/audit-log-page.test.tsx \
  src/features/audit-log/components/evidence-export-dialog.test.tsx \
  src/features/outbox-dead/components/dead-deliveries-page.test.tsx \
  src/features/api-keys/components/api-keys-page.test.tsx \
  src/features/api-keys/components/dialogs.test.tsx \
  src/features/gdpr/components/gdpr-page.test.tsx \
  src/features/mcp-clients/components/mcp-clients-page.test.tsx
```

Run the Console gates:

```sh
cd console
pnpm tsc -b --noEmit
pnpm biome check
pnpm exec vite build
pnpm vitest run --coverage
pnpm arch
```

Run repository-level checks before completion. These do not replace the Console
build and architecture gates above; both sets should be cited in the PR:

```sh
go vet ./...
go test -short ./...
make ci-check
```

The implementation PR should cite the actual command output in its description.

Local verification on 2026-06-30:

- `pnpm tsc -b --noEmit` passed.
- `pnpm biome check` passed.
- Focused Vitest for audit log, feedback detail, terminal failure workbench,
  API keys, outbox dead queue, GDPR, and MCP clients passed: 7 files, 65 tests.
- `pnpm exec vite build` passed, with the existing large-chunk warning.
- `pnpm vitest run --coverage` passed: 94 files, 850 tests; statement
  coverage 77.14%.
- `pnpm arch` passed with no dependency violations when run with Node v24.14.0:
  371 modules and 1707 dependencies cruised. dependency-cruiser rejects the
  local Homebrew Node v23.11.0 runtime.
- `bash scripts/lint-artifacts.sh --strict` passed hard checks A/B, with only
  pre-existing advisory Check C findings.
- `git diff --check` passed.
- Local browser E2E smoke against `console/dist` served by a same-origin mock API
  passed with zero browser console errors. It covered the shell skip-link DOM
  contract; API key create/secret/revoke focus restoration; MCP client
  selection, tool-policy labels, and revoke focus restoration; GDPR subject
  export step-up focus, request table, and filter state; audit-log local-search
  shortcut metadata and detail focus restoration; feedback detail-sheet focus
  restoration; and outbox dead-queue table, filter, and retry action labels. The
  browser-control layer did not reliably synthesize global Tab or Enter
  activation, so those keyboard activation contracts remain covered by the
  focused Vitest + user-event tests.
- Additional visible in-app browser control on 2026-07-01 passed after aligning
  local mock payloads to the generated Console contracts. Actions were driven in
  the browser: dead-delivery status filtering and retry-label checks, API key
  create/secret/revoke dialogs, MCP client selection/tool-policy toggle/revoke
  dialog, GDPR export filter and step-up dialog, audit-log `/` shortcut plus
  detail dialog, and feedback detail sheet open/Escape-close focus restoration.
  Each checked path completed with zero browser console errors.
- Mobile viewport browser regression on 2026-07-01 found GDPR request-center
  horizontal overflow caused by grid item min-content sizing. The implementation
  now lets the GDPR grid columns shrink before dense table and card content
  scrolls, and the rebuilt Console passed mobile GDPR checks for document width,
  step-up initial focus, Escape dismissal focus restoration, and zero browser
  console errors.
- Continued in-app browser testing on 2026-07-01 covered the rebuilt Console
  through a same-origin mock API and static asset server with the `/console`
  asset prefix mapped to `console/dist`. Desktop browser flows passed for
  feedback detail Sheet focus restoration, audit-log detail Sheet focus
  restoration and shortcut metadata, API-key create/secret/revoke dialogs, MCP
  client selection and tool-policy labels, outbox status filtering and retry
  labels, and GDPR step-up/filter state. A mobile viewport sweep across
  feedback, API keys, MCP clients, audit log, GDPR, and outbox found no document
  horizontal overflow and zero browser console errors; the mobile nav Sheet also
  opened, exposed the primary navigation label, navigated to API keys, and
  closed after navigation.
- Additional focused browser testing on 2026-07-01 verified API-key effective
  scope disclosure state and controlled-region wiring, MCP session termination,
  refresh-grant revocation, MCP client revoke-dialog Escape focus restoration,
  MCP client revoke confirmation, and mobile GDPR step-up verification followed
  by export without reopening the step-up dialog. The browser-control layer
  still did not synthesize a reliable global Tab sequence for the hidden skip
  link, so that keyboard activation path remains covered by the focused
  Vitest/user-event shell test while the browser run verifies the target
  `main#main-content` contract.
- `go vet ./...`, `go build ./...`, and `go test -short ./...` passed.
- `make ci-check` passed. It ran Go vet/build/race unit tests, golangci-lint,
  lizard, logging/artifact/raw-pointer/error-code/integration-layout checks,
  jscpd, Console tsc/biome/vitest, and skipped TruffleHog because it is not
  installed locally.

Final Console verification on 2026-07-01 after the browser-stress expansion:

- `pnpm biome check` passed.
- `pnpm exec tsc -b --noEmit --pretty false` passed.
- `pnpm exec vite build` passed, with the existing large-chunk warning.
- `pnpm vitest run --coverage` passed: 94 files, 851 tests; statement coverage
  77.22%.
- `pnpm arch` passed with no dependency violations when run with Node v22.22.3:
  373 modules and 1716 dependencies cruised. dependency-cruiser rejects the
  local Homebrew Node v23.11.0 runtime.
- `pnpm exec playwright test --config playwright.config.ts --reporter=list
  --workers=1` passed: 62 tests across `chromium-desktop` and
  `chromium-mobile`, including API-key revoke success/error coverage and
  toast status checks against the Sonner `Notifications alt+T` live region.
- In-app browser testing against `http://127.0.0.1:8090` with the built
  `console/dist` assets passed for feedback/detail overflow, long GDPR subject
  input containment, API-key dialog open/cancel cycles, unnamed
  `aria-controls` controls, and browser console errors.
- `git diff --check` passed.
- `scripts/lint-artifacts.sh --strict` passed hard checks A/B, with only
  pre-existing advisory Check C findings.

## References

- [Issue #171: `feat(console): accessibility and keyboard triage pass for critical workbenches`](https://github.com/Phixsura/attune/issues/171)
- [Issue #202: industry gap closure meta issue](https://github.com/Phixsura/attune/issues/202)
- [Industry gap analysis pinned at `9ab7ef98`](https://github.com/Phixsura/attune/blob/9ab7ef98/docs/analysis/2026-06-29-industry-gap-analysis.md)
- [WAI-ARIA APG: Dialog modal pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/)
- [WAI-ARIA APG: Grid pattern](https://www.w3.org/WAI/ARIA/apg/patterns/grid/)
- [Radix UI Dialog](https://www.radix-ui.com/primitives/docs/components/dialog)
- [React Aria accessibility introduction](https://react-spectrum.adobe.com/react-aria/accessibility.html)
- [GitHub Primer accessibility](https://primer.style/accessibility/)
- [IBM Carbon accessibility](https://carbondesignsystem.com/guidelines/accessibility/overview/)
- [Fluent UI accessibility](https://react.fluentui.dev/?path=/docs/concepts-developer-accessibility--docs)
- [USWDS accessibility](https://designsystem.digital.gov/documentation/accessibility/)
- [GOV.UK Design System accessibility](https://design-system.service.gov.uk/accessibility/)
- [Shopify Polaris accessibility](https://polaris.shopify.com/accessibility)
- [Atlassian Design System accessibility](https://atlassian.design/foundations/accessibility/)
