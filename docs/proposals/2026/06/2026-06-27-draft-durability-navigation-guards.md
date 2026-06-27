# Draft Durability and Navigation Guards for Settings Editors

| | |
|---|---|
| **Issue** | #172 |
| **Status** | Implemented |
| **Started** | 2026-06-27 |
| **Related** | #90 (dimensions editor identity — established the seed-once pattern and `editable-rows.ts`) |

---

## Problem

Classification settings and prompt policy editing carry real operational risk.
Today, the console has **no protection** against accidental draft loss:

1. **No navigation guards** — navigating away from a dirty editor silently
   discards all work. Neither `useBlocker` nor `beforeunload` is used anywhere
   in the console.
2. **No draft persistence** — a page refresh (F5, browser crash, accidental
   close) destroys the in-progress edit. The form state lives only in React
   `useState`.
3. **No recovery path** — once lost, the operator must re-enter everything from
   scratch.

The enrichment runtime page (`enrichment-runtime-page.tsx`) has partial
protection — `shouldHydrateRuntimeDraft()` prevents background refetches from
clobbering a dirty draft, and `expectedVersion` provides optimistic concurrency
— but it still lacks navigation guards and draft persistence.

The classification settings page (`classification-settings-page.tsx`) has no
protection at all: its draft is seeded once in a `useState` initializer and
guarded only by `refetchOnWindowFocus: false`.

---

## Goals

- Unsaved edits survive harmless page refreshes (F5) and are recoverable.
- Navigation away from a dirty editor is intercepted with a confirmation dialog.
- Tab close / browser close triggers the native `beforeunload` prompt.
- The pattern is reusable across both target surfaces and future settings pages.
- Tests cover remount, navigation block, save, discard, and draft recovery.

## Non-goals

- **Server-side draft persistence** — adds backend complexity; `sessionStorage`
  (tab-scoped) is sufficient for the "accidental refresh" scenario.
- **Cross-tab draft sync** — `sessionStorage` is per-tab by design; operators
  editing the same setting in two tabs is an edge case covered by the existing
  `expectedVersion` optimistic lock (runtime page) or future backend versioning
  (classification page).
- **JSON side-by-side diff preview** — no industry precedent for settings pages
  (Grafana's JSON diff approach is a documented anti-pattern with widespread
  false-positive issues; see research).
- **Changing the runtime page's existing JSON-stringify dirty detection** — it
  works and is protected by `shouldHydrateRuntimeDraft()`; refactoring it is
  out of scope.
- **Adding `expected_version` to the classification settings backend** — a
  follow-up concern; this proposal focuses on frontend protection.

---

## Proposal

### Architecture: `useDraftGuard` hook + `UnsavedChangesDialog`

A single reusable hook encapsulates three layers of protection:

```
useDraftGuard<T>({ storageKey, draft, dirty, onRestore })
  ├── 1. sessionStorage persistence (debounced write on draft change)
  ├── 2. TanStack Router useBlocker (in-app navigation interception)
  └── 3. beforeunload (tab close / refresh interception)
```

A shared `UnsavedChangesDialog` component is driven by the blocker's resolver
state.

#### `useDraftGuard` API

```typescript
interface UseDraftGuardOpts<T> {
  /** Stable key for sessionStorage, e.g. "classification-settings".
   *  The hook prefixes this with "attune:draft:" automatically. */
  storageKey: string
  /** Current draft value to persist */
  draft: T
  /** Whether the draft has unsaved changes */
  dirty: boolean
  /** Disable all guards (e.g. when the user lacks edit permission) */
  disabled?: boolean
}

interface UseDraftGuardReturn {
  /** Whether the navigation blocker dialog should be shown */
  dialogOpen: boolean
  /** Confirm navigation (discard draft and proceed) */
  confirmLeave: () => void
  /** Cancel navigation (stay on page) */
  cancelLeave: () => void
  /** Explicitly clear the stored draft (call on save success or manual discard) */
  clearDraft: () => void
}
```

