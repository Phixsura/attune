# Durable Row Identity for the Dimensions Editor (remount-safe new/persisted tracking)

| | |
|---|---|
| **Issue** | #90 |
| **Status** | Implemented |
| **Started** | 2026-06-22 |
| **Updated** | 2026-06-23 — v2 after a 7-lens adversarial review (correctness, scope/history, test rigor, a11y, process, completeness, devil's-advocate). Corrected the change-history attribution, committed the seed-once/re-seed mechanism, added the `urgentSet` defect, fixed the test taxonomy (fail-on-main vs guard tests), resolved the dependency-cruiser placement, and pinned the a11y decisions. |
| **Related** | #89 (4th-round review that filed this), #91 (mutation-testing pilot — the dead-test gap that hid it), #10 (`flat-labels` — established `name` as the immutable machine key) |

> attune is Apache-2.0 OSS; this proposal is the canonical English record so
> external contributors can read it cold. CJK strings, where they appear, are
> concrete i18n examples, not commentary.

---

## TL;DR (30-second expectation for reviewers)

- **Decision**: the dimensions editor tracks "this row is new (identifier
  editable) vs persisted (identifier locked)" in **component-instance `useRef`
  (a `WeakMap` + a `Set`)**. That state is destroyed on any genuine
  unmount→remount, so an unsaved row's `name`/`kind` become permanently
  `readOnly`. **Move row identity and the new/persisted flag into the editor's
  working data, derived from a stable identity assigned once at creation —
  never in refs, never derived from the typed `name`.**
- **Industry evidence converges**: every mature client puts identity *in the
  data* and derives new-vs-persisted from "has a stable identity yet" — Sanity
  `_key` (mandatory per-array-item key), Backbone `cid`/`isNew`, Strapi's
  `isTemporary` content-type-builder flag. The two analogs that key off a
  user-typed name (Directus, Cal.com) are exactly the ones whose maintainers
  regret it.
