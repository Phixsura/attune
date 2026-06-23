import { describe, expect, it, vi } from 'vitest'
import {
  type EditableDimension,
  markPersisted,
  mergePromotedValue,
  newDimension,
  newKey,
  newTaxonomy,
  seedDimensions,
  toWireDimensions,
} from '@/lib/editable-rows'
import type { Dimension } from '@/proto/attune/v1/common'

function dim(over: Partial<Dimension> = {}): Dimension {
  return {
    name: 'severity',
    displayName: { entries: { default: 'Severity' } },
    kind: 'single',
    taxonomy: [],
    urgentSet: [],
    required: false,
    examples: [],
    extractionHint: '',
    ...over,
  } as Dimension
}

describe('editable-rows', () => {
  it('newDimension / newTaxonomy are marked new with a unique key', () => {
    const a = newDimension()
    const b = newDimension()
    expect(a._isNew).toBe(true)
    expect(a._key).toBeTruthy()
    expect(a._key).not.toBe(b._key)
    const tax = newTaxonomy()
    expect(tax._isNew).toBe(true)
    expect(tax._key).toBeTruthy()
  })

  it('newKey falls back to a unique counter when crypto.randomUUID is unavailable', () => {
    vi.stubGlobal('crypto', {})
    try {
      const a = newKey()
      const b = newKey()
      expect(a).not.toBe(b)
      expect(a).toMatch(/^row-/)
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('seedDimensions marks loaded rows persisted and keys every row + taxonomy', () => {
    const rows = seedDimensions([
      dim({ name: 'severity', taxonomy: [{ value: 'P0', examples: [] }] }),
    ])
    expect(rows[0]._isNew).toBe(false)
    expect(rows[0]._key).toBeTruthy()
    expect(rows[0].taxonomy[0]._isNew).toBe(false)
    expect(rows[0].taxonomy[0]._key).toBeTruthy()
    expect(rows[0].taxonomy[0]._key).not.toBe(rows[0]._key)
  })

  it('seedDimensions deep-clones so the draft does not alias the server snapshot', () => {
    const source = dim({
      name: 'severity',
      urgentSet: ['P0'],
      taxonomy: [{ value: 'P0', examples: [] }],
    })
    const rows = seedDimensions([source])
    expect(rows[0].urgentSet).not.toBe(source.urgentSet)
    expect(rows[0].taxonomy[0]).not.toBe(source.taxonomy[0])
    rows[0].urgentSet.push('P1')
    expect(source.urgentSet).toEqual(['P0']) // source untouched
  })

  it('seedDimensions tolerates a missing taxonomy array', () => {
    const rows = seedDimensions([{ ...dim(), taxonomy: undefined as unknown as [] }])
    expect(rows[0].taxonomy).toEqual([])
  })

  it('toWireDimensions strips client fields and yields a plain Dimension[] (deep-equal)', () => {
    const input = dim({ name: 'severity', taxonomy: [{ value: 'P0', examples: [] }] })
    const wire = toWireDimensions(seedDimensions([input]))
    // Deep-equal to the original plain Dimension — catches ANY extra key, not
    // just _key/_isNew by name.
    expect(wire).toEqual([input])
    // Belt: the serialized payload is byte-clean.
    expect(JSON.stringify(wire)).not.toMatch(/_key|_isNew/)
  })

  it('toWireDimensions does not strip operator-controlled keys inside displayName.entries', () => {
    const rows = seedDimensions([
      dim({ displayName: { entries: { default: 'Sev', _key: 'a locale literally named _key' } } }),
    ])
    const wire = toWireDimensions(rows)
    expect(wire[0].displayName?.entries._key).toBe('a locale literally named _key')
  })

  it('markPersisted flips SUBMITTED rows true→false and leaves later-added rows new', () => {
    const submittedTax = newTaxonomy() // _isNew: true
    const lateTax = newTaxonomy() // added after the save click
    const submittedDim: EditableDimension = {
      ...newDimension(), // _isNew: true
      taxonomy: [submittedTax, lateTax],
    }
    const lateDim = newDimension() // added after the save click
    const rows = [submittedDim, lateDim]

    const sentDimKeys = new Set([submittedDim._key])
    const sentTaxKeys = new Set([submittedTax._key])
    const next = markPersisted(rows, sentDimKeys, sentTaxKeys)

    expect(next[0]._isNew).toBe(false) // submitted dim: true → false (the load-bearing flip)
    expect(next[0].taxonomy[0]._isNew).toBe(false) // submitted tax: true → false
    expect(next[0].taxonomy[1]._isNew).toBe(true) // tax added after click → still new
    expect(next[1]._isNew).toBe(true) // dim added after click → still new
  })

  it('markPersisted with empty sets flips nothing', () => {
    const fresh = newDimension()
    const next = markPersisted([fresh], new Set(), new Set())
    expect(next[0]._isNew).toBe(true)
  })

  describe('mergePromotedValue', () => {
    it('appends a promoted value (with the server displayName) as persisted', () => {
      const rows = seedDimensions([dim({ name: 'modules', taxonomy: [] })])
      const next = mergePromotedValue(rows, 'modules', 'billing', {
        entries: { 'zh-CN': 'billing' },
      })
      expect(next[0].taxonomy.map((t) => t.value)).toEqual(['billing'])
      expect(next[0].taxonomy[0]._isNew).toBe(false) // already persisted server-side
      expect(next[0].taxonomy[0]._key).toBeTruthy()
      // Mirrors the POSTed/stored displayName key, not a 'default' rewrite.
      expect(next[0].taxonomy[0].displayName?.entries['zh-CN']).toBe('billing')
    })

    it('falls back to a default displayName when none is provided', () => {
      const rows = seedDimensions([dim({ name: 'modules', taxonomy: [] })])
      const next = mergePromotedValue(rows, 'modules', 'billing')
      expect(next[0].taxonomy[0].displayName?.entries.default).toBe('billing')
    })

    it('is a no-op when the value already exists on the dimension', () => {
      const rows = seedDimensions([
        dim({ name: 'modules', taxonomy: [{ value: 'billing', examples: [] }] }),
      ])
      const next = mergePromotedValue(rows, 'modules', 'billing')
      expect(next[0].taxonomy).toHaveLength(1)
    })

    it('is a no-op for a dimension not in the draft', () => {
      const rows = seedDimensions([dim({ name: 'modules', taxonomy: [] })])
      const next = mergePromotedValue(rows, 'severity', 'P0')
      expect(next[0].taxonomy).toHaveLength(0)
    })

    it('affects only the FIRST dimension when two share a name', () => {
      const rows = seedDimensions([
        dim({ name: 'modules', taxonomy: [] }),
        dim({ name: 'modules', taxonomy: [] }),
      ])
      const next = mergePromotedValue(rows, 'modules', 'billing')
      expect(next[0].taxonomy.map((t) => t.value)).toEqual(['billing'])
      expect(next[1].taxonomy).toHaveLength(0)
    })

    it('trims the promoted value to match server storage', () => {
      const rows = seedDimensions([dim({ name: 'modules', taxonomy: [] })])
      const next = mergePromotedValue(rows, 'modules', '  billing  ')
      expect(next[0].taxonomy[0].value).toBe('billing')
    })
  })
})