The hook also exports a standalone pure function for use in `useState`
initializers:

```typescript
function readDraft<T>(storageKey: string): T | null
```

#### Behavior details

**sessionStorage persistence:**
- Key format: `attune:draft:<storageKey>` (e.g. `attune:draft:classification-settings`)
- Write: debounced 500ms after `draft` changes, only when `dirty` is true
- Read: the consuming page's `useState` initializer calls `readDraft(key)`
  synchronously to seed the form on first render (no flash of server data).
  The hook itself does NOT read or restore — restoration is the page's
  responsibility so the page controls what state shape to hydrate.
- Clear: on `clearDraft()` call (save success, explicit discard, or
  "Restore Default")
- Serialization: `JSON.stringify` / `JSON.parse` — the draft types are plain
  objects (no `Date`, no `Map`, no circular refs)

**Navigation guard (in-app):**
- Uses `useBlocker({ shouldBlockFn: () => dirty, withResolver: true,
  enableBeforeUnload: dirty })` from TanStack Router
- When blocked, `dialogOpen` becomes `true` and drives `UnsavedChangesDialog`
- `confirmLeave` calls `resolver.proceed()` + clears sessionStorage
- `cancelLeave` calls `resolver.reset()`
- `enableBeforeUnload` is conditional on `dirty` — no phantom prompts on
  clean forms

**beforeunload (browser-level):**
- Handled by TanStack Router's `enableBeforeUnload` option — no manual
  `addEventListener` needed

#### `UnsavedChangesDialog`

A controlled Radix dialog (using the existing `Dialog` component) with three
actions:

| Button | Style | Action |
|---|---|---|
| Cancel (stay) | `outline` (secondary) | `cancelLeave()` — close dialog, remain on page |
| Discard and leave | `destructive` | `confirmLeave()` — clear draft, proceed to target |
| _(No "Save and leave")_ | — | Save-and-leave adds complexity (mutation in flight during navigation); the operator should save first, then navigate |

Copy is i18n'd:
- Title: `draft.unsaved_changes_title` → "Unsaved changes"
- Body: `draft.unsaved_changes_body` → "You have unsaved changes that will be
  lost if you leave this page."

### Integration: Classification Settings Page

**Draft state shape for storage:**

```typescript
interface ClassificationDraft {
  prompt: string
  rows: EditableDimension[]
}
```

**Changes to `ClassificationSettingsForm`:**

1. **useState initializer** reads sessionStorage first:
   ```typescript
   const stored = readDraft<ClassificationDraft>('classification-settings')
   const [prompt, setPrompt] = useState(() =>
     stored?.prompt ?? initial.promptTemplate ?? initial.defaultPromptTemplate
   )
   const [rows, setRows] = useState<EditableDimension[]>(() =>
     stored?.rows ?? seedDimensions(initial.dimensions ?? [])
   )
   ```
   If a stored draft exists, a toast with an action button offers "Undo restore"
   (revert to server state) for 8 seconds.

2. **useDraftGuard** hook wired up:
   ```typescript
   const guard = useDraftGuard({
     storageKey: 'classification-settings',
     draft: { prompt, rows },
     dirty: touched,
     disabled: !canEdit,
   })
   ```

3. **Dirty detection** — event-driven, not JSON diff:
   ```typescript
   const [touched, setTouched] = useState(false)
   // Set touched=true on any edit action (setPrompt, setRows, etc.)
   // Reset to false on save success or discard
   ```
   This avoids Grafana's false-positive problem entirely.

4. **Save success** calls `guard.clearDraft()` + resets `touched`.

5. **"Discard Changes" button** added next to "Save":
   - Resets prompt and rows to `initial.*` values
   - Calls `guard.clearDraft()`
   - Requires confirmation via a small inline dialog
   - Only visible when `dirty` is true

6. **`UnsavedChangesDialog`** rendered at the bottom of the form, driven by
   `guard.dialogOpen`.

