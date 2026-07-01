# Console WCAG 2.2 A/AA Traceability

This document maps issue
[#171](https://github.com/Phixsura/attune/issues/171) Console workbench changes
to WCAG 2.2 A and AA success criteria. It is a traceability aid for engineering
review, not a legal conformance statement.

Scope: `/console/feedback`, `/console/feedback/terminal-failures`,
`/console/integrations/api-keys`, `/console/mcp-clients`,
`/console/administration/gdpr`,
`/console/administration/dead-deliveries`, and
`/console/administration/audit-log`.

Automation evidence comes from Vitest accessibility smoke tests and Playwright
Chromium browser E2E coverage at desktop and mobile viewports, including
forced-colors mode, WCAG text-spacing overrides, and 200% text sizing. Manual
evidence must be recorded separately when assistive-technology review is
performed.

## Evidence Legend

- `Automated pass`: covered by static, unit, component, or Playwright browser
  tests in this change.
- `Manual checklist required`: requires human review in the assistive-technology
  matrix before any conformance claim.
- `Not applicable`: the scoped Console surfaces do not include that content or
  interaction.

## Criteria

| Criterion | Level | Status | Evidence |
|---|---:|---|---|
| 1.1.1 Non-text Content | A | Automated pass + manual checklist required | Icon buttons and brand imagery expose text alternatives or accessible names; manual screen-reader review must confirm names in workflow context. |
| 1.2.1 Audio-only and Video-only (Prerecorded) | A | Not applicable | The scoped Console routes do not include prerecorded audio-only or video-only media. |
| 1.2.2 Captions (Prerecorded) | A | Not applicable | The scoped Console routes do not include prerecorded synchronized media. |
| 1.2.3 Audio Description or Media Alternative (Prerecorded) | A | Not applicable | The scoped Console routes do not include prerecorded video content. |
| 1.2.4 Captions (Live) | AA | Not applicable | The scoped Console routes do not include live audio or video. |
| 1.2.5 Audio Description (Prerecorded) | AA | Not applicable | The scoped Console routes do not include prerecorded video content. |
| 1.3.1 Info and Relationships | A | Automated pass + manual checklist required | Native tables, captions, headings, labels, dialog titles, and axe checks cover the final DOM. |
| 1.3.2 Meaningful Sequence | A | Automated pass + manual checklist required | Route sweeps verify rendered landmarks and headings; manual reading-order review remains required. |
| 1.3.3 Sensory Characteristics | A | Automated pass + manual checklist required | Critical actions use text labels, names, and states rather than shape or position alone. |
| 1.3.4 Orientation | AA | Automated pass | Desktop and mobile Playwright projects cover responsive routes without orientation-specific blocking. |
| 1.3.5 Identify Input Purpose | AA | Manual checklist required | Console admin fields are domain-specific and labelled; autocomplete purpose review requires manual confirmation. |
| 1.4.1 Use of Color | A | Automated pass + manual checklist required | Statuses include text/state in addition to color; routed DOM checks run in normal and forced-colors browser modes. |
| 1.4.2 Audio Control | A | Not applicable | The scoped Console routes do not autoplay audio. |
| 1.4.3 Contrast (Minimum) | AA | Automated pass | Playwright axe checks caught and fixed status, toast, selected-row, and warning contrast issues. |
| 1.4.4 Resize Text | AA | Automated pass + manual checklist required | The browser gate applies 200% text sizing across scoped routes and asserts no document-level horizontal overflow; human zoom review remains required before a conformance claim. |
| 1.4.5 Images of Text | AA | Not applicable | The scoped Console routes do not use images of text for required information. |
| 1.4.10 Reflow | AA | Automated pass + manual checklist required | Mobile route sweeps plus 200% text-sizing sweeps assert no document-level horizontal overflow; manual zoom/reflow review remains required. |
| 1.4.11 Non-text Contrast | AA | Automated pass + manual checklist required | Focus rings, icon controls, and status indicators are checked by component and browser review, including forced-colors mode; manual visual review remains required. |
| 1.4.12 Text Spacing | AA | Automated pass + manual checklist required | The browser gate applies WCAG text-spacing overrides across scoped routes and asserts axe-clean, overflow-safe layouts; custom manual review remains required. |
| 1.4.13 Content on Hover or Focus | AA | Automated pass + manual checklist required | Audit action affordances respond to keyboard focus; hover/focus persistence requires manual sweep. |
| 2.1.1 Keyboard | A | Automated pass | MCP selection, tool policies, dialogs, filters, retry actions, and route workflows have keyboard-oriented tests. |
| 2.1.2 No Keyboard Trap | A | Automated pass + manual checklist required | Dialog and sheet focus tests cover critical flows; manual AT review remains required. |
| 2.1.4 Character Key Shortcuts | A | Automated pass | Audit-log shortcut behavior ignores text-entry targets and is covered by tests. |
| 2.2.1 Timing Adjustable | A | Not applicable | The scoped Console routes do not add time-limited interactions. |
| 2.2.2 Pause, Stop, Hide | A | Automated pass + manual checklist required | Reduced-motion CSS is present; loading and spinner behavior must be reviewed manually for distraction impact. |
| 2.3.1 Three Flashes or Below Threshold | A | Automated pass | The scoped routes do not introduce flashing content. |
| 2.4.1 Bypass Blocks | A | Automated pass | The app shell has a skip link to `#main-content`, covered by shell and route tests. |
| 2.4.2 Page Titled | A | Automated pass | Covered routes set route-specific document titles and Playwright asserts them. |
| 2.4.3 Focus Order | A | Automated pass + manual checklist required | Dialog open/close flows and keyboard workflows are tested; manual AT review must confirm perceived order. |
| 2.4.4 Link Purpose (In Context) | A | Automated pass + manual checklist required | Routed links and workflow links expose visible names; manual context review remains required. |
| 2.4.5 Multiple Ways | AA | Automated pass | The Console shell navigation and canonical routes provide stable access to scoped pages. |
| 2.4.6 Headings and Labels | AA | Automated pass + manual checklist required | Each covered route has a level-one heading and labelled critical controls. |
| 2.4.7 Focus Visible | AA | Automated pass + manual checklist required | Browser workflows exercise keyboard focus on critical controls; manual visual review remains required. |
| 2.4.11 Focus Not Obscured (Minimum) | AA | Manual checklist required | Sticky headers and modal layers need manual keyboard sweep on desktop and mobile. |
| 2.5.1 Pointer Gestures | A | Automated pass | Critical workflows use buttons, links, and ordinary activation, not path-based gestures. |
| 2.5.2 Pointer Cancellation | A | Automated pass + manual checklist required | Native buttons and links are used for critical actions; manual pointer review remains required. |
| 2.5.3 Label in Name | A | Automated pass + manual checklist required | Playwright uses role/name locators tied to visible labels for critical controls. |
| 2.5.4 Motion Actuation | A | Not applicable | The scoped Console routes do not use device or user-motion activation. |
| 2.5.7 Dragging Movements | AA | Not applicable | The scoped Console routes do not require dragging movements. |
| 2.5.8 Target Size (Minimum) | AA | Manual checklist required | Mobile route sweeps run in Playwright; target sizing review must be recorded manually. |
| 3.1.1 Language of Page | A | Automated pass | The Console root sets page language through the application shell. |
| 3.1.2 Language of Parts | AA | Manual checklist required | Mixed product terms and locale strings require manual review in localized builds. |
| 3.2.1 On Focus | A | Automated pass + manual checklist required | Tested focus flows do not trigger unexpected context changes. |
| 3.2.2 On Input | A | Automated pass + manual checklist required | Filters and form controls do not commit destructive actions on input alone. |
| 3.2.3 Consistent Navigation | AA | Automated pass | The app shell navigation remains stable across covered routes. |
| 3.2.4 Consistent Identification | AA | Automated pass + manual checklist required | Reused controls keep consistent names and icons; manual review remains required across locales. |
| 3.2.6 Consistent Help | A | Not applicable | The scoped routes do not introduce help mechanisms that must be ordered consistently across pages. |
| 3.3.1 Error Identification | A | Automated pass + manual checklist required | Dialog validation and mutation error paths expose text errors; manual AT review remains required. |
| 3.3.2 Labels or Instructions | A | Automated pass + manual checklist required | Forms and sensitive flows have labels and helper text covered by role/name checks. |
| 3.3.3 Error Suggestion | AA | Manual checklist required | Error messages are readable; suggestion quality needs manual review per workflow. |
| 3.3.4 Error Prevention (Legal, Financial, Data) | AA | Automated pass + manual checklist required | GDPR destructive flows require confirmation and step-up; manual review must confirm recovery clarity. |
| 3.3.7 Redundant Entry | A | Manual checklist required | Scoped workflows do not force repeated known data in automated tests; manual review remains required. |
| 3.3.8 Accessible Authentication (Minimum) | AA | Manual checklist required | GDPR step-up is password based and labelled; authentication alternatives require manual product review. |
| 4.1.2 Name, Role, Value | A | Automated pass | Vitest role/name tests and browser axe checks cover critical controls, dialogs, tables, and disclosures. |
| 4.1.3 Status Messages | AA | Automated pass + manual checklist required | Loading states use status semantics, and toast-based workflow results are asserted inside the Sonner `Notifications alt+T` polite live region; manual screen-reader announcement review remains required. |

WCAG 2.2 removed 4.1.1 Parsing as an active success criterion, so it is not
included in the A/AA evidence table.
