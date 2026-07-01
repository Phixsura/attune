# Console Accessibility Industry Alignment

| | |
|---|---|
| **Issue** | [#171](https://github.com/Phixsura/attune/issues/171) |
| **Status** | Accepted |
| **Started** | 2026-07-01T01:29:08+08:00 |
| **Related** | [Baseline #171 proposal](../06/2026-06-30-console-accessibility-keyboard-triage.md), [#202](https://github.com/Phixsura/attune/issues/202) (industry gap analysis) |

---

## Problem

The baseline #171 implementation proposal closes concrete keyboard and accessible
name defects across the Console's critical workbenches. That is necessary, but
not sufficient for parity with mature product and design-system programs.

Industry-leading systems do not treat accessibility as a one-time component
patch. They combine:

- standards traceability against WCAG and WAI-ARIA Authoring Practices
- component-level accessibility contracts
- browser-level keyboard and assistive-technology-sensitive regression tests
- responsive, adaptive-color, text-spacing, and text-resize regression checks
- explicit manual screen-reader coverage where automation cannot prove behavior
- CI gates that keep those checks running after the #171 implementation lands

The current #171 branch has strong foundations: page-level `axe-core` smoke
tests in Vitest, explicit labels and state for dense workbench controls, Dialog
and Sheet focus restoration tests, table captions, an app-shell skip link,
mobile overflow fixes found through in-app browser testing, and repository-level
verification paths through `make ci-check`.

The remaining gap is durability. Without a browser E2E gate, WCAG mapping,
assistive-technology matrix, and reusable Console accessibility contracts, the
same class of regression can return when the next admin page, dense table,
mobile layout, or dialog variant ships.

This proposal raises #171 from a page-patch bar to an industry-aligned
completion bar.

## Goals / Non-goals

### Goals

- Add a real-browser Console accessibility gate to CI.
- Verify critical Console workflows in Chromium at desktop and mobile
  viewports.
- Verify critical Console routes in forced-colors mode, with WCAG text-spacing
  overrides, and with 200% text sizing so resize/reflow regressions are caught
  before manual release review.
- Run `axe-core` against the final browser DOM, including Radix portals and
  routed page states that jsdom cannot fully model.
- Fail the browser gate on untriaged browser console errors, document-level
  horizontal overflow, broken skip-link wiring, unlabeled critical tables, and
  invalid Dialog/Sheet focus contracts.
- Add a WCAG 2.2 A/AA traceability document for the Console surfaces covered by
  #171, with every A/AA criterion classified and evidenced.
- Add an assistive-technology test matrix. The matrix must have dated passing
  rows before this proposal can move to `Implemented` or support a
  screen-reader conformance claim.
- Promote repeated page fixes into reusable Console accessibility contracts for
  tables, dialogs, disclosures, filters, icon buttons, loading states, and
  shortcut-enabled workbenches.
- Add a manual/scheduled supplemental browser workflow for the CI-safe Firefox
  and WebKit desktop subset, while keeping Edge available in the local
  supplemental sweep.
- Keep all work scoped to the Console, test infrastructure, documentation, and
  #171 verification evidence.

### Non-goals

- Do not claim full product accessibility certification or legal conformance
  from automation alone.
- Do not make product roadmap items from the broader industry gap analysis part
  of #171, such as public voting portals, NPS/CSAT, Jira sync, Slack ingest, or
  analytics dashboards.
- Do not replace Radix/shadcn primitives with another design-system framework.
- Do not convert native tables into ARIA grids unless the page implements the
  full grid keyboard interaction model.
- Do not require every browser engine in the default PR CI gate for #171. Use
  Chromium for the required automated browser gate; use the supplemental
  workflow for Firefox and WebKit, the local supplemental script for Edge,
  Firefox, and WebKit desktop sweeps, and the assistive-technology matrix for
  screen-reader evidence.
- Do not make visual snapshot review the only accessibility signal. Screenshots
  are useful for focus and overflow regressions, but role/name/state assertions
  remain the primary contract.

## Industry Alignment Findings

| Source | Industry practice | #171 alignment decision |
|---|---|---|
| WCAG 2.2 | Track testable success criteria, including keyboard access, focus order, focus visibility, target size, status messages, and accessible authentication. | Add a Console WCAG 2.2 A/AA mapping with every A/AA criterion classified as automated pass, manual pass, or not applicable with rationale. |
| WAI-ARIA APG Dialog | Modal dialogs keep focus contained, close with Escape where appropriate, and return focus to the invoking element unless workflow context requires another target. | Make Dialog and Sheet focus contracts part of browser E2E and component tests. |
| WAI-ARIA APG Grid | Grids require a distinct arrow-key interaction model and are not a synonym for dense tables. | Keep native tables for ordinary data views; add captions, row actions, and labels instead of overusing `role="grid"`. |
| GitHub Primer | Engineering accessibility checks are role/name/state oriented and should be included before merge. | Keep Testing Library role/name tests and add browser assertions for final routed DOM. |
| IBM Carbon | Component accessibility status is explicit, with automated checks and screen-reader testing as separate signals. | Add component contracts plus an assistive-technology matrix instead of relying on `axe-core` alone. |
| GOV.UK Design System | Publicly tracks WCAG 2.2 AA failures and assigns concrete responsibility to unresolved accessibility items. | Add a living Console WCAG traceability document for #171 scope. |
| USWDS | Accessibility guidance treats semantic structure, keyboard behavior, and page tests as design-system responsibilities. | Move repeated page patterns into reusable Console contracts. |
| Atlassian Design System | Accessibility is validated across product flows and cannot be reduced to static linting. | Add end-to-end workflow coverage for the critical workbenches. |
| Shopify Polaris | Product pages still own meaningful labels, disclosure wording, and workflow context even when primitives are accessible. | Keep labels and names close to feature code while checking them centrally. |
| React Aria / Radix UI | Overlay and modal behavior are infrastructure-level concerns because focus, portals, and assistive technology have edge cases. | Keep Radix primitives, but test Attune's final composition in a real browser. |

## Proposal

### 1. Add a Console browser accessibility gate

Add Playwright-based E2E coverage under `console/e2e/accessibility/`.

Planned files:

| File | Purpose |
|---|---|
| `console/playwright.config.ts` | Browser E2E configuration, Chromium desktop/mobile projects, opt-in supplemental Edge/Firefox/WebKit desktop projects, managed preview server, trace on failure. |
| `console/e2e/accessibility/console-accessibility.spec.ts` | Browser-level route sweep, axe assertions, overflow checks, and critical workflow tests. |
| `console/src/testing/fixtures/console-a11y-fixtures.ts` | Shared typed fixture payloads reused by Vitest and Playwright route mocks. |
| `console/e2e/accessibility/route-mocks.ts` | Playwright request routing built from the shared typed fixtures. |
| `console/e2e/accessibility/helpers.ts` | Shared assertions for axe, console errors, focus restoration, skip link, table names, and overflow. |
| `console/package.json` | Add default and supplemental `test:e2e:a11y` scripts. |
| `.github/workflows/ci.yml` | Run the browser gate in the existing Console job after `vite build` and before `arch`. |
| `.github/workflows/console-a11y-supplemental.yml` | Run the CI-safe Firefox and WebKit supplemental sweep manually and on a weekly schedule. |

Dependency decision:

| Dependency | Type | Rationale |
|---|---|---|
| `@playwright/test` | devDependency | Provides the standard browser E2E runner, trace artifacts, viewport projects, request routing, and accessible locators. It is test-only and does not enter the production bundle. |

Local setup should install only Chromium:

```sh
cd console
pnpm exec playwright install chromium
```

CI should use the Linux dependency installer:

```sh
cd console
pnpm exec playwright install --with-deps chromium
```

The browser gate should run against the built Console rather than the dev server:

```sh
pnpm exec vite build
pnpm exec vite preview --host 127.0.0.1 --port 4173
```

This keeps the E2E surface close to the shipped asset graph and catches
production-only routing, asset, and CSS issues.

`playwright.config.ts` must own server startup so local and CI verification do
not depend on a manually started process:

```ts
webServer: {
  command: 'pnpm exec vite preview --host 127.0.0.1 --port 4173',
  url: 'http://127.0.0.1:4173/console/',
  reuseExistingServer: !process.env.CI,
  timeout: 60_000,
}
```

The E2E script must use the managed preview server:

```json
{
  "test:e2e:a11y": "playwright test --config playwright.config.ts",
  "test:e2e:a11y:headed": "playwright test --config playwright.config.ts --headed",
  "test:e2e:a11y:supplemental": "ATTUNE_CONSOLE_E2E_SUPPLEMENTAL=1 playwright test --config playwright.config.ts --project=edge-desktop --project=firefox-desktop --project=webkit-desktop",
  "test:e2e:a11y:supplemental:ci": "ATTUNE_CONSOLE_E2E_SUPPLEMENTAL=1 ATTUNE_CONSOLE_E2E_SUPPLEMENTAL_PROJECTS=firefox,webkit playwright test --config playwright.config.ts --project=firefox-desktop --project=webkit-desktop",
  "test:e2e:a11y:supplemental:headed": "ATTUNE_CONSOLE_E2E_SUPPLEMENTAL=1 playwright test --config playwright.config.ts --project=edge-desktop --project=firefox-desktop --project=webkit-desktop --headed"
}
```

The supplemental projects are not part of the default #171 CI gate. They exist
for release checks and browser-parity claims where Edge, Firefox, and WebKit
desktop behavior should be exercised in addition to Chromium. The scheduled and
manual supplemental workflow runs Firefox and WebKit because those browsers can
be installed deterministically on GitHub's Linux runners; Edge remains part of
the local supplemental sweep where the channel is installed.

### 2. Cover the critical #171 workbenches in Chromium

Minimum route matrix:

| Route | Browser assertions |
|---|---|
| `/console/feedback` | Search, row action naming, detail Sheet focus restoration, zero serious axe violations, no overflow. |
| `/console/feedback/terminal-failures` | Terminal failure actions reachable by role/name, in-page jump target remains reachable, no overflow. |
| `/console/administration/audit-log` | `/` search shortcut metadata, entry detail Sheet, Escape close, focus restoration, action buttons visible on focus. |
| `/console/integrations/api-keys` | Create, secret, and revoke dialogs; scope disclosure `aria-expanded` / `aria-controls`; table name. |
| `/console/mcp-clients` | Keyboard-selectable client control, tool policy labels, session termination, refresh grant revocation, revoke dialog focus behavior. |
| `/console/administration/gdpr` | Request filters `aria-pressed`, request table caption/name, step-up dialog initial focus and restoration, mobile overflow guard. |
| `/console/administration/dead-deliveries` | Status filters `aria-pressed`, retry action names, table name, no overflow. |

Redirect and navigation aliases are part of the gate:

| Entry point | Expected result |
|---|---|
| `/console/api-keys` | Redirects to `/console/integrations/api-keys` and preserves the API-key page contracts. |
| `/console/outbox-dead` | Redirects to `/console/administration/dead-deliveries` and preserves the dead-delivery page contracts. |
| Shell navigation for MCP clients | Lands on `/console/mcp-clients` and preserves the MCP page contracts. |
| Shell navigation for dead deliveries | Lands on the canonical dead-delivery route after redirect and preserves the page contracts. |

Viewports:

| Project | Viewport | Purpose |
|---|---:|---|
| `chromium-desktop` | 1365 x 768 | Small laptop, dense admin layout, focus visibility. |
| `chromium-mobile` | 390 x 844 | Mobile nav, table/card scroll containment, target spacing, no document overflow. |

The mobile project should assert:

```ts
document.documentElement.scrollWidth <= document.documentElement.clientWidth + 1
```

for every covered route after the route has settled.

Adaptive-mode checks are part of the default browser gate:

| Mode | Assertion |
|---|---|
| Forced colors | Critical routes keep accessible structure, avoid document overflow, and remain axe-clean under `forced-colors: active`. |
| WCAG text spacing | Critical routes tolerate 1.5 line height, 0.12em letter spacing, 0.16em word spacing, and 2em paragraph spacing without axe or overflow regressions. |
| 200% text sizing | Critical routes keep document-level horizontal overflow at zero while dense regions shrink, wrap, or scroll inside their own containers. |

### 3. Run axe in the real browser DOM

Reuse the existing `axe-core` dev dependency by injecting the package into the
Playwright page.

Default rule policy:

- no global suppressions
- fail on any `critical` or `serious` violation
- fail on `moderate` violations unless the spec documents a narrow,
  issue-linked allowlist
- include the route, viewport, violation id, impact, target, and summary in the
  assertion output
- keep route-specific suppressions close to the route test, not in a global
  helper
- attach the full violation payload to Playwright artifacts on failure

The browser `axe-core` gate complements, but does not replace, the existing
Vitest helper. Vitest remains faster and covers individual component states;
Playwright validates routed composition, real focus, CSS, portals, and viewport
behavior.

Browser console policy:

- fail on every untriaged page `console.error`
- fail on failed network responses for mocked `/fb/v1/console/**` endpoints
- allow a message only through an issue-linked allowlist near the assertion
- include console and network failure logs in Playwright artifacts

### 4. Share typed fixtures between Vitest and Playwright

The browser gate must not grow a second set of contract-shaped mock data that
drifts from the component tests.

Add shared fixture builders under `console/src/testing/fixtures/` and type them
against generated proto/client shapes where those shapes exist. Playwright route
handlers should import the fixture builders and adapt them to request routing;
Vitest/MSW tests can import the same builders when they need the same state.

Minimum fixture coverage:

| Area | Required mocked endpoints |
|---|---|
| Session | `/fb/v1/console/me`, auth providers where login redirection is checked. |
| Feedback | feedback list, feedback detail, feedback audit timeline, terminal-failure workbench, retry enrichment. |
| Audit log | audit-log list, evidence export start/status/download where the route touches export controls. |
| API keys | list keys, list scopes, list presets, create key, revoke key. |
| MCP clients | list clients, client detail, update governance, update tool policies, revoke client, revoke session, revoke grant. |
| GDPR | operations, request list, step-up verify, export request, export status, export download, revoke export, delete request, cancel request. |
| Dead deliveries | delivery list, retry delivery. |

Unhandled `/fb/v1/console/**` requests must fail the E2E test with the method
and URL in the assertion output.

### 5. Add reusable Console accessibility contracts

Add a short engineering guide:

`docs/accessibility/console-component-contracts.md`

It should define the minimum contract for:

| Pattern | Contract |
|---|---|
| Dialog / Sheet | Has a title, has a description or intentional equivalent, initial focus is intentional, Tab stays inside, Escape behavior is documented, close restores focus. |
| Dense table | Uses native table semantics by default, has a nearby heading or caption, row actions are real buttons/links, no row-only pointer activation. |
| Filter group | Uses button or tab semantics consistently; current state uses `aria-pressed`, selected tab state, or selected option state. |
| Disclosure | Uses a real button, exposes `aria-expanded`, and keeps `aria-controls` wired to a stable controlled region across collapsed and expanded states. |
| Icon button | Has a stable accessible name through visible text, `aria-label`, or labelled content. |
| Loading / async status | Uses `role="status"` and polite live semantics when the text represents user-relevant app state. |
| Shortcut surface | Exposes accurate `aria-keyshortcuts`, ignores text-entry targets, and has tests for conflicts. |
| Mobile overflow | Page-level grids use `min-w-0`; dense tables scroll inside their own container, not at document level. |
| Page title | Each routed workbench sets a route-specific document title that identifies the current page. |

The goal is not a large design-system rewrite. The goal is to make the rules
that #171 already fixed reusable for the next feature.

### 6. Add WCAG 2.2 A/AA traceability for Console scope

Add:

`docs/accessibility/console-wcag-2.2-aa.md`

The document must track every WCAG 2.2 A/AA criterion for the #171 Console
scope. It must not include AAA criteria in the required table.

| Criterion | Status | Evidence | Owner |
|---|---|---|---|
| 1.3.1 Info and Relationships | Automated pass + manual pass | Native tables, captions, headings, form labels, axe results | Console |
| 1.4.10 Reflow | Automated pass + manual pass | Mobile no-overflow route sweep plus manual zoom/reflow check | Console |
| 2.1.1 Keyboard | Automated pass + manual pass | Vitest keyboard tests + Playwright route workflows | Console |
| 2.1.2 No Keyboard Trap | Automated pass + manual pass | Dialog/Sheet Tab and Escape tests | Console |
| 2.4.1 Bypass Blocks | Automated pass | App-shell skip link + browser DOM contract | Console |
| 2.4.2 Page Titled | Automated pass | Route-specific document title assertion | Console |
| 2.4.3 Focus Order | Automated pass + manual pass | Dialog opener restoration and route focus sweep | Console |
| 2.4.6 Headings and Labels | Automated pass + manual pass | Role/name assertions and manual screen-reader pass | Console |
| 2.4.7 Focus Visible | Automated pass + manual pass | Browser focus checks plus failure screenshots | Console |
| 2.4.11 Focus Not Obscured (Minimum) | Manual pass | Desktop and mobile focus sweep | Console |
| 2.5.3 Label in Name | Automated pass + manual pass | Role/name assertions on visible text controls | Console |
| 2.5.8 Target Size (Minimum) | Manual pass | Mobile route sweep and touch-target review | Console |
| 3.3.1 Error Identification | Automated pass + manual pass | Form and mutation error paths identify failures by text | Console |
| 3.3.2 Labels or Instructions | Automated pass + manual pass | Form labels, descriptions, and step-up instructions | Console |
| 3.3.8 Accessible Authentication (Minimum) | Manual pass | Login and GDPR step-up checks | Console |
| 4.1.2 Name, Role, Value | Automated pass | Vitest role/name + browser axe | Console |
| 4.1.3 Status Messages | Automated pass + manual pass | Loading/status live-region and toast/status announcement checks | Console |

The final document must include all A/AA criteria, including criteria that are
not represented in the sample rows above. Allowed statuses:

- `Automated pass`
- `Automated pass + manual pass`
- `Manual checklist required`
- `Not applicable`

`Not applicable` requires a one-sentence rationale. A criterion with no evidence
blocks this proposal from moving to `Implemented`.

### 7. Add an assistive-technology matrix

Add:

`docs/accessibility/console-assistive-technology-matrix.md`

Required matrix:

| Platform | Browser | Assistive technology | Required workflows |
|---|---|---|---|
| macOS | Safari or Chrome | VoiceOver | Skip link, GDPR step-up, API key dialogs, audit detail Sheet. |
| Windows | Chrome or Edge | NVDA | Feedback triage, MCP tool policy controls, outbox retry, audit shortcuts. |
| Windows | Chrome or Edge | JAWS | Dense table navigation, API key dialogs, MCP revoke dialog, audit detail Sheet. |
| iOS | Safari | VoiceOver | Mobile nav, GDPR request center, API key dialog close/restore. |
| Android | Chrome | TalkBack | Mobile nav, GDPR request center, dead-delivery retry, MCP client detail. |

Each assistive-technology row must record:

- date tested
- tester
- build or commit SHA
- workflow result
- unresolved defects with issue links
- relevant notes about browser, OS, and assistive-technology version

Automation cannot replace this matrix. Every row must be tested and pass before
this proposal moves to `Implemented` or supports a screen-reader conformance
claim. A failed row requires a same-branch fix and retest evidence. An unrecorded
row blocks that status change.

### 8. Add a release checklist

Add:

`docs/accessibility/console-release-checklist.md`

The checklist must combine repository gates, default browser automation,
supplemental desktop-engine runs, artifact hygiene, and the manual
assistive-technology matrix into one release-facing acceptance path. It should
make clear that supplemental browser automation can support browser-parity
claims, while manual AT rows remain required for screen-reader conformance
claims.

### 9. Add status-message and error-path coverage

The browser route matrix must include at least one success status and one error
or validation status per high-risk workflow family:

| Area | Required status evidence |
|---|---|
| API keys | create success and revoke success are announced or visibly reported. |
| GDPR | missing step-up or validation error is identified, and step-up success is reported. |
| MCP clients | revoke/session/grant success is reported; failed mutation surfaces a readable error. |
| Dead deliveries | retry success is reported; retry conflict/error surfaces a readable error. |
| Feedback | retry-enrichment success or failure is reported in an accessible status surface. |

Where a status uses a toast, the test should assert that the toast container is
available to assistive technology or record the gap in the WCAG mapping as a
blocking defect.

### 10. Add visual focus and responsive failure artifacts

For the #171 browser gate, use Playwright trace, screenshot, and video
artifacts only on failure. Do not require committed screenshot baselines yet.

Failure artifacts should make these defects easy to debug:

- focus is invisible or hidden behind sticky UI
- mobile document overflow reappears
- dialogs open with the wrong initial focus
- Escape closes to `<body>` instead of the opener
- Radix portal content loses its accessible title or description

Committed visual snapshots are outside #171 unless the failure artifacts prove
insufficient for diagnosing focus regressions.

### 11. Update #171 verification and changelog

The baseline #171 proposal should remain as the implementation history for the
page-level work. This proposal extends completion criteria.

Before marking #171 complete, cite:

- existing Console static and unit gates
- new Playwright browser accessibility gate
- complete WCAG 2.2 A/AA traceability document
- the assistive-technology matrix, with any unrecorded rows called out plainly
- manual in-app browser evidence where it covers behavior CI cannot

Because this changes test infrastructure and may add user-facing accessibility
behavior, keep the existing `CHANGELOG.md` entry and update it if this proposal
introduces additional Console behavior changes.

## Alternatives Considered

| Alternative | Decision | Reason |
|---|---|---|
| Keep Playwright out of #171 | Rejected | The user asked to finish industry alignment in this issue. Browser-level regression is the largest remaining gap. |
| Use only the in-app browser manual run | Rejected | Manual browser evidence is valuable, but it does not prevent future regressions in CI. |
| Use Lighthouse only | Rejected | Lighthouse is useful for page-level audits but weaker for workflow-specific Dialog, Sheet, disclosure, and dense table contracts. |
| Add Storybook and Chromatic in #171 | Rejected | Useful for component visual review, but heavier than the browser workflow gate and less aligned to routed Console behavior. |
| Run Chromium, Firefox, and WebKit in default PR CI for #171 | Scoped out | Broader engine coverage is valuable, but the required #171 CI gate should stay fast and stable. Provide a manual/scheduled Firefox/WebKit workflow and an opt-in local Edge/Firefox/WebKit Playwright script for release checks and browser-parity claims. |
| Convert tables to ARIA grids | Reject by default | APG grid semantics require an arrow-key cell model. Attune mostly needs native tables with real controls. |
| Create a large design-system package in #171 | Scoped out | The issue needs documented contracts and targeted helpers, not package reorganization. |

## Risks / Tradeoffs

| Risk | Mitigation |
|---|---|
| Browser E2E increases CI time. | Run one Chromium browser with two viewports and deterministic mock data; keep specs focused on #171 workbenches. |
| Playwright dependency adds supply-chain surface. | Keep it dev-only, justify it in the PR, and use it only inside Console test scripts. |
| Mocked API payloads drift from generated contracts. | Use shared typed fixture builders for Vitest and Playwright; fail unhandled Console API requests. |
| Axe findings can be noisy. | Fail hard on serious/critical, require issue-linked allowlists for any moderate exception, and keep suppressions local. |
| Manual screen-reader matrix requires access to multiple platforms. | Keep this proposal out of `Implemented` until every matrix row is tested and passing. Do not substitute browser automation for screen-reader evidence. |
| Browser tests can become brittle. | Use role/name/data-contract locators, avoid timing assumptions, and keep page states deterministic. |
| Visual focus regressions may not be fully captured by role assertions. | Store trace and screenshots on failure; committed snapshots are outside #171 unless failure artifacts prove insufficient. |

## Implementation Plan

1. Add this accepted proposal as the #171 industry-alignment scope.
2. Add `@playwright/test` and the Console E2E scripts.
3. Add Playwright configuration with Chromium desktop and mobile projects plus
   opt-in supplemental Edge, Firefox, and WebKit desktop projects.
4. Add supplemental project filtering so CI can run the Firefox/WebKit subset
   without requiring an ambient Edge channel.
5. Add shared typed fixture builders for Console E2E and Vitest reuse.
6. Build deterministic mock API routing for the seven #171 Console routes.
7. Add browser helpers for axe, console-error failure, network failure,
   skip-link target checks, table naming, and Dialog/Sheet focus restoration.
8. Add canonical route coverage under `console/e2e/accessibility/`.
9. Add status-message assertions for the high-risk workflows covered by the
   browser gate.
10. Add adaptive browser-mode coverage for forced colors, WCAG text spacing,
    and 200% text sizing.
11. Add `docs/accessibility/console-component-contracts.md`.
12. Add `docs/accessibility/console-wcag-2.2-aa.md`.
13. Add `docs/accessibility/console-assistive-technology-matrix.md`.
14. Add `docs/accessibility/console-release-checklist.md`.
15. Record the required assistive-technology matrix before moving this proposal
    to `Implemented`.
16. Wire `pnpm exec playwright install --with-deps chromium` and
    `pnpm test:e2e:a11y` into the Console CI job after build.
17. Add a manual/scheduled supplemental workflow for Firefox and WebKit desktop
    browser sweeps.
18. Run local verification and update the baseline #171 proposal's verification
    section with the new browser gate results.
19. Update `CHANGELOG.md` if implementation introduces additional user-facing
    behavior beyond test and documentation infrastructure.
20. Move this proposal to `Implemented` after the browser gate, docs,
    repository checks, and required manual assistive-technology rows pass.

## Verification

Required local commands:

```sh
cd console
pnpm install --frozen-lockfile
pnpm exec playwright install chromium
pnpm exec vite build
pnpm tsc -b --noEmit
pnpm biome check
pnpm vitest run --coverage
pnpm arch
pnpm test:e2e:a11y
pnpm test:e2e:a11y:supplemental:ci
ATTUNE_CONSOLE_E2E_SUPPLEMENTAL=1 pnpm exec playwright test --config playwright.config.ts --project=edge-desktop --project=firefox-desktop --project=webkit-desktop --list
```

Required repository-level commands:

```sh
go vet ./...
go build ./...
go test -short ./...
make ci-check
```

Required browser evidence:

- desktop Chromium route sweep passes for all seven #171 routes
- mobile Chromium route sweep passes for all seven #171 routes
- no untriaged browser console errors in the covered workflows
- no unhandled or unexpected failed mocked `/fb/v1/console/**` requests
- no document-level horizontal overflow in the mobile project
- no untriaged serious, critical, or unallowlisted moderate axe violations
- required success/status-message assertions pass for API keys, feedback
  retry-enrichment, MCP clients, GDPR, and dead deliveries
- Dialog focus contracts pass for API keys, MCP clients, GDPR, and dead
  deliveries
- forced-colors, WCAG text-spacing, and 200% text-sizing sweeps pass for the
  critical route set
- terminal-failure workbench metrics and MCP client layout remain contained
  under 200% text sizing

Current automated browser evidence:

```sh
cd console
ATTUNE_CONSOLE_E2E_EXECUTABLE_PATH=/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome \
  pnpm exec playwright test --config playwright.config.ts --reporter=list --workers=1
```

Result: 68 passed across `chromium-desktop` and `chromium-mobile`.

Current CI-safe supplemental browser evidence:

```sh
cd console
pnpm test:e2e:a11y:supplemental:ci
```

Result: 68 passed across the Firefox and WebKit desktop projects.

Current supplemental browser project evidence:

```sh
cd console
ATTUNE_CONSOLE_E2E_SUPPLEMENTAL=1 \
  pnpm exec playwright test --config playwright.config.ts \
    --project=edge-desktop --project=firefox-desktop --project=webkit-desktop \
    --reporter=list --workers=1
```

Result: 102 passed across the Edge, Firefox, and WebKit desktop projects.

The final gate includes the seven routed Console pages, legacy redirect aliases,
shell navigation checks, narrow mobile route containment at 320px and 280px,
route churn, long GDPR subject identifiers, repeated API-key dialog cycles,
repeated feedback/audit detail-sheet cycles, terminal workbench jump links,
API-key create/revoke success and error paths, and success/error status paths
for feedback retry-enrichment, MCP clients, GDPR validation, and dead-delivery
retry. It also covers forced-colors mode, WCAG text-spacing overrides, and
200% text sizing. Toast-based status checks assert the Sonner
`Notifications alt+T` polite live region as well as the visible message text.

Current in-app browser evidence from the built Console and full Go server:

- The manual pass used the repository server at `http://127.0.0.1:8090` with
  the current built `console/dist` assets.
- Seven routed Console pages passed real-browser checks for canonical URL,
  route-specific title, level-one heading, skip link to `#main-content`,
  exactly one `main#main-content`, no document-level horizontal overflow, and
  available table names where the real data state renders a table.
- Legacy entries `/console/api-keys` and `/console/outbox-dead` redirected to
  their canonical integration and administration routes while preserving page
  title and heading contracts.
- Mobile viewport navigation at 390 x 844 opened the drawer and reached MCP
  clients and dead deliveries without document-level overflow.
- API key create dialog restored focus to the opener after cancel and exposed a
  stable `aria-controls="api-key-effective-scopes"` relationship for the
  effective-scopes disclosure before and after expansion.
- GDPR validation and step-up behavior was verified without submitting a
  sensitive operation: empty export produced the subject-key validation message,
  the step-up dialog exposed the password field, and Escape dismissed it.
- MCP clients were verified against the current real database state, where all
  clients are revoked: tool-policy checkboxes had stable accessible names and
  were disabled with the read-only alert present.
- Dead-delivery status filters changed `aria-pressed` correctly; the default
  dead-letter state rendered the named table, and the empty failed state
  rendered the expected empty message.
- Audit-log `/` shortcut focused the loaded-record search input.
- Browser error logs were empty after the manual run.
- Additional in-app browser edge checks on 2026-07-01 used a 320 x 568 viewport
  against the live local Console. Feedback and feedback-detail states had no
  document overflow and no unnamed `button[aria-controls]` controls. GDPR held a
  497-character subject key with no document overflow. API-key create dialog
  open/cancel cycles left no visible dialog behind, had no unnamed controlled
  buttons, and produced no browser console errors. The workflow transition
  select's placeholder state now has a localized "next status" accessible name; the mocked
  Playwright route exercises that state directly because the current local
  database row had no executable transition route.

Required documentation evidence:

- `docs/accessibility/console-component-contracts.md` describes reusable
  contracts for the patterns changed by #171
- `docs/accessibility/console-wcag-2.2-aa.md` maps every WCAG 2.2 A/AA
  criterion for #171 scope and has no unevidenced rows
- `docs/accessibility/console-assistive-technology-matrix.md` records the
  required manual assistive-technology rows; unrecorded rows must stay visible
  until tested
- `docs/accessibility/console-release-checklist.md` defines the release-facing
  automation and manual AT acceptance path for Console accessibility changes
- The Codex environment assessment in
  `docs/accessibility/console-assistive-technology-matrix.md` records that the
  local session has macOS browser automation but no Windows AT, mobile AT,
  iOS/Android simulator tooling, macOS accessibility automation permission, or
  reliable screen-reader speech/caption capture. The manual AT rows therefore
  remain `Not recorded` rather than being replaced by automation.

## References

- [Issue #171: Console accessibility and keyboard triage pass](https://github.com/Phixsura/attune/issues/171)
- [WCAG 2.2](https://www.w3.org/TR/WCAG22/)
- [WAI-ARIA APG: Dialog modal pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/)
- [WAI-ARIA APG: Grid pattern](https://www.w3.org/WAI/ARIA/apg/patterns/grid/)
- [GitHub Primer accessibility engineering checklist](https://primer.style/accessibility/tools-and-resources/checklists/engineering-checklist/)
- [IBM Carbon accessibility status](https://carbondesignsystem.com/components/overview/accessibility-status/)
- [GOV.UK Design System accessibility statement](https://design-system.service.gov.uk/accessibility-statement/)
- [USWDS accessibility guidance](https://designsystem.digital.gov/documentation/accessibility/)
- [Atlassian Design System accessibility](https://atlassian.design/foundations/accessibility/)
- [Shopify Polaris accessibility](https://polaris.shopify.com/foundations/accessibility)
- [React Aria accessibility introduction](https://react-spectrum.adobe.com/react-aria/accessibility.html)
- [Radix UI Dialog](https://www.radix-ui.com/primitives/docs/components/dialog)