### Integration: Enrichment Runtime Page

**Changes are minimal** because the page already has `shouldHydrateRuntimeDraft`
and `expectedVersion`:

1. **useDraftGuard** hook wired up with existing `dirty` computation:
   ```typescript
   const guard = useDraftGuard({
     storageKey: 'enrichment-runtime',
     draft,
     dirty,
     disabled: !canEdit,
   })
   ```

2. **useState initializer** reads sessionStorage:
   ```typescript
   const [draft, setDraft] = useState<DraftState>(() =>
     readDraft<DraftState>('enrichment-runtime') ?? emptyDraft()
   )
   ```

3. **Hydration guard** — `shouldHydrateRuntimeDraft` already prevents server
   refetches from clobbering a dirty draft; no change needed.

4. **Save/reset/rollback success** calls `guard.clearDraft()`.

5. **`UnsavedChangesDialog`** rendered, driven by `guard.dialogOpen`.

### Draft recovery UX

When a stored draft is found on mount, a **sonner action toast** appears:

```
┌──────────────────────────────────────┐
│ ↻  Draft recovered from last session │
│                          [Discard]   │
└──────────────────────────────────────┘
```

- Auto-dismisses after 8 seconds (the draft is already applied)
- "Discard" button reverts to server state and clears sessionStorage
- No modal interruption — the operator can immediately continue editing

This is lighter than a blocking dialog (Cloudscape pattern) and matches the
"restore is the default, discard is opt-in" UX that preserves work.

---

## Alternatives considered

### A. localStorage instead of sessionStorage

Drafts would survive tab close and persist across sessions. Rejected because:
- Classification settings and prompt policies are security-sensitive
  configurations (GitLab explicitly prohibits auto-save for such data)
- Stale drafts from days ago auto-restoring is a footgun
- Would need TTL-based expiry and multi-tab conflict resolution

### B. Blocking modal on draft recovery (instead of action toast)

A modal forces the operator to choose before seeing the page. Rejected because:
- Interrupts the workflow; the operator may not remember what they were editing
- The toast approach lets them see the recovered draft in context first
- If they want to discard, one click on the toast's "Discard" button suffices

### C. "Save and leave" in the navigation dialog

Adding a third button that triggers save, waits for success, then navigates.
Rejected because:
- Save can fail (validation, network, conflict) — handling failure mid-navigation
  is complex
- The operator should save explicitly, see the success feedback, then navigate
- Industry consensus (Cloudscape, GitLab Pajamas) uses only "Discard and leave"
  + "Stay"

### D. JSON diff for dirty detection on classification page

Compare `JSON.stringify(current)` against `JSON.stringify(initial)`. Rejected
based on Grafana's documented pain (issues #111400, #24663): null handling,
field ordering, and schema drift cause false positives. Event-driven `touched`
flag is simpler and correct.

---

## Risks / tradeoffs

| Risk | Mitigation |
|---|---|
| sessionStorage has ~5MB limit per origin | Classification draft is small (~10KB max); not a concern |
| `EditableDimension` contains `_key` (crypto UUID) — restoring from storage creates new `_key` values | Store rows with their `_key`s; on restore, the keys survive the round-trip through JSON |
| Debounced write may lose the last ~500ms of edits before a crash | Acceptable tradeoff; the alternative (sync write on every keystroke) causes jank on large prompt templates |
| `useBlocker` with `withResolver` is a newer TanStack Router API | Already exported in v1.95.0 (verified in `node_modules`); well-documented with stable types |
| Draft restored from sessionStorage may be for a different tenant (if operator switches tenants) | Include tenant ID in the storage key: `attune:draft:<tenantId>:<page>` |

---

## Implementation plan

### Phase 1: Shared infrastructure (hook + dialog)

1. Create `console/src/hooks/use-draft-guard.ts` — the `useDraftGuard` hook
2. Create `console/src/hooks/use-draft-guard.test.ts` — unit tests for
   sessionStorage read/write/clear, debounce behavior, and blocker state
