import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { DimensionsEditor } from '@/components/dim/dimensions-editor'
import { type EditableDimension, seedDimensions } from '@/lib/editable-rows'
import type { Dimension, Taxonomy } from '@/proto/attune/v1/common'
import { fireEvent, renderWithProviders, screen } from '@/testing/test-utils'

// Builders ----------------------------------------------------------------

function makeTax(over: Partial<Taxonomy> = {}): Taxonomy {
  return {
    value: 'P0',
    displayName: { entries: { default: 'P0' } },
    examples: [],
    ...over,
  } as Taxonomy
}

function makeDim(over: Partial<Dimension> = {}): Dimension {
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

// Controlled wrapper — DimensionsEditor pushes edits via onChange; mirror the
// route-level state pattern (seed-once into the editable model) so identity
// tests can observe behavior across edit cycles.
function Harness({
  initial,
  onChange,
}: {
  initial: Dimension[]
  onChange?: (v: EditableDimension[]) => void
}) {
  const [value, setValue] = useState<EditableDimension[]>(() => seedDimensions(initial))
  return (
    <DimensionsEditor
      value={value}
      onChange={(next) => {
        setValue(next)
        onChange?.(next)
      }}
    />
  )
}

// Like Harness, but the editor gets a bump-able `key` while the array stays in
// the parent's state — a real fiber unmount→remount with the data preserved.
// This is the #90 repro seam: on the old ref-based source the "new" flag lived
// in the editor instance and was lost here; with identity in the data it
// survives. (Must NOT be wrapped in <StrictMode> — that preserves refs on the
// same fiber and would be a false negative.)
function RemountHarness({ initial }: { initial: Dimension[] }) {
  const [value, setValue] = useState<EditableDimension[]>(() => seedDimensions(initial))
  const [k, setK] = useState(0)
  return (
    <div>
      <button type="button" data-testid="force-remount" onClick={() => setK((n) => n + 1)}>
        remount
      </button>
      <DimensionsEditor key={k} value={value} onChange={setValue} />
    </div>
  )
}

// `input[...]` is deliberate: the help <p> also carries a `dim-name-help-…`
// id, which a bare `[id^="dim-name-"]` would match.
const nameInput = () => document.querySelector<HTMLInputElement>('input[id^="dim-name-"]')
const allNameInputs = () =>
  Array.from(document.querySelectorAll<HTMLInputElement>('input[id^="dim-name-"]'))

describe('DimensionsEditor — identity tracking + sparse edits', () => {
  it('renders one card per dim in value, with the resolved display label', () => {
    const dims = [
      makeDim({ name: 'severity', displayName: { entries: { default: 'Severity' } } }),
      makeDim({ name: 'modules', displayName: { entries: { default: 'Modules' } }, kind: 'multi' }),
    ]
    renderWithProviders(<Harness initial={dims} />)
    expect(screen.getByText('Severity')).toBeInTheDocument()
    expect(screen.getByText('Modules')).toBeInTheDocument()
  })

  it('empty displayName falls back to the unnamed-dim i18n string', () => {
    renderWithProviders(<Harness initial={[makeDim({ name: '', displayName: { entries: {} } })]} />)
    const title = screen.getByTestId('dim-card-title')
    expect(title.textContent?.length ?? 0).toBeGreaterThan(0)
  })

  it('"Add dimension" appends a blank Dimension to onChange', async () => {
    const onChange = vi.fn()
    const { user } = renderWithProviders(<Harness initial={[]} onChange={onChange} />)
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    expect(onChange).toHaveBeenCalledTimes(1)
    const next = onChange.mock.calls[0][0] as EditableDimension[]
    expect(next).toHaveLength(1)
    expect(next[0]).toMatchObject({
      name: '',
      kind: 'single',
      taxonomy: [],
      urgentSet: [],
      required: false,
      _isNew: true,
    })
    expect(next[0]._key).toBeTruthy()
  })

  it('the trash button removes the matching card', async () => {
    const onChange = vi.fn()
    const dims = [makeDim({ name: 'a' }), makeDim({ name: 'b' })]
    const { user } = renderWithProviders(<Harness initial={dims} onChange={onChange} />)
    const trashButtons = screen.getAllByTestId('dim-editor-delete-dim')
    expect(trashButtons).toHaveLength(2)
    await user.click(trashButtons[0])
    const next = onChange.mock.calls[0][0] as EditableDimension[]
    expect(next.map((d) => d.name)).toEqual(['b'])
  })

  it('persisted dim: Name is readOnly and Kind renders as a read-only field (no combobox)', async () => {
    const dim = makeDim({ name: 'severity', kind: 'single' })
    const { user } = renderWithProviders(<Harness initial={[dim]} />)
    // Persisted cards start collapsed — expand via the header testid.
    await user.click(screen.getByTestId('dim-card-header'))
    expect(nameInput()?.readOnly).toBe(true)
    // Locked Kind is a read-only field, not a disabled combobox.
    expect(screen.queryByRole('combobox')).toBeNull()
    expect(screen.getByTestId('dim-kind-readonly')).toBeInTheDocument()
  })

  it('NEW dim (added via button): Name is editable and Kind is an enabled combobox', async () => {
    const { user } = renderWithProviders(<Harness initial={[]} />)
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    // A freshly-added card auto-opens; no header click needed.
    expect(nameInput()?.readOnly).toBe(false)
    const kindTrigger = screen.getByRole('combobox')
    expect(kindTrigger).not.toBeDisabled()
  })

  it("editing a NEW dim's name keeps it editable (identity is in the data, not the value)", async () => {
    const { user } = renderWithProviders(<Harness initial={[]} />)
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    const input = nameInput()
    expect(input).not.toBeNull()
    await user.type(input as HTMLInputElement, 'sev')
    // The DOM id is derived from the stable _key, so it does NOT change as the
    // operator types — the same input stays editable.
    expect(nameInput()?.value).toBe('sev')
    expect(nameInput()?.readOnly).toBe(false)
  })

  // --- #90 core: identity must survive a genuine remount ------------------

  it('a NEW dim stays editable across a real editor remount (array preserved)', async () => {
    const { user } = renderWithProviders(<RemountHarness initial={[]} />)
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    expect(nameInput()?.readOnly).toBe(false)
    await user.click(screen.getByTestId('force-remount'))
    // On the old ref-based source the "new" flag died here → readOnly=true.
    expect(nameInput()?.readOnly).toBe(false)
  })

  it('compound: add → type a partial name → remount → still editable and retains input', async () => {
    const { user } = renderWithProviders(<RemountHarness initial={[]} />)
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    await user.type(nameInput() as HTMLInputElement, 'sev')
    await user.click(screen.getByTestId('force-remount'))
    expect(nameInput()?.value).toBe('sev')
    expect(nameInput()?.readOnly).toBe(false)
  })

  // Guard (NOT a fail-on-main repro): a persisted row's locked state was the
  // default on the old ref-based source too, so this was green there. It guards
  // against a regression that flips persisted rows editable after remount.
  it('a persisted dim stays locked across a real editor remount', async () => {
    const { user } = renderWithProviders(
      <RemountHarness initial={[makeDim({ name: 'severity' })]} />,
    )
    await user.click(screen.getByTestId('force-remount'))
    await user.click(screen.getByTestId('dim-card-header'))
    expect(nameInput()?.readOnly).toBe(true)
  })

  // --- a11y: stable, unique DOM ids --------------------------------------

  it('the disclosure button exposes expanded state via aria-expanded', async () => {
    const { user } = renderWithProviders(<Harness initial={[makeDim({ name: 'severity' })]} />)
    const toggle = screen.getByTestId('dim-card-header')
    expect(toggle).toHaveAttribute('aria-expanded', 'false') // persisted starts collapsed
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
  })

  it('two NEW dims render unique, non-colliding name input ids', async () => {
    const { user } = renderWithProviders(<Harness initial={[]} />)
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    const ids = allNameInputs().map((el) => el.id)
    expect(ids).toHaveLength(2)
    expect(new Set(ids).size).toBe(2)
  })

  // --- G2: classify by identity, never by value --------------------------

  it('typing a NEW name equal to an existing persisted name does NOT lock it', async () => {
    // Persisted 'severity' is seeded (and collapsed). Add a new card and type
    // 'severity' into it — value-based classification (A2) would flip it to
    // "persisted" and lock; identity-based does not.
    const { user } = renderWithProviders(<Harness initial={[makeDim({ name: 'severity' })]} />)
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    // Only the new (auto-opened) card exposes a name input; the persisted one
    // is collapsed.
    await user.type(nameInput() as HTMLInputElement, 'severity')
    expect(nameInput()?.value).toBe('severity')
    expect(nameInput()?.readOnly).toBe(false)
  })

  it('removing a persisted sibling leaves the new row editable', async () => {
    const { user } = renderWithProviders(<Harness initial={[makeDim({ name: 'severity' })]} />)
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    // Two cards now: persisted 'severity' (collapsed) + new (open). Remove the
    // persisted one via its trash button (the first card's).
    const trash = screen.getAllByTestId('dim-editor-delete-dim')
    await user.click(trash[0])
    expect(nameInput()?.readOnly).toBe(false)
  })

  it('adding a dimension focuses its Name input (WCAG 2.4.3)', async () => {
    const { user } = renderWithProviders(<Harness initial={[]} />)
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    expect(document.activeElement?.id.startsWith('dim-name-')).toBe(true)
  })

  // --- taxonomy layer (same identity model via the shared helpers) -------

  it('adding a taxonomy pushes a blank entry; removing it syncs urgent_set', async () => {
    const dim = makeDim({
      name: 'severity',
      taxonomy: [makeTax({ value: 'P0' }), makeTax({ value: 'P1' })],
      urgentSet: ['P0'],
    })
    const onChange = vi.fn()
    const { user } = renderWithProviders(<Harness initial={[dim]} onChange={onChange} />)
    await user.click(screen.getByTestId('dim-card-header'))
    const removeButtons = screen.getAllByTestId('dim-editor-remove-value')
    expect(removeButtons).toHaveLength(2)
    await user.click(removeButtons[0])
    const final = onChange.mock.calls[onChange.mock.calls.length - 1][0] as EditableDimension[]
    // The value is removed from BOTH taxonomy and urgentSet in one atomic patch.
    expect(final[0].taxonomy.map((t) => t.value)).toEqual(['P1'])
    expect(final[0].urgentSet).toEqual([])
  })

  it('clicking a chip stores the wire VALUE in urgent_set, not the display label', async () => {
    // Distinct value vs label so the test discriminates: urgentSet must hold the
    // machine value ('p0'), never the chip's display text ('Priority Zero').
    const dim = makeDim({
      name: 'severity',
      taxonomy: [
        makeTax({ value: 'p0', displayName: { entries: { default: 'Priority Zero' } } }),
        makeTax({ value: 'p1', displayName: { entries: { default: 'Priority One' } } }),
      ],
      urgentSet: [],
    })
    const onChange = vi.fn()
    const { user } = renderWithProviders(<Harness initial={[dim]} onChange={onChange} />)
    await user.click(screen.getByTestId('dim-card-header'))
    const chip = screen.getAllByRole('button').find((b) => b.textContent === 'Priority Zero')
    expect(chip).toBeTruthy()
    await user.click(chip as HTMLElement)
    const after = onChange.mock.calls[onChange.mock.calls.length - 1][0] as EditableDimension[]
    expect(after[0].urgentSet).toContain('p0')
    expect(after[0].urgentSet).not.toContain('Priority Zero')
  })

  it('removing a NON-urgent taxonomy preserves other urgent memberships', async () => {
    const dim = makeDim({
      name: 'severity',
      taxonomy: [makeTax({ value: 'P0' }), makeTax({ value: 'P1' })],
      urgentSet: ['P0'],
    })
    const onChange = vi.fn()
    const { user } = renderWithProviders(<Harness initial={[dim]} onChange={onChange} />)
    await user.click(screen.getByTestId('dim-card-header'))
    const removeButtons = screen.getAllByTestId('dim-editor-remove-value')
    // Remove P1 (non-urgent); P0's urgent membership must survive, and P1 must
    // actually leave the taxonomy.
    await user.click(removeButtons[1])
    const final = onChange.mock.calls[onChange.mock.calls.length - 1][0] as EditableDimension[]
    expect(final[0].taxonomy.map((t) => t.value)).toEqual(['P0'])
    expect(final[0].urgentSet).toEqual(['P0'])
  })

  it('renders an urgent chip only for named taxonomies (mixed named + empty)', async () => {
    const dim = makeDim({
      name: 'severity',
      taxonomy: [makeTax({ value: 'P0' }), makeTax({ value: '' })],
      urgentSet: [],
    })
    const { user } = renderWithProviders(<Harness initial={[dim]} />)
    await user.click(screen.getByTestId('dim-card-header'))
    // Two taxonomy rows but only the named one is eligible → exactly one chip.
    expect(document.querySelectorAll('[aria-pressed]')).toHaveLength(1)
  })

  it('an unnamed (empty-value) taxonomy renders no urgent chip; naming it adds one', async () => {
    const { user } = renderWithProviders(<Harness initial={[makeDim({ name: 'severity' })]} />)
    await user.click(screen.getByTestId('dim-card-header'))
    await user.click(screen.getByTestId('dim-editor-add-value'))
    // Empty value → not eligible for urgent → no chip (chips carry aria-pressed).
    expect(document.querySelector('[aria-pressed]')).toBeNull()
    const valueInput = document.querySelector<HTMLInputElement>('input[id^="tax-value-"]')
    await user.type(valueInput as HTMLInputElement, 'crit')
    expect(document.querySelector('[aria-pressed]')).not.toBeNull()
  })

  it('renaming a NEW taxonomy value remaps its urgent membership (no dangling value)', async () => {
    const onChange = vi.fn()
    const { user } = renderWithProviders(
      <Harness initial={[makeDim({ name: 'severity' })]} onChange={onChange} />,
    )
    await user.click(screen.getByTestId('dim-card-header'))
    await user.click(screen.getByTestId('dim-editor-add-value'))
    const valueInput = document.querySelector<HTMLInputElement>('input[id^="tax-value-"]')
    await user.type(valueInput as HTMLInputElement, 'crit')
    const chip = screen.getAllByRole('button').find((b) => b.textContent === 'crit')
    await user.click(chip as HTMLElement)
    // Atomically rename the value; urgentSet must follow, not dangle on 'crit'.
    fireEvent.change(valueInput as HTMLInputElement, { target: { value: 'urgent' } })
    const last = onChange.mock.calls[onChange.mock.calls.length - 1][0] as EditableDimension[]
    expect(last[0].urgentSet).toEqual(['urgent'])
  })

  it('renaming a NEW urgent value onto another urgent value de-dups urgent_set', async () => {
    const onChange = vi.fn()
    const { user } = renderWithProviders(
      <Harness initial={[makeDim({ name: 'severity' })]} onChange={onChange} />,
    )
    await user.click(screen.getByTestId('dim-card-header'))
    await user.click(screen.getByTestId('dim-editor-add-value'))
    await user.click(screen.getByTestId('dim-editor-add-value'))
    const inputs = () =>
      Array.from(document.querySelectorAll<HTMLInputElement>('input[id^="tax-value-"]'))
    await user.type(inputs()[0], 'p0')
    await user.type(inputs()[1], 'p1')
    const chip = (txt: string) =>
      screen.getAllByRole('button').find((b) => b.textContent === txt) as HTMLElement
    await user.click(chip('p0'))
    await user.click(chip('p1'))
    // Both urgent; rename p0 → p1 (a value already in urgentSet).
    fireEvent.change(inputs()[0], { target: { value: 'p1' } })
    const last = onChange.mock.calls[onChange.mock.calls.length - 1][0] as EditableDimension[]
    expect(last[0].urgentSet).toHaveLength(1)
    expect(last[0].urgentSet).toContain('p1')
  })

  it('renaming an urgent value to empty drops it from urgent_set (no stray empty)', async () => {
    const onChange = vi.fn()
    const { user } = renderWithProviders(
      <Harness initial={[makeDim({ name: 'severity' })]} onChange={onChange} />,
    )
    await user.click(screen.getByTestId('dim-card-header'))
    await user.click(screen.getByTestId('dim-editor-add-value'))
    const valueInput = document.querySelector<HTMLInputElement>('input[id^="tax-value-"]')
    await user.type(valueInput as HTMLInputElement, 'crit')
    const chip = screen.getAllByRole('button').find((b) => b.textContent === 'crit')
    await user.click(chip as HTMLElement)
    fireEvent.change(valueInput as HTMLInputElement, { target: { value: '' } })
    const last = onChange.mock.calls[onChange.mock.calls.length - 1][0] as EditableDimension[]
    expect(last[0].urgentSet).toEqual([])
  })

  it('edits new dimension metadata and toggles an urgent taxonomy back off', async () => {
    const onChange = vi.fn()
    const lastChange = () =>
      onChange.mock.calls[onChange.mock.calls.length - 1][0] as EditableDimension[]
    const { user } = renderWithProviders(<Harness initial={[]} onChange={onChange} />)

    await user.click(screen.getByTestId('dim-editor-add-dim'))
    await user.type(nameInput() as HTMLInputElement, 'severity')
    await user.click(screen.getByRole('combobox'))
    await user.click(screen.getByRole('option', { name: 'multi（多选）' }))
    expect(lastChange()[0].kind).toBe('multi')

    fireEvent.change(screen.getAllByPlaceholderText('severity')[0], {
      target: { value: 'Severity label' },
    })
    expect(lastChange()[0].displayName?.entries.default).toBe('Severity label')

    await user.click(screen.getByTestId('dim-editor-add-value'))
    const valueInput = document.querySelector<HTMLInputElement>('input[id^="tax-value-"]')
    await user.type(valueInput as HTMLInputElement, 'p0')
    fireEvent.change(screen.getAllByPlaceholderText('p0')[0], {
      target: { value: 'Priority Zero' },
    })
    expect(lastChange()[0].taxonomy[0].displayName?.entries.default).toBe('Priority Zero')

    const chip = screen.getByRole('button', { name: 'Priority Zero' })
    await user.click(chip)
    expect(lastChange()[0].urgentSet).toEqual(['p0'])
    await user.click(chip)
    expect(lastChange()[0].urgentSet).toEqual([])
  })

  it('removing a taxonomy row moves focus to the Add value button (WCAG 2.4.3)', async () => {
    const dim = makeDim({ name: 'severity', taxonomy: [makeTax({ value: 'P0' })] })
    const { user } = renderWithProviders(<Harness initial={[dim]} />)
    await user.click(screen.getByTestId('dim-card-header'))
    await user.click(screen.getByTestId('dim-editor-remove-value'))
    expect(document.activeElement).toBe(screen.getByTestId('dim-editor-add-value'))
  })

  it('two NEW taxonomy rows render unique, non-colliding value input ids', async () => {
    const { user } = renderWithProviders(<Harness initial={[makeDim({ name: 'severity' })]} />)
    await user.click(screen.getByTestId('dim-card-header'))
    await user.click(screen.getByTestId('dim-editor-add-value'))
    await user.click(screen.getByTestId('dim-editor-add-value'))
    const taxIds = Array.from(
      document.querySelectorAll<HTMLInputElement>('input[id^="tax-value-"]'),
    ).map((el) => el.id)
    expect(taxIds).toHaveLength(2)
    expect(new Set(taxIds).size).toBe(taxIds.length)
    // New taxonomy value is editable.
    const newTaxInput = document.querySelector<HTMLInputElement>('input[id^="tax-value-"]')
    expect(newTaxInput?.readOnly).toBe(false)
  })

  it('taxonomy help text differs by kind — asserted via data-kind attribute, not text', async () => {
    const { user, unmount } = renderWithProviders(
      <Harness initial={[makeDim({ kind: 'single' })]} />,
    )
    await user.click(screen.getByTestId('dim-card-header'))
    const singleHelp = screen.getByTestId('dim-card-taxonomy-help')
    expect(singleHelp).toHaveAttribute('data-kind', 'single')
    const singleText = singleHelp.textContent ?? ''
    expect(singleText.length).toBeGreaterThan(0)
    unmount()

    const { user: u2 } = renderWithProviders(<Harness initial={[makeDim({ kind: 'multi' })]} />)
    await u2.click(screen.getByTestId('dim-card-header'))
    const multiHelp = screen.getByTestId('dim-card-taxonomy-help')
    expect(multiHelp).toHaveAttribute('data-kind', 'multi')
    const multiText = multiHelp.textContent ?? ''
    expect(multiText.length).toBeGreaterThan(0)
    expect(multiText).not.toBe(singleText)
  })
})