- **Scope decision (大而全, consolidated under #90)**: the assigned bug (#90) is
  the *editor's* lost identity. Verification proved the issue's literal repro
  ("navigate away and back → readOnly") actually triggers a **second, distinct
  bug** in the parent route: the whole unsaved draft is discarded and re-synced
  from the server cache, so the new card *vanishes entirely* before the readOnly
  bug can manifest. The maintainer chose to fix **both** at the right altitude,
  plus the smaller defects in the same code path, and to keep the whole thing
  **under #90** rather than splitting — one issue, one proposal, one PR that
  `Closes #90`. The expanded scope is recorded here and synced onto #90 itself
  (CLAUDE.md §6), so the tracker reflects it.
- **Six defects, one coherent fix**:
  1. **(#90 core)** editor identity/new-flag in refs → lost on remount.
  2. **(parent draft-loss)** `useEffect(() => setDimensions(cfg.data.dimensions), [cfg.data])` blindly overwrites the working copy on remount **and on every background refetch** (`refetchOnWindowFocus: true` inherited from `__root.tsx:16`; `staleTime: 30s`) → silent draft loss.
  3. **(a11y)** DOM `id`/`htmlFor` derived from the user-typed name (`dim-name-${dim.name}`) → empty/duplicate names collide on one DOM id.
  4. **(a11y consistency)** locked `name` uses `readOnly` (focusable, announced) but locked `kind` uses Radix `disabled` (removed from tab order) — divergent AT semantics for one "locked" concept.
  5. **(parity)** the `Taxonomy` row layer repeats defect 1 (`taxRefs`/`newTaxIds`).
  6. **(urgentSet identity)** the urgent-set chip strip keys on `key={tax.value}` (`:315`) — two new taxonomy rows with `value=""` collide on `key=""`, and urgent membership is value-addressed, so a not-yet-named taxonomy cannot be toggled coherently.
- **Altitude**: a parent-owned **edit model** seeded **once** from the server
  snapshot (not `useEffect`-synced), holding rows as `Dimension` + two
  **client-only** fields (`_key`, `_isNew`), stripped at the save boundary. The
  editor becomes a pure controlled view keyed on `_key`. The identity/seed/strip
  logic lives in a **shared `src/lib` hook** (not `features/settings` — see
  Process), serving the Dimension and Taxonomy layers (CLAUDE.md §6: Taxonomy is
  the 2nd instance).
- **Explicitly NOT done**: re-introducing placeholder names (`dim_<n>`) — the
  #10-era approach, rejected because operators can legitimately name a dim
  `dim_foo`. Adding a server-assigned surrogate `id` to the `Dimension` proto —
  meaningless for a `repeated` field saved as one wholesale-PUT blob with no
  per-item addressing, and it would dilute the deliberate "`name` is the
  immutable machine key" discipline from #10.

---

## Problem

### #90's acceptance criteria (verbatim, so reviewers can check faithfulness)

> - Pick a fix option (or propose a 4th in a proposal).
> - `dimensions-editor.test.tsx` gains a mount → unmount → remount test that
>   fails on the current source and passes after the fix.
> - No regression in the existing tests.

This proposal picks a 4th option (data-resident identity) and meets all three.
The expanded parent-draft scope is justified below precisely *because* the
issue's own repro is pre-empted by the parent bug.

### The code as it stands

`console/src/components/dim/dimensions-editor.tsx` renders a controlled list of
`Dimension` cards. A persisted dimension's stable identifier (`name`) and `kind`
are read-only; a brand-new (unsaved) dimension's `name`/`kind` are editable —
because `name` is the immutable machine key (proto `common.ts:190-194`: *"Stable
machine key … Immutable after creation; rename = delete + recreate"*; rationale
in the #10 `flat-labels` proposal: `enriched_attrs` JSONB keys, webhook
envelopes `attrs.<name>`, and filter URLs `?severity=critical` all depend on a
stable, non-renamable `name`).

"Newness" is tracked entirely in component-instance refs:

```ts
// dimensions-editor.tsx:53-54
const refs = useRef<WeakMap<Dimension, DimRefKey>>(new WeakMap())
const newDimIds = useRef<Set<DimRefKey>>(new Set())
```

`isNew = newDimIds.current.has(idOf(dim))` (`:98-99`) drives
`readOnly={!isNew || disabled}` on the name `<input>` (`:209`) and `disabled` on
the kind Radix Select (`:221`). The `Taxonomy` layer mirrors the exact pattern
(`taxRefs`/`newTaxIds`, `:142-143`).

### Why it's broken

1. **Refs die on remount (the #90 core).** `useRef(new WeakMap())` /
   `useRef(new Set())` are per-instance. On a genuine unmount→remount they are
   fresh and empty: `idOf(dim)` misses → a new id is minted → `newDimIds`
   (empty) reports `isNew=false` → a still-unsaved dimension's `name` becomes
   `readOnly`. (`:53-64`, `:98-99`, `:209`.)

2. **The `WeakMap` is also fragile within a single mount.** Every edit creates a
   new object (`const merged = { ...prev, ...patch }`, `:69`) and re-keys the
   map by hand (`:73-75`). It works today *only because* the parent stores the
   array by reference without cloning; any future Immer/`structuredClone`/
   normalizing refetch breaks it silently.

3. **StrictMode is a red herring — a genuine remount is not.** StrictMode's
   dev double-invocation is mount-time, *exempts event-handler code*, and
   preserves `useRef` on the same fiber — so a `Set` populated by the "Add
   dimension" click is untouched (the app is wrapped in `<StrictMode>` at
   `main.tsx:36`; tests are not). The real triggers are **route navigation, a
   changed `key`, or a conditional render**. (This is why a `<StrictMode>`
   wrapper is a false-negative test — see Verification.)

4. **The parent makes the *stated* repro a different bug.** TanStack Router
   unmounts inactive routes (no `keepAlive`), so `ClassificationSettingsPage`
   remounts and `useEffect(() => setDimensions(cfg.data.dimensions ?? []), [cfg.data])`
   (`classification-settings-page.tsx:32-36`) resets the working copy to the
   *server* snapshot — which never had the unsaved row. **The new card vanishes
   entirely; it is never "readOnly".** Worse, with the global
   `refetchOnWindowFocus: true` (inherited from `__root.tsx:16`; only `staleTime`
   is overridden in `get-enrich-config.ts:14`), a background refetch on
   tab-refocus fires the same effect and **silently wipes unsaved edits without
   any navigation** — the higher-severity trust bug.

5. **Two a11y defects on the same lines.** `id={`dim-name-${dim.name}`}` and the
   matching `htmlFor` (`:205-207`) collide when two new rows both have `name=""`
   (duplicate DOM ids → wrong label targeting, ambiguous `getByLabelText`). And
   "locked" is `readOnly` on the `<input>` but `disabled` on the Radix Select
   (`:221`); the kind `<Label>` (`:217`) has **no `htmlFor`** and the Radix
   trigger no `id`, so the kind control is effectively unlabeled for AT; the
   `name_help`/`kind_help` hints are not wired via `aria-describedby`.

6. **`urgentSet` value-addressing.** The urgent-set chip strip uses
   `key={tax.value}` (`:311-336`, `:315`); two unnamed new taxonomy rows
   (`value=""`) collide on `key=""`, and membership is value-keyed
   (`dim.urgentSet.includes(tax.value)`), so a not-yet-named taxonomy can never
   be toggled coherently, and an in-place value edit silently desyncs
   `urgentSet` (only `removeTaxonomy` at `:291` reconciles today).

### Test/cite accuracy

`dimensions-editor.test.tsx` has **10** `it()` blocks (the #90 text and the
draft-v1 said "11" — corrected here). None exercises a *state-preserving*
remount; the single `unmount()` (`:185-193`) is a teardown before an independent
fresh render, not an identity-preserving remount. The substantive #91 point
(dead-test gap) stands.

### Change history (verified against `main`)

`main`'s editor uses the WeakMap+Set approach today (landed via PR #86,
`ab6fe9c6` — this is the code the proposal replaces). An **earlier #10-era
approach** used `dim_<n>` placeholder names + a `.startsWith('dim_')` heuristic
(`e5ea4c3f`); it was rejected because operators can legitimately name a
dimension `dim_foo`, which the heuristic then locks forever (silent data
corruption). The rationale is recorded in commit `c838c349`'s message — a
pre-merge branch commit **not** in `main`'s history, cited only for that
reasoning. **Do not resurrect placeholder names.**

### Impact

- Operators cannot finish creating a dimension after any remount/refetch; in the
  common path the in-progress dimension disappears with no warning.
- Silent data loss on tab-refocus is a trust bug, not just an annoyance.
- The dead-test gap (#91) let this ship.

---

## Goals / Non-goals

### Goals

| | |
|---|---|
| **G1** | New/persisted classification survives a genuine editor remount (array preserved). An unsaved row's `name`/`kind` stay editable; a persisted row stays locked. |
| **G2** | Classification is **identity-based**, never value-based: typing a new row's `name` to equal an existing name does **not** lock it mid-typing. |
| **G3** | A background refetch (tab-refocus) and an editor remount (array preserved) no longer silently overwrite the in-progress draft. (Surviving a full route navigation away-and-back would need a draft store / unsaved-changes guard — out of scope, see Non-goals.) |
| **G4** | React `key`, DOM `id`/`htmlFor` derive from a stable per-row client key. Plus: the kind control gets a programmatic label, and both help hints are wired via `aria-describedby`. |
| **G5** | One "locked" semantic for `name` and `kind` — read-only, focusable, announced (see Proposal for the Radix-specific mechanism). |
| **G6** | The Dimension and Taxonomy layers share one identity/seed/strip implementation (shared hook in `src/lib`). |
| **G7** | Client-only identity fields never reach the wire (stripped at save; asserted by a request-capture test). |
| **G8** | `urgentSet` chips key on the row `_key` (membership stays value-based, per the wire contract); the new/empty-value and in-place-edit behaviors are defined. |
| **G9** | Focus moves sensibly on add (to the new row's name input) and remove (to a neighbor), not to `<body>` (WCAG 2.4.3). |
| **G10** | Regression tests: fail-on-main repros (remount, compound edit-then-remount, duplicate-id, refetch-clobber, taxonomy parity) **plus** guard tests (collision, clean-wire, save-failure, disabled-during-save), honestly labeled. No regression in the existing 10. |

### Non-goals

- **Not** changing the `Dimension`/`Taxonomy` proto contract. `name` stays the
  immutable machine key (#10). No server surrogate `id`, no persisted `_key`
  (Alternatives A3/A4).
- **Not** reverting "rename = delete + recreate" — a deliberate constraint.
- **Not** introducing a form library (RHF/TanStack Form) — does not solve
  identity-across-remount (A5).
- **Not** building collaborative draft sync or a multi-step undo stack. An
  "unsaved-changes navigation guard" is out of scope (noted as a possible
  follow-up).

---

## Proposal

### Core principle

Row identity and "new vs persisted" must live **in the data, derived from a
stable identity assigned once at creation** — not in component refs, and not
computed from the mutable, user-typed `name`.

### Client-side editable row type (client-only fields)

```ts
// console/src/lib/editable-rows.ts  (new — SHARED layer, see Process)
// `_key` is readonly (minted once, never reassigned).
type EditableTaxonomy  = Taxonomy  & { readonly _key: string; _isNew: boolean }
type EditableDimension = Omit<Dimension,'taxonomy'> &
  { readonly _key: string; _isNew: boolean; taxonomy: EditableTaxonomy[] }
```

- `_key`: stable client identity minted once (`crypto.randomUUID()`) when the
  row enters the working set — at seed time for loaded rows, at "add" time for
  new rows. Used as the React `key`, the base for DOM `id`/`htmlFor`, and the
  `urgentSet` chip key. Never derived from `name`; never regenerated on edit.
  (Verified available in the test env — jsdom 29.1.1 / Vitest 4.1.8 — and the
  app is a client-only SPA, so there is no SSR path; a counter fallback in the
  id helper is the cheap insurance noted in Risks.)
- `_isNew`: set once (`false` at seed, `true` at add); read directly to gate
  editability — immune to what the user types (G2). This is Strapi's
  `isTemporary` flag.

Because `_key`/`_isNew` ride **in the row**, the existing
`merged = { ...prev, ...patch }` spread carries them forward for free — the
WeakMap's only load-bearing job (manual re-keying) disappears with no behavioral
loss. Nothing else reads the WeakMap.

### Parent: seed once, never blind-sync (fixes G3 + the anti-pattern)

`ClassificationSettingsPage` stops doing
`useEffect(() => setDimensions(cfg.data.dimensions), [cfg.data])` (the react.dev
"don't mirror server state with an Effect" anti-pattern). Committed mechanism
(no longer a coin-flip):

- The page already early-returns a spinner while `cfg.isPending`
  (`:77-84`), so `cfg.data` is defined when the editor section renders. Render a
  **gated child** in that branch and **seed from props in a `useState`
  initializer** — runs exactly once per mount, never reads a transient
  `undefined`, never re-fires on refetch. Each loaded row is stamped
  `_key`/`_isNew=false` (and its taxonomy).
- Add the per-query override `refetchOnWindowFocus: false` to `enrichConfigQuery`
  (it currently *inherits* the root `true`; only `staleTime` is set). A long
  `staleTime` is **not** a substitute — it does not stop a focus refetch once
  stale — so it is not offered as a co-equal mitigation.
- Keep the seeded server snapshot as `initialData` alongside the working
  `modifiedData`, used to (a) reconcile `_isNew` on save and (b) optionally show
  a dirty marker. A full "reload from server" affordance is an **optional
  follow-up**, and if added it must guard against clobbering a dirty draft.

### Save round-trip (fixes G7; closes the concurrent-edit hole the review found)

`handleSave` strips the client-only fields with a single row-level helper and
sends plain `Dimension[]` (wire shape `{ dimensions, promptTemplate }`
unchanged). The strip is **row-level only** — it deletes `_key`/`_isNew` from
each Dimension and each Taxonomy and does **not** recurse into
`displayName.entries` (whose keys are operator-controlled BCP-47 tags). It is
**load-bearing, not defensive**: the proto types here are interface-only and the
PUT body is `JSON.stringify`'d verbatim (`api-client.ts:49`), so any extra field
ships unless stripped. A literal compile-time brand is structurally infeasible
here — `EditableDimension` is a structural subtype of `Dimension`, so it is
assignable to the fixed proto `Dimension[]` param regardless of any phantom tag.
The enforcement is therefore three layers: (1) `toWireDimensions` is the
**single producer** the save AND preview paths both call; (2) a unit test that
deep-equals the output to a plain `Dimension[]` (fails on **any** extra key, not
just `_key`/`_isNew` by name); (3) a page-level integration test that captures
the real PUT body and asserts it is client-field-free. `_key` is `readonly` so
it cannot be reassigned mid-edit. (Whether the Go handler is strict-protojson →
400 or DiscardUnknown → silent leak is to be confirmed in the PR; either way the
strip is required.)

On success, reconcile **imperatively from the mutation's `onSuccess`** (not by
observing `cfg.data`, whose identity churns on every refetch): for each row that
was in the submitted snapshot, match by `_key` and flip `_isNew=false` **in
place**, and refresh `initialData` to the saved config — **without discarding
rows or edits the operator added after clicking Save**. The Save button is
already gated on `save.isPending` (`:155`); the editor inputs are not, so the
in-place reconcile (rather than a wholesale re-seed) is what prevents losing
edits made during the in-flight save. A failed save does **not** reconcile and
does **not** flip `_isNew`.

### Editor: pure controlled view (fixes G1, G2, G4, G5, G6, G8)

Drop all refs (`refs`/`newDimIds`/`seqRef`/`taxRefs`/`newTaxIds`/`taxSeq`). The
components read:

- `key={row._key}` (stable across edits/reorder — focus preserved).
- lock from `row._isNew` (data-resident — survives remount, immune to collision).
- `id={`dim-name-${row._key}`}` / `htmlFor` from `_key`; likewise the kind
  control gets `id={`dim-kind-${row._key}`}` + a matching `<Label htmlFor>`, and
  each control gets `aria-describedby` → its help `<p id=...>`.
- `urgentSet` chips key on the taxonomy row `_key`; membership stays value-based
  (`urgentSet` is `repeated string` of `taxonomy.value` on the wire,
  `common.ts:207-208`). Rule: a taxonomy with `value=""` is not eligible for
  urgent until named; editing a NEW taxonomy's `value` in place updates any
  matching `urgentSet` entry (the reconcile the current code only does on
  delete).

**One locked semantic (G5), corrected for Radix.** The kind control is a Radix
Select (a `<button role="combobox">`, `select.tsx`), not a native `<select>`.
Radix `disabled` removes it from the tab order and adds no "why locked" signal —
inconsistent with the `readOnly` (focusable, announced) name input. Decision:
make **both** read-only-style. Name `<input>`: `readOnly`. Kind: a focusable,
inert presentation — `aria-disabled` + blocked `onValueChange` (or render the
locked kind as static read-only text, since a persisted kind is genuinely
immutable). We do **not** pass Radix `disabled`. (No native form submission is
involved — `handleSave` serializes from React state, `kind` is always present —
so the "disabled controls aren't submitted" caveat is irrelevant here.)

### Focus management (G9)

On add, move focus to the new row's name input (a `ref` keyed on the new
`_key`); on remove, move focus to a sensible neighbor (previous row's delete
button or the Add button) so keyboard users are not dropped to `<body>`.

### Shared hook (G6) + layer placement

Extract seed/add/remove/strip + `_key`/`_isNew` into a generic
`useEditableRows<T>`. It **must** live in `src/lib/` (or `src/components/dim/`),
**not** `features/settings/lib/`: the consumer `components/dim/dimensions-editor.tsx`
is in the shared layer, and the dependency-cruiser `shared-no-up` rule forbids
`components|lib|proto → features`. The `EditableDimension`/`EditableTaxonomy`
types and seed/strip helpers sit in the shared layer; only the parent edit-model
wiring lives in `features/settings/`. (I18nInput is out of scope — its rows key
on the locale tag, which is stable and deduped in `collectLocales`; the hook does
not extend to it.)

### Why this is correct under every verified failure mode

| Scenario (verified) | Current (refs) | This proposal |
|---|---|---|
| Genuine editor remount, array preserved | ✗ locks new row | ✓ `_isNew` rides in data |
| New row typed to collide with a saved name | ⚠ ok by luck (ref) | ✓ `_isNew` independent of value |
| Background refetch on tab-refocus | ✗ silent draft wipe | ✓ seed-once + refetch off |
| Route navigate away + back (unsaved draft) | ✗ vanishes | ⚠ still vanishes — out of scope (needs a draft store / nav guard; see Non-goals) |
| Edit during in-flight save | n/a | ✓ editor disabled while pending + in-place `_key` reconcile |
| Two new rows, DOM id uniqueness | ✗ duplicate `id=""` | ✓ `_key`-based ids |
| Two unnamed taxonomies, urgent chip key | ✗ duplicate `key=""` | ✓ `_key` chip keys |
| Wire payload contains client fields | n/a | ✓ single-producer strip + deep-equal + PUT-capture |
| Save → new becomes persisted | n/a | ✓ in-place reconcile sets `_isNew=false` |

---

## Alternatives considered

- **A1 — Placeholder names (`dim_<n>` + `.startsWith('dim_')`).** **Rejected
  (already tried, #10-era, `e5ea4c3f`).** An operator can legitimately name a
  dimension `dim_foo`, which the heuristic locks forever (silent corruption;
  rationale in `c838c349`). Do not resurrect.

- **A2 — Snapshot-by-name membership (`isNew = !serverNameSet.has(dim.name)`).**
  **Rejected.** Value-based, not identity-based: a new row typed to match a
  saved name flips to "persisted" and locks mid-typing — the Formik #2292
  "baseline tracks the value" failure mode. Also doesn't solve keying/DOM-id.

- **A3 — Server-assigned surrogate `id` on `Dimension`.** **Rejected for this
  data model.** The closest analogs (Twenty, Saleor, PostHog) use a server id —
  but their fields are *individually addressable server rows*. attune's
  dimensions are a `repeated` field in one config blob saved by wholesale PUT,
  with no per-item endpoint, row, or FK. A server-minted id references nothing
  and would churn on refetch (reproducing the bug); it also dilutes the #10
  "`name` is the immutable machine key" discipline.

- **A4 — Persisted, immutable `_key` in the proto (Sanity `_key` style).**
  **Deferred.** The principled long-term identity *if* attune ever needs
  cross-client/reorder/diff/audit identity in the saved document. For the
  current single-editor, wholesale-PUT case, a **client-only** `_key` suffices
  and avoids a contract change.

- **A5 — Adopt React Hook Form / TanStack Form `useFieldArray`.** **Rejected as
  a fix.** `field.id` lives in library-internal state regenerated on
  reinit/remount (maintainer: "don't rely on those id"); you'd still need the
  data-resident `_key`. Adds a dependency and a large refactor for no identity
  gain.

- **A6 — Lift `newDimIds` to the route `useState`.** Partial (issue option 2).
  Survives remount only if the route doesn't remount (it does, on nav), stores a
  redundant flag to keep in sync, and leaves the collision + a11y defects.
  Subsumed by the edit-model approach.

- **A7 — Fix only the editor; split draft-loss into a separate issue.** This is
  the **baseline** and is faithful to #90's narrowest reading. **Not chosen.**
  The parent draft-loss is the bug #90's own repro actually hits and the
  refetch-clobber is a silent-data-loss trust bug, so the maintainer chose to
  consolidate everything under #90 (one issue, one proposal, one PR) rather than
  fragment a single user-visible failure across two trackers. The editor-only
  fix remains the floor the consolidated scope builds on.

---

## Risks / tradeoffs

- **R1 — Client-only fields leaking to the wire.** The proto types are
  interface-only and the PUT body is `JSON.stringify`'d verbatim
  (`api-client.ts:49`), so the strip is structural, not cosmetic. A compile-time
  brand can't enforce it (a structural subtype is assignable to the fixed proto
  `Dimension[]` param), so the mitigation is three layers: `toWireDimensions` as
  the single producer both save and preview call; a unit test deep-equaling the
  output to a plain `Dimension[]` (catches any extra key); and a page-level test
  capturing the real PUT body. `_key` is `readonly`. The `CLIENT_FIELDS` list is
  the single source of truth shared by the type and the runtime strip.
- **R2 — Identifier edit during an in-flight save.** The editor is disabled
  while `save.isPending`, so a just-submitted row's identifier can't be edited
  mid-save (which would lock it on success to a value the server never
  received). The `onSuccess` reconcile additionally flips `_isNew` in place by
  `_key` (not a wholesale re-seed). Both are tested (a deferred-PUT page test).
- **R3 — Restructuring the settings page edit flow** touches a shipped surface.
  Mitigation: keep the wire shape identical; lean on the existing 10 tests plus
  the new ones; land as one coherent change (see plan).
- **R4 — `refetchOnWindowFocus: false`** reduces background freshness for this
  one operator-edited config. Acceptable (TkDodo's "React Query and Forms"
  recommends treating the cache as initial data for edit forms); a manual reload
  affordance can cover refresh if added later.
- **R5 — Dependency-cruiser placement** is **resolved**: the hook/types live in
  `src/lib` (shared), never `features/settings/lib`, because the consumer is a
  shared component. `pnpm arch` gates this.
- **R6 — `crypto.randomUUID`** is verified present in the jsdom test env and the
  app is a client-only SPA; a counter fallback in the id helper is cheap
  insurance and removes any env assumption.
- **R7 — Duplicate empty/identical names across two new rows.** `_key` makes
  them distinct in the UI (DOM id, React key), but two unsaved rows can both hold
  `name=""` (or the same name) until Save. The server validates this
  (`DIM_NAME_DUP`/`DIM_NAME_RESERVED`/`DIM_NAME_FORMAT`, `common.ts:66-73`) and
  the existing `onError` toast surfaces it. Acceptable as-is; an optional inline
  hint on the offending row is a follow-up, not a blocker.

---

## Implementation plan

The editor cannot read `_key`/`_isNew` unless the parent stamps them, and
stamping them *is* the seed-once change — so the editor fix and the parent
seed-once fix are **one coherent change**, not two independently-landable PRs
(the review correctly flagged a false split). Plan:

1. **PR 1 — the fix (`Closes #90`, covering all six defects).**
   - Shared `src/lib/editable-rows.ts`: `EditableDimension`/`EditableTaxonomy`
     types (readonly `_key`), pure helpers (`seedDimensions`/`newDimension`/
     `newTaxonomy`/`toWireDimensions`/`markPersisted`/`mergePromotedValue`), id
     helper with fallback.
   - Parent: replace the `useEffect`-sync with a gated child + `useState`
     initializer seed-once; add `refetchOnWindowFocus: false`; strip on save;
     in-place `_isNew` reconcile in `onSuccess`.
   - Editor: drop all refs; controlled `_key`/`_isNew`; DOM-id/`htmlFor` from
     `_key`; kind label + `aria-describedby`; unified read-only semantic;
     `urgentSet` chip keys on `_key`; focus management on add/remove. Same
     treatment for the Taxonomy layer via the shared hook.
   - Tests per Verification. `Closes #90`.
   - CHANGELOG `[Unreleased]` `### Fixed` — both defects.
2. **PR 2 (optional) — UX polish**: an explicit "reload from server" affordance
   + dirty indicator + (optionally) an unsaved-changes navigation guard. If it
   ships user-facing behavior it carries its own `### Fixed`/`### Changed`
   entry; a pure refactor/test slice may claim the §2 exemption (called out in
   the PR description).

## Verification

**Honest test taxonomy** (the #91 lens: do not ship guard tests as fail-on-main
repros):

- **Fail-on-main repros** (red on `main`, green after):
  1. **Editor remount** — a `RemountHarness` holding `value` in parent
     `useState` and rendering `<DimensionsEditor key={k} …>` with a "remount"
     button that bumps `k`. Add a dim → assert editable → bump `k` → assert
     **still editable**. A key-bump forces a real fiber unmount→remount; refs
     reset on `main` → readOnly. **Must not** be wrapped in `<StrictMode>`
     (false negative).
  2. **Compound edit-then-remount** — add dim, type `sev` into name, bump `k`,
     assert name still `readOnly=false` **and** retains `sev` (the exact
     operator scenario).
  3. **Duplicate DOM id** — two empty-name new rows;
     `document.querySelectorAll('[id^="dim-name-"]')` yields **unique** ids
     (precise; survives i18n label renames, unlike `getByLabelText`).
  4. **Refetch-clobber (no navigation)** — render the page, add a dim,
     `queryClient.invalidateQueries(['console','enrich-config'])` (or re-resolve
     the MSW GET), assert the draft survives. Easiest to build and the primary
     G3 test.
  5. **Taxonomy parity** — remount + duplicate-id mirrored at the taxonomy
     layer.
- **Guard tests** (green on `main`; they protect the new code, not repros — say
  so):
  - **Name collision (G2)** — type a new row's name to equal a saved name;
    assert it stays editable. (On `main` `isNew` is set-tracked, not value-
    derived, so this is green there too; it guards against a future regression to
    A2.)
  - **Clean wire (G7)** — MSW `request.json()` capture (pattern at
    `update-enrich-config.test.ts:24`) asserts no `_key`/`_isNew`.
  - **Save-failure** — MSW 400 (pattern at `update-enrich-config.test.ts:79`);
    assert the new row stays `_isNew`/editable and the draft is intact.
  - **Disabled-during-save** — a deferred PUT holds the save in flight; assert
    the editor is disabled (the Add control is gone) and re-enabled after.
  - **Remove-a-new-row** — seed `[persisted, new]`, remove the persisted row,
    assert the surviving new row is still editable (and the inverse).
  - **Locked semantic (G5)** — the locked kind exposes the chosen
    read-only/`aria-disabled` affordance.
- **Router-navigation draft survival** — **not built / out of scope.** A full
  route unmount re-seeds the form from cache (the route has no `keepAlive`), so
  the unsaved draft is lost on navigate-away regardless of this fix; preserving
  it would need a draft store or an unsaved-changes nav guard (Non-goals). The
  G3 coverage here is the refetch-clobber test above (a remount that preserves
  the array), not a route-navigation test.

**Gates / process**:
- `pnpm tsc -b --noEmit`, `pnpm biome check`, `pnpm vitest run --coverage`,
  `pnpm arch`, `pnpm exec vite build` — cite outputs in the PR (CLAUDE.md §9).
- **Coverage**: preserve the existing `dimensions-editor.tsx: { lines: 80 }`
  floor (`vite.config.ts`) post-rewrite, and **add per-file `thresholds`
  entries** for the new `src/lib/editable-rows.ts` so the opt-in Console
  Coverage gate actually protects it (its add/remove/strip branches are exactly
  the under-tested kind).
- Tests assert **behavior**, not implementation, for mutation-resistance (#91).
- Manual: no real-LLM e2e (UI-only); a recorded Console walkthrough (add →
  remount editor → still editable; tab away/back → draft intact; save → locks).

## References

- Issue #90 (+ acceptance criteria, quoted above); PR #89 (4th-round review that
  filed it); #91 (mutation testing).
- #10 `flat-labels` proposal (`docs/proposals/2026/06/2026-06-07-flat-labels.md`)
  — the immutable-`name`, rename=delete+recreate discipline.
- Source: `console/src/components/dim/dimensions-editor.tsx`,
  `console/src/features/settings/components/classification-settings-page.tsx`,
  `console/src/features/settings/api/{get,update}-enrich-config.ts`,
  `console/src/routes/__root.tsx`, `console/src/main.tsx`,
  `console/src/lib/api-client.ts`, `console/src/components/ui/select.tsx`,
  `console/src/proto/attune/v1/common.ts`, `console/vite.config.ts`,
  `console/.dependency-cruiser.cjs`, `console/src/testing/router-utils.tsx`.
- History (verified on `main`): `e5ea4c3f` (#10, placeholder heuristic),
  `ab6fe9c6` (#86, WeakMap — current `main`), `c838c349` (pre-merge branch,
  rationale only).
- Industry: Sanity `_key`; Backbone `cid`/`isNew`; Strapi `isTemporary`; Twenty
  / Saleor / PostHog (server-id, addressable rows); Directus #2711 & Cal.com
  (name-as-key, regretted); Formik #2292 (value-vs-identity); RHF `useFieldArray`
  (`field.id` not remount-stable); react.dev — StrictMode, useId, Preserving and
  Resetting State, You Might Not Need an Effect; TkDodo "React Query and Forms";
  MDN `readonly` vs `disabled`; WCAG 2.4.3 (Focus Order).

---

## Appendix A — Review hardening (v2)

This proposal was hardened against a **7-lens parallel adversarial review**
(React correctness, scope/history, test rigor/#91, accessibility, CLAUDE.md
process, completeness, devil's-advocate). All seven returned
*approve-with-changes*; every cited `file:line` was independently re-verified as
accurate (refs at `:53-54`, `isNew` gating at `:99`/`:209`/`:221`, the parent
blind-sync at `:32-36`, `staleTime:30s`, root `refetchOnWindowFocus:true`, the
`dim-name-${dim.name}` collision, the Taxonomy mirror, and the
no-`id`-on-`Dimension` proto fact). Nine blocking issues were raised and each is
resolved in v2:

| # | Blocking finding | Resolution in v2 |
|---|---|---|
| 1 | Re-seed-after-save would clobber edits made during the in-flight save (re-introducing the silent-overwrite class) | Reconcile `_isNew` **in place by `_key`** in `onSuccess` (not a wholesale re-seed); editor disabled while `save.isPending`; deferred-PUT guard test. |
| 2 | Seed-once mechanism left undecided; one option unsafe | Committed to **gated child + `useState` initializer** seeded from props after the `cfg.isPending` guard; re-seed driven imperatively from `onSuccess`, never from observing `cfg.data`. |
| 3 | History attribution wrong (placeholder "introduced in #86, removed in c838c349") | Corrected on `main`: placeholder = #10 (`e5ea4c3f`); WeakMap = #86 (`ab6fe9c6`, current `main`); `c838c349` is a pre-merge branch commit cited for rationale only. |
| 4 | PR-1/PR-2 not independently landable (editor needs parent to stamp `_key`) | Merged into **one coherent PR-1**; PR-2 is optional UX polish only. |
| 5 | G2/G7 claimed "fail on main" but are green there (guard tests) | Verification split into **fail-on-main repros vs guard tests**, honestly labeled (#91 anti-dead-test). |
| 6 | Core remount test not constructible with the existing Harness | Specified **`RemountHarness`** (parent-held `value` + `key`-bump button); explicitly **not** under `<StrictMode>` (false negative). |
| 7 | "Draft survives" test had no seam | Built the **refetch-clobber** test (invalidate query, no router). Full route-navigation draft survival is out of scope (needs a draft store / nav guard — Non-goals), so no router test is claimed. |
| 8 | Shared-hook placement (`features/settings/lib`) would fail `pnpm arch` | Pinned to **`src/lib`** (consumer is a shared component; `shared-no-up` forbids shared→features). |
| 9 | Scope inversion (P2 → route refactor) without issue-owner ratification | Recorded as **maintainer-ratified 大而全**, consolidated **under #90** (one issue, one proposal, one PR `Closes #90`); decision synced onto #90. |

Substantive non-blocking improvements also folded in: kind control is a **Radix
Select** (not native `<select>`) → unified read-only semantic without Radix
`disabled`; added the **6th defect** (`urgentSet` value-addressing); hardened the
strip as a single-producer boundary with a deep-equal unit test + a PUT-capture
integration test + `CLIENT_FIELDS` single source (interface-only protos +
`JSON.stringify` passthrough make it load-bearing); strip is **row-level** (no
recursion into `displayName.entries`); corrected the test count to **10**;
verified `crypto.randomUUID` in jsdom; added **focus management (G9)** and the
**kind-label + `aria-describedby`** a11y fixes; dropped "long `staleTime`" as a
non-fix; scoped out `I18nInput` (locale-keyed rows).

## Appendix B — Pre-Accept checklist (CLAUDE.md §10 / proposal gate)

- [x] **Sync the expanded scope onto #90** — done (issue #90 comment recording
      the six defects, the chosen design, and a link to this proposal).
- [x] Maintainer accepted ("开始开发"/"完整开发"); implemented on branch
      `fix/90-dim-editor-identity-remount`.
- [x] `[Unreleased]` `### Fixed` entry under #90 added.
- [x] Per-file vitest `thresholds` for `src/lib/editable-rows.ts` added;
      `dimensions-editor.tsx: { lines: 80 }` preserved.

### Implementation notes (as built)

- Identity lives in `src/lib/editable-rows.ts` as **pure helpers** (`newKey`,
  `newDimension`, `newTaxonomy`, `seedDimensions`, `toWireDimensions`,
  `markPersisted`) rather than a `useEditableRows` hook — the editor is fully
  controlled and the parent owns the `useState`, so a state-owning hook would
  duplicate that. Same shared-identity outcome (G6), less state.
- Locked **Kind** renders as a read-only `<Input>` (focusable, announced),
  matching the locked Name — not a disabled Radix Select. Editable Kind stays a
  Radix Select.
- The editor is disabled while `save.isPending` (prevents a mid-save identifier
  edit from locking to an unsubmitted value); the `onSuccess` reconcile flips
  `_isNew` in place by submitted `_key` (dim + taxonomy sets).
- `enrichConfigQuery` keeps its shared definition; `refetchOnWindowFocus: false`
  is applied as a scoped override on the Settings page's `useQuery` only (no
  blast radius to other consumers).

### Review-driven additions (post 6-lens code review)

A 6-lens adversarial code review (per-finding verified) surfaced 19 real issues,
all addressed:

- **Promote→editor desync (the one regression the review caught):** removing the
  blind `useEffect` sync meant a value promoted in the Suggested Values panel no
  longer reached the seeded editor, and a later save would clobber it. Fixed with
  `mergePromotedValue` + an `onPromoted` callback that folds the promoted value
  into the draft (persisted, no re-seed) — draft isolation preserved.
- **Disclosure a11y:** the collapse chevron is now the real disclosure button
  (`aria-expanded`/`aria-controls`/`aria-label`), not a bare-div onClick.
- **`urgentSet` integrity (defect 6, fully closed):** in-place value renames remap
  the matching `urgentSet` entry; empty-value rows render no urgent chip; chips
  carry `aria-pressed`; the taxonomy value input gets an `aria-label`.
- **Draft isolation:** `seedDimensions` deep-clones (`structuredClone`) so the
  draft never aliases the query cache.
- **Test hardening:** `markPersisted` now exercises the true→false flip (was a
  no-op vs the identity mutant); the urgent-chip test asserts the wire value not
  the display label; added page-level clean-wire (PUT capture), save-locks, and
  save-failure-keeps-editable guards; relabeled the persisted-remount guard.

A **second** review round (same harness, on the fixed code) found 10 more, all
addressed — including one **blocker** the first pass missed:

- **Blocker — taxonomy remove was dead:** `onRemove` fired two sequential
  `onChange` calls (taxonomy then urgentSet) that both read the same captured
  array, so the second clobbered the first and the value never left the
  taxonomy. Collapsed into one atomic `onChange({ taxonomy, urgentSet })`; the
  removal tests now assert the taxonomy array actually shrank (the blind spot
  that hid it). This bug pre-dated the rewrite and was carried forward — now
  fixed.
- **Promote-wiring had no test:** deleting the `onPromoted` wiring left every
  test green. Added a page-level integration test that drives a promote and
  asserts the value rides the next save's PUT (client-field-free).
- **a11y:** the disclosure control is now the whole title row (a real `<button>`
  self-labeled by its text, larger target), `aria-controls` only while open;
  delete/remove buttons interpolate the dim name / value into their labels.
- **`mergePromotedValue`:** first-matching-dimension only.

A **third** round (on the round-2 code) found 6 more (all minor/nit) — severity
converged with no blocker/major — all addressed:

- `mergePromotedValue` now threads the server `displayName` (the POSTed key)
  through `onPromoted`, so the draft mirrors server truth and a later save can't
  silently rewrite the persisted i18n key.
- The `urgentSet` rename remap de-dups (rename onto an already-urgent value no
  longer ships a duplicate).
- Removing a taxonomy row moves focus to the Add-value button (WCAG 2.4.3),
  matching the dimension-remove path.
- Per-instance `aria-label` on taxonomy value inputs; `newKey` fallback branch
  now covered; the clean-wire page test asserts a non-empty dimensions array so
  an empty-array mutant fails.

Rounds **4–6** drove convergence (severity monotonically falling: round 4 all
minor, round 5 all nit, round 6 one optional nit). Net of those rounds: the
editor is disabled while `save.isPending` (closing a mid-save identifier-edit
race); the value `aria-label` was reverted to a stable string (interpolating the
live value re-announced on every keystroke — round 6 independently confirmed the
stable choice); `mergePromotedValue` trims the value and threads the server
`displayName`; the proposal's G3 / route-navigation claims were corrected to
match what's actually delivered (editor-remount identity + refetch-clobber;
full route-navigation draft survival is out of scope); added characterization
tests for the preview clean-wire path, the `mergePromotedValue` first-match +
trim guards, the urgent-rename-to-empty path, and `aria-expanded`; and grouped
the urgent chips under their label (`role="group"`/`aria-labelledby`). Round 6
found **zero correctness defects** and re-confirmed prior decisions — the review
loop converged.