3. Create `console/src/components/unsaved-changes-dialog.tsx` — the shared
   dialog component
4. Create `console/src/components/unsaved-changes-dialog.test.tsx` — render
   tests for open/close/action states

### Phase 2: Classification settings integration

5. Modify `classification-settings-page.tsx`:
   - Add `touched` state and dirty detection
   - Wire `useDraftGuard` with sessionStorage restore in useState initializer
   - Add "Discard Changes" button with confirmation
   - Render `UnsavedChangesDialog`
6. Add tests for draft recovery, navigation blocking, save-clears-draft,
   discard-clears-draft

### Phase 3: Enrichment runtime integration

7. Modify `enrichment-runtime-page.tsx`:
   - Wire `useDraftGuard` with existing `dirty` state
   - Add sessionStorage restore in useState initializer
   - Call `clearDraft()` on save/reset/rollback success
   - Render `UnsavedChangesDialog`
8. Add tests for navigation blocking and draft persistence

### Phase 4: i18n and polish

9. Add all new translation keys to `console/src/i18n/zh-CN.json`
10. Verify Strict Mode / double-mount behavior in tests

### Files changed

| File | Action |
|---|---|
| `console/src/hooks/use-draft-guard.ts` | New |
| `console/src/hooks/use-draft-guard.test.ts` | New |
| `console/src/components/unsaved-changes-dialog.tsx` | New |
| `console/src/components/unsaved-changes-dialog.test.tsx` | New |
| `console/src/features/settings/components/classification-settings-page.tsx` | Modify |
| `console/src/features/settings/components/classification-settings-page.test.tsx` | New |
| `console/src/features/settings/components/enrichment-runtime-page.tsx` | Modify |
| `console/src/features/settings/components/enrichment-runtime-page.test.tsx` | New or modify |
| `console/src/i18n/zh-CN.json` | Modify |
| `CHANGELOG.md` | Modify |

---

## Verification

- [ ] Classification: edit prompt → navigate away → dialog appears → "Stay"
      keeps draft → "Discard and leave" proceeds
- [ ] Classification: edit prompt → F5 → page reloads → toast shows "Draft
      recovered" → edits are restored → "Discard" on toast reverts to server
      state
- [ ] Classification: edit → save → navigate away → no dialog (clean state)
- [ ] Classification: edit → "Discard Changes" → confirm → form resets to
      server state, sessionStorage cleared
- [ ] Runtime: edit fields → navigate away → dialog appears
- [ ] Runtime: edit fields → F5 → draft recovered from sessionStorage
- [ ] Runtime: save/reset/rollback → sessionStorage cleared
- [ ] Strict Mode: double-mount does not produce duplicate toasts or phantom
      dirty state
- [ ] `beforeunload`: dirty form → close tab → browser native prompt fires
- [ ] `beforeunload`: clean form → close tab → no prompt
- [ ] All existing tests pass (no regressions)

---

## References

- [TanStack Router useBlocker](https://tanstack.com/router/v1/docs/framework/react/guide/navigation-blocking) — `withResolver` mode, `enableBeforeUnload`
- [Cloudscape unsaved changes pattern](https://cloudscape.design/patterns/general/unsaved-changes/) — two-layer guard model
- [GitLab Pajamas saving & feedback](https://design.gitlab.com/usability/saving-and-feedback) — modal copy, auto-save prohibition for sensitive data
- [Grafana save patterns (Saga)](https://grafana.com/developers/saga/patterns/save/) — five-tier taxonomy
- [Grafana JSON diff issues](https://github.com/grafana/grafana/issues/111400) — false-positive dirty detection
- [React Router useBlocker ADR](https://github.com/remix-run/react-router/blob/main/decisions/0001-use-blocker.md) — three-state model, POP navigation handling
- #90 proposal — established seed-once pattern and `editable-rows.ts`
