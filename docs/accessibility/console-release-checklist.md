# Console Accessibility Release Checklist

Use this checklist for Console changes that touch keyboard interaction,
focus management, dialogs, sheets, dense tables, routed workbenches, or status
messages. It is the release companion for issue
[#171](https://github.com/Phixsura/attune/issues/171).

## Automation Gate

Record the command output in the PR description.

| Check | Command | Required result |
|---|---|---|
| Repository gate | `make ci-check` | Passes on the final branch state. |
| Browser accessibility gate | `cd console && pnpm test:e2e:a11y` | Passes the default Chromium desktop and mobile projects. |
| Adaptive browser modes | Included in `pnpm test:e2e:a11y` | Forced colors, WCAG text spacing, and 200% text sizing keep critical routes axe-clean and free of document overflow where applicable. |
| Supplemental desktop engines | `cd console && pnpm test:e2e:a11y:supplemental` | Required when the change claims local browser parity across Edge, Firefox, and WebKit. |
| Supplemental CI engines | `cd console && pnpm test:e2e:a11y:supplemental:ci` | Runs Firefox and WebKit in the manual/scheduled `Console Accessibility Supplemental` workflow. |
| Artifact hygiene | `bash scripts/lint-artifacts.sh --strict` | Hard checks pass. |

The supplemental Playwright scripts are intentionally opt-in. The default CI
gate stays focused on deterministic Chromium desktop and mobile coverage, while
the local supplemental run covers Edge, Firefox, and WebKit desktop projects
for release or browser-parity claims. The scheduled/manual GitHub workflow runs
the CI-safe Firefox and WebKit subset; Edge remains part of the local
supplemental sweep where the channel is installed.

## Manual Assistive-Technology Gate

Record dated rows in
[`console-assistive-technology-matrix.md`](console-assistive-technology-matrix.md).

| Row | Required platform | Required before |
|---|---|---|
| macOS VoiceOver | Safari or Chrome on macOS | Any desktop screen-reader conformance claim. |
| Windows NVDA | Chrome or Edge on Windows | Any Windows screen-reader conformance claim. |
| Windows JAWS | Chrome or Edge on Windows | Enterprise screen-reader conformance claims. |
| iOS VoiceOver | Safari on iOS | Mobile screen-reader conformance claims. |
| Android TalkBack | Chrome on Android | Mobile screen-reader conformance claims. |

Every row must include the OS version, browser version, assistive-technology
version, tester, date, build identifier, workflows covered, and linked defects.
Do not mark a row passing when a required workflow has an unresolved defect.

## Required Workflow Sweep

Cover these workflows in automation and in each applicable manual AT row:

| Workflow | Required evidence |
|---|---|
| Skip link and main landmark | Keyboard focus moves to the main content target. |
| Feedback triage and terminal failures | Row actions, retry buttons, jump links, and detail sheets expose stable role/name/state. |
| API keys | Create, secret reveal, scope disclosure, revoke success, revoke error, and focus restoration are operable by keyboard. |
| MCP clients | Client selection, policy controls, revoke dialogs, session termination, and grant revocation are reachable by role/name. |
| GDPR | Filters, step-up dialog, validation status, long subject keys, and mobile layout have no document overflow. |
| Dead deliveries | Status filters and retry actions announce success and error status messages. |
| Audit log | Search shortcut, detail sheet, Escape close, and focus restoration are reliable. |
| Adaptive modes | Forced-colors mode, WCAG text spacing, and 200% text sizing preserve route structure without document-level horizontal overflow. |

## Defect Handling

- Link every accessibility defect to an issue or PR comment.
- Re-run the affected automated test before marking the defect fixed.
- Re-run the affected manual AT row when the defect involves spoken output,
  virtual cursor behavior, touch exploration, or platform screen-reader
  navigation.
- Keep automation evidence and manual AT evidence separate. Passing Playwright,
  axe, Testing Library, or browser console checks does not establish
  screen-reader conformance.
