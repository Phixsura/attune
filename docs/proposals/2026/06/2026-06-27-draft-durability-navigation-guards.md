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

- **Server-side draft persistence** — adds backend complexity; localStorage
  with TTL-based expiry is sufficient for the "accidental refresh" scenario.
- **Cross-tab draft conflict resolution** — `BroadcastChannel` notifies other
  tabs when a draft is cleared; concurrent editing in two tabs is an edge case
  covered by the existing `expectedVersion` optimistic lock (runtime page) or
  future backend versioning (classification page).
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
useDraftGuard<T>({ storageKey, draft, dirty, disabled, onExternalSave })
  ├── 1. localStorage persistence (debounced write, StoredEnvelope with 24h TTL)
  ├── 2. TanStack Router useBlocker (in-app navigation interception)
  ├── 3. beforeunload (synchronous flush + tab close interception)
  └── 4. BroadcastChannel cross-tab draft-cleared notification
```

A shared `UnsavedChangesDialog` component is driven by the blocker's resolver
state.

#### `useDraftGuard` API

```typescript
interface UseDraftGuardOpts<T> {
  /** Stable key for localStorage, e.g. "classification-settings".
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

**localStorage persistence (StoredEnvelope):**
- Key format: `attune:draft:<storageKey>` (e.g. `attune:draft:classification-settings`)
- Storage format: `StoredEnvelope<T>` with `_v` (schema version), `_ts`
  (timestamp), `data` (draft payload). Non-envelope legacy data is discarded
  on read. Drafts older than 24 hours (configurable TTL) are auto-purged.
- Write: debounced 500ms after `draft` changes, only when `dirty` is true
- Read: the consuming page's `useState` initializer calls `readDraft(key)`
  synchronously to seed the form on first render (no flash of server data).
  `readDraft` also handles sessionStorage→localStorage migration (one-time).
- Clear: on `clearDraft()` call (save success, explicit discard, or
  "Restore Default"). Clears both localStorage and sessionStorage.
- Cross-tab: `clearDraft()` posts a `draft-cleared` message via
  `BroadcastChannel`; other tabs receive it and call `onExternalSave` (refetch).

**Navigation guard (in-app):**
- Uses `useBlocker({ shouldBlockFn: () => isBlockedRef.current, withResolver:
  true, disabled: !isBlocked })` from TanStack Router
- When blocked, `dialogOpen` becomes `true` and drives `UnsavedChangesDialog`
- `confirmLeave` calls `clearDraft()` + `blocker.proceed()`
- `cancelLeave` calls `blocker.reset()`

**beforeunload (browser-level):**
- Manual `window.addEventListener('beforeunload', flush)` that synchronously
  writes the current draft to localStorage via `writeDraft`. This ensures the
  draft survives even if the debounce timer hasn't fired yet.

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

1. **useState initializer** reads localStorage first (via `readDraft`):
   ```typescript
   const storedDraft = useRef(
     readDraft<{ prompt: string; rows: EditableDimension[] }>('classification-settings'),
   ).current
   const [prompt, setPrompt] = useState(() =>
     storedDraft?.prompt ?? initial.promptTemplate ?? initial.defaultPromptTemplate
   )
   const [rows, setRows] = useState<EditableDimension[]>(() =>
     storedDraft?.rows ?? seedDimensions(initial.dimensions ?? [])
   )
   ```
   If a stored draft exists and differs from server state, a persistent
   `DraftBanner` (recovery variant) is shown with "Discard" and "Keep draft"
   options.

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

2. **useState initializer** reads localStorage (via `readDraft`):
   ```typescript
   const storedDraft = useRef(readDraft<Partial<DraftState>>('enrichment-runtime')).current
   const [draft, setDraft] = useState<DraftState>(() =>
     storedDraft ? { ...emptyDraft(), ...storedDraft } : emptyDraft()
   )
   ```

3. **Hydration guard** — `shouldHydrateRuntimeDraft` already prevents server
   refetches from clobbering a dirty draft; no change needed.

4. **Save/reset/rollback success** calls `guard.clearDraft()`.

5. **`UnsavedChangesDialog`** rendered, driven by `guard.dialogOpen`.

### Draft recovery UX

When a stored draft is found on mount and differs from server state, a
persistent **`DraftBanner`** (recovery variant) is shown inline:

```
┌─────────────────────────────────────────────────────────────────┐
│ 检测到未保存的草稿  —  保存于约 {age} 前    [丢弃] [保留草稿]   │
└─────────────────────────────────────────────────────────────────┘
```

- Stays visible until the operator explicitly chooses an action
- "Discard" button reverts to server state and clears localStorage
- "Keep draft" dismisses the banner and the operator continues editing
- If draft matches server state (e.g., operator saved in another tab),
  the draft is silently cleared on mount — no banner shown

The enrichment runtime page additionally supports a **conflict** variant:
when the server version changes while the operator has local edits, a
conflict banner offers "Load latest" or "Keep my edits".

---

## Alternatives considered

### A. ~~localStorage instead of sessionStorage~~ (revised — adopted)

Originally rejected in favor of sessionStorage. During implementation, switched
to localStorage with a `StoredEnvelope<T>` wrapper (`_v` schema version + `_ts`
timestamp) and 24-hour TTL auto-expiry. This addresses the original concerns:
- **Stale drafts** — 24h TTL auto-purges old entries; non-envelope legacy data
  is discarded on read.
- **Multi-tab** — `BroadcastChannel` notifies other tabs when a draft is
  cleared (save/discard), triggering `onExternalSave` refetch.
- **Security** — drafts are still client-only and auto-expire; operators must
  re-authenticate to reach the page at all.

The switch was motivated by the observation that sessionStorage (tab-scoped)
does not survive browser crashes or accidental tab close — the primary scenario
this feature protects against.

### B. ~~Blocking modal on draft recovery~~ (revised — persistent banner)

Originally proposed an action toast (auto-dismiss 8s). Replaced with a
persistent inline `DraftBanner` component (recovery variant) that remains
visible until the operator explicitly chooses "Keep draft" or "Discard". This
avoids the problem of the toast dismissing before the operator has context to
decide, and keeps the recovery action always reachable without modal
interruption.

### C. ~~"Save and leave" rejected~~ (revised — adopted)

Originally rejected for complexity. Added during implementation because both
pages already have well-tested save mutations with validation, and the pattern
is straightforward: `submitSave(() => guard.proceed())`. The save-and-leave
button is disabled during the mutation (shows "保存中…") and the dialog
prevents dismiss via overlay while saving. Only shown when `canEdit` is true.

### D. JSON diff for dirty detection on classification page

Compare `JSON.stringify(current)` against `JSON.stringify(initial)`. Rejected
based on Grafana's documented pain (issues #111400, #24663): null handling,
field ordering, and schema drift cause false positives. Event-driven `touched`
flag is simpler and correct.

---

## Risks / tradeoffs

| Risk | Mitigation |
|---|---|
| localStorage has ~5MB limit per origin | Classification draft is small (~10KB max); not a concern |
| `EditableDimension` contains `_key` (crypto UUID) — restoring from storage creates new `_key` values | Store rows with their `_key`s; on restore, the keys survive the round-trip through JSON |
| Debounced write may lose the last ~500ms of edits before a crash | `beforeunload` handler flushes synchronously via `writeDraft`; only a hard kill (OOM, force-quit) loses the tail |
| `useBlocker` with `withResolver` is a newer TanStack Router API | Already exported in v1.95.0 (verified in `node_modules`); well-documented with stable types |
| Draft restored from localStorage may be for a different tenant (if operator switches tenants) | Accepted risk: attune is currently single-tenant per deployment; if multi-tenant console is added, prefix storage keys with tenant ID |

---

## Implementation plan

### Phase 1: Shared infrastructure (hook + dialog)

1. Create `console/src/hooks/use-draft-guard.ts` — the `useDraftGuard` hook
2. Create `console/src/hooks/use-draft-guard.test.ts` — unit tests for
   localStorage read/write/clear, debounce behavior, and blocker state
3. Create `console/src/components/unsaved-changes-dialog.tsx` — the shared
   dialog component
4. Create `console/src/components/unsaved-changes-dialog.test.tsx` — render
   tests for open/close/action states

### Phase 2: Classification settings integration

5. Modify `classification-settings-page.tsx`:
   - Add `touched` state and dirty detection
   - Wire `useDraftGuard` with localStorage restore in useState initializer
   - Add "Discard Changes" button with confirmation
   - Render `UnsavedChangesDialog`
6. Add tests for draft recovery, navigation blocking, save-clears-draft,
   discard-clears-draft

### Phase 3: Enrichment runtime integration

7. Modify `enrichment-runtime-page.tsx`:
   - Wire `useDraftGuard` with existing `dirty` state
   - Add localStorage restore in useState initializer
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
| `console/src/hooks/use-keyboard-save.ts` | New |
| `console/src/hooks/use-keyboard-save.test.ts` | New |
| `console/src/components/unsaved-changes-dialog.tsx` | New |
| `console/src/components/unsaved-changes-dialog.test.tsx` | New |
| `console/src/components/draft-banner.tsx` | New |
| `console/src/components/draft-banner.test.tsx` | New |
| `console/src/components/save-status.tsx` | New |
| `console/src/components/save-status.test.tsx` | New |
| `console/src/features/settings/components/classification-settings-page.tsx` | Modify |
| `console/src/features/settings/components/classification-settings-page.test.tsx` | Modify |
| `console/src/features/settings/components/enrichment-runtime-page.tsx` | Modify |
| `console/src/features/settings/components/enrichment-runtime-page.test.ts` | Modify |
| `console/src/i18n/zh-CN.json` | Modify |
| `CHANGELOG.md` | Modify |

---

## Verification

- [ ] Classification: edit prompt → navigate away → dialog appears → "Stay"
      keeps draft → "Discard and leave" proceeds
- [ ] Classification: edit prompt → F5 → page reloads → DraftBanner shows
      "Draft recovered" → edits are restored → "Discard" reverts to server state
- [ ] Classification: edit → save → navigate away → no dialog (clean state)
- [ ] Classification: edit → "Discard Changes" → confirm → form resets to
      server state, localStorage cleared
- [ ] Runtime: edit fields → navigate away → dialog appears
- [ ] Runtime: edit fields → F5 → draft recovered from localStorage
- [ ] Runtime: save/reset/rollback → localStorage cleared
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
