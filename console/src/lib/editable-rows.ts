import type { Dimension, I18nString, Taxonomy } from '@/proto/attune/v1/common'

// Editable row identity for the Dimensions editor.
//
// "Is this row new (identifier editable) vs persisted (identifier locked)"
// must live IN the working data, not in component refs — refs are destroyed
// on remount and re-keyed objects break a WeakMap, so a ref-based new/persisted
// flag is lost the moment the editor unmounts/remounts (issue #90). Identity
// here is a client-minted `_key` assigned once at row creation (used as the
// React key and the base for DOM ids — never the user-typed `name`), plus an
// `_isNew` flag read directly to gate editability (immune to what the operator
// types, unlike deriving "new" from whether the name matches a server value).
//
// Both fields are client-only and are stripped before the array is sent to the
// server (`toWireDimensions`): the proto types are interface-only and the
// request body is JSON-stringified verbatim, so any un-stripped field ships.
// `_key` is `readonly` — minted once, never reassigned (reassigning it would
// remount the row and lose focus, the exact #90 class of bug).

// Single source of truth for the client-only field names — used by both the
// type and the runtime strip so they cannot drift.
const CLIENT_FIELDS = ['_key', '_isNew'] as const
type ClientField = (typeof CLIENT_FIELDS)[number]

export interface EditableTaxonomy extends Taxonomy {
  readonly _key: string
  _isNew: boolean
}

export interface EditableDimension extends Omit<Dimension, 'taxonomy'> {
  readonly _key: string
  _isNew: boolean
  taxonomy: EditableTaxonomy[]
}

let seq = 0

// newKey mints a stable client identity. Prefers crypto.randomUUID (present in
// every supported browser and the jsdom test env); falls back to a monotonic
// counter so the helper never throws in an exotic runtime.
export function newKey(): string {
  const c = globalThis.crypto
  if (c && typeof c.randomUUID === 'function') return c.randomUUID()
  seq += 1
  return `row-${seq}`
}

// newTaxonomy is the blank-slate Taxonomy a fresh value row starts with —
// Value + DisplayName empty, marked new so its Value stays editable until save.
export function newTaxonomy(): EditableTaxonomy {
  return {
    value: '',
    displayName: { entries: { default: '' } },
    examples: [],
    _key: newKey(),
    _isNew: true,
  }
}

// newDimension is the blank-slate Dimension a fresh card starts with — Name +
// DisplayName empty, marked new so Name + Kind stay editable until save.
export function newDimension(): EditableDimension {
  return {
    name: '',
    displayName: { entries: { default: '' } },
    kind: 'single',
    taxonomy: [],
    urgentSet: [],
    required: false,
    examples: [],
    extractionHint: '',
    _key: newKey(),
    _isNew: true,
  }
}

// seedDimensions wraps server-loaded Dimensions into the editable model,
// minting a fresh `_key` per row (and per taxonomy entry) and marking
// everything persisted. Call this ONCE when the editor is seeded from the
// server snapshot — never on every render/refetch (that is the blind-overwrite
// anti-pattern that clobbers in-progress drafts).
//
// The input is deep-cloned: `initial.dimensions` is the exact object held in
// the TanStack Query cache, and the editor's draft must not alias (let alone
// mutate) it.
export function seedDimensions(dims: Dimension[]): EditableDimension[] {
  return dims.map((d) => {
    const copy = structuredClone(d)
    return {
      ...copy,
      _key: newKey(),
      _isNew: false,
      taxonomy: (copy.taxonomy ?? []).map((tx) => ({ ...tx, _key: newKey(), _isNew: false })),
    }
  })
}

function omitClientFields<T extends object>(row: T): Omit<T, ClientField> {
  const out = { ...row } as Record<string, unknown>
  for (const f of CLIENT_FIELDS) delete out[f]
  return out as Omit<T, ClientField>
}

// toWireDimensions strips the client-only identity fields, row-level only — it
// does NOT recurse into displayName.entries, whose keys are operator-controlled
// locale tags. This is the single save/preview boundary; everything else on the
// row (including any future proto field) passes through untouched.
export function toWireDimensions(rows: EditableDimension[]): Dimension[] {
  return rows.map((r) => ({
    ...(omitClientFields(r) as Omit<EditableDimension, ClientField>),
    taxonomy: r.taxonomy.map((tx) => omitClientFields(tx) as Taxonomy),
  }))
}

// markPersisted flips `_isNew=false` for exactly the rows (and taxonomy rows)
// whose `_key` is in the submitted sets — used after a successful save to lock
// the identifiers of the rows that round-tripped, WITHOUT disturbing rows the
// operator added during the in-flight save.
export function markPersisted(
  rows: EditableDimension[],
  sentDimKeys: ReadonlySet<string>,
  sentTaxKeys: ReadonlySet<string>,
): EditableDimension[] {
  return rows.map((r) => ({
    ...r,
    _isNew: sentDimKeys.has(r._key) ? false : r._isNew,
    taxonomy: r.taxonomy.map((tx) => ({
      ...tx,
      _isNew: sentTaxKeys.has(tx._key) ? false : tx._isNew,
    })),
  }))
}

// mergePromotedValue folds a server-promoted suggested value (an off-list
// taxonomy value the operator promoted in the Suggested Values panel) into the
// editor's draft — appending it to the matching dimension's taxonomy as a
// persisted (`_isNew=false`) row. It reconciles the draft WITHOUT re-seeding
// from the cache, so concurrent edits survive. No-op if the dimension isn't in
// the draft or already has the value.
export function mergePromotedValue(
  rows: EditableDimension[],
  dimName: string,
  value: string,
  // The displayName the panel POSTed (and the server stored) — threaded through
  // so the draft mirrors server truth and a later wholesale save doesn't rewrite
  // the persisted i18n key. Falls back to a `default` entry if absent.
  displayName?: I18nString,
): EditableDimension[] {
  // Match the server, which trims the value before storing.
  const v = value.trim()
  let done = false
  return rows.map((r) => {
    // Only the FIRST dimension whose name matches is affected — a draft may
    // hold a new dim the operator named identically to a server dim.
    if (done || r.name !== dimName) return r
    done = true
    if (r.taxonomy.some((tx) => tx.value === v)) return r
    const promoted: EditableTaxonomy = {
      value: v,
      displayName: displayName ?? { entries: { default: v } },
      examples: [],
      _key: newKey(),
      _isNew: false,
    }
    return { ...r, taxonomy: [...r.taxonomy, promoted] }
  })
}
