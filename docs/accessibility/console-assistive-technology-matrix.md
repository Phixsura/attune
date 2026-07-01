# Console Assistive-Technology Matrix

This matrix records manual assistive-technology evidence for issue
[#171](https://github.com/Phixsura/attune/issues/171). Automated Playwright and
axe evidence is necessary but does not replace this matrix.

Do not use this document to claim product conformance unless each required row
has a dated passing result for the build being released.

Use
[`console-release-checklist.md`](console-release-checklist.md)
as the release gate that combines this manual matrix with the automated
Console browser checks.

## Required Workflows

- Skip link to main content.
- Feedback triage route and terminal-failure workbench.
- API key create, secret reveal, and close/restore behavior.
- MCP client selection and tool-policy controls.
- GDPR step-up dialog and request center.
- Dead-delivery retry action.
- Audit-log search shortcuts and detail sheet.

## Manual Matrix

| Platform | Browser | Assistive technology | Required workflows | Date | Tester | Build | Result | Notes |
|---|---|---|---|---|---|---|---|---|
| macOS | Safari or Chrome | VoiceOver | Skip link, GDPR step-up, API key dialogs, audit detail sheet |  |  |  | Not recorded | Required before any screen-reader conformance claim. |
| Windows | Chrome or Edge | NVDA | Feedback triage, MCP tool policies, outbox retry, audit shortcuts |  |  |  | Not recorded | Required before any screen-reader conformance claim. |
| Windows | Chrome or Edge | JAWS | Dense table navigation, API key dialogs, MCP revoke dialog, audit detail sheet |  |  |  | Not recorded | Required before any screen-reader conformance claim. |
| iOS | Safari | VoiceOver | Mobile navigation, GDPR request center, API key dialog close/restore |  |  |  | Not recorded | Required before any mobile screen-reader conformance claim. |
| Android | Chrome | TalkBack | Mobile navigation, GDPR request center, dead-delivery retry, MCP client detail |  |  |  | Not recorded | Required before any mobile screen-reader conformance claim. |

## Codex Environment Assessment

This repository change was prepared in a local Codex session on macOS 26.1. The
session can run browser automation and local shell checks, but it must not turn
missing assistive-technology coverage into a passing manual result.

| Check | Command | Result | Impact |
|---|---|---|---|
| Host OS | `sw_vers` | macOS 26.1, Darwin arm64 | macOS VoiceOver is the only potentially relevant desktop AT on this host. |
| Browser availability | `ls /Applications` | Safari, Google Chrome, and Microsoft Edge are installed | Browser automation is available, but this does not prove screen-reader output. |
| Windows AT availability | `ls /Applications` / `pgrep` checks for NVDA and JAWS | Not installed | NVDA and JAWS rows require a Windows environment. |
| Mobile AT availability | `xcrun simctl list devices available` and `command -v adb` | `simctl` unavailable; `adb` unavailable | iOS VoiceOver and Android TalkBack rows require device or simulator/emulator access. |
| macOS accessibility automation | `osascript -e 'tell application "System Events" to get UI elements enabled'` | `false` | Codex cannot drive macOS UI accessibility automation in this session. |
| VoiceOver observability | Local shell and browser checks | No reliable speech or caption output capture is available | Codex cannot honestly record a VoiceOver pass without a human tester verifying spoken output and navigation. |

The matrix therefore stays `Not recorded` for every manual AT row. This is an
intentional quality gate: automated Playwright, axe, and DOM accessibility-tree
checks remain valid evidence, but they are not a substitute for a dated human
screen-reader run.

## Automated Evidence For This Change

| Evidence | Command | Result |
|---|---|---|
| Desktop and mobile browser E2E route sweep, workflow checks, narrow-viewport stress, route churn, forced-colors mode, WCAG text spacing, and 200% text sizing | `cd console && ATTUNE_CONSOLE_E2E_EXECUTABLE_PATH=... pnpm exec playwright test --config playwright.config.ts --reporter=list --workers=1` | 68 passed |
| Supplemental CI desktop browser E2E sweep | `cd console && pnpm test:e2e:a11y:supplemental:ci` | 68 passed across Firefox and WebKit desktop projects |
| Supplemental local desktop browser E2E sweep | `cd console && ATTUNE_CONSOLE_E2E_SUPPLEMENTAL=1 pnpm exec playwright test --config playwright.config.ts --project=edge-desktop --project=firefox-desktop --project=webkit-desktop --reporter=list --workers=1` | 102 passed across Edge, Firefox, and WebKit desktop projects |
| Static accessibility lint and formatting | `cd console && pnpm biome check` | Passed |
| Type and production build smoke | `cd console && pnpm exec tsc -b --noEmit --pretty false` and `cd console && pnpm exec vite build` | Passed |
| Component and page accessibility/unit coverage | `cd console && pnpm vitest run --coverage` | 94 files / 851 tests passed |

## Recording Rules

- Record the exact OS, browser, and assistive-technology versions in `Notes`.
- Record the commit SHA or build identifier in `Build`.
- Link every unresolved defect to an issue.
- Retest the affected row after fixing a defect.
- Keep automation and manual AT evidence separate; passing browser automation
  does not imply passing assistive-technology review.
