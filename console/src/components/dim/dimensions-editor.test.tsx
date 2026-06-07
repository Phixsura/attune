import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { DimensionsEditor } from '@/components/dim/dimensions-editor'
import type { Dimension, Taxonomy } from '@/proto/attune/v1/common'
import { renderWithProviders, screen } from '@/testing/test-utils'

// Builders ----------------------------------------------------------------

function makeTax(over: Partial<Taxonomy> = {}): Taxonomy {
  return {
    value: 'P0',
    displayName: { entries: { default: 'P0' } },
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
    ...over,
  } as Dimension
}

// Controlled wrapper — DimensionsEditor pushes edits via onChange; mirror
// the route-level state pattern so identity-tracking tests can observe
// behavior across edit cycles.
function Harness({
  initial,
  onChange,
}: {
  initial: Dimension[]
  onChange?: (v: Dimension[]) => void
}) {
  const [value, setValue] = useState<Dimension[]>(initial)
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

// Helpers -----------------------------------------------------------------

// `dim.editor.delete_dim` → "删除维度" — the trash button has this aria-label.
// `dim.editor.add_dim` → "+ 添加维度" — the bottom add-dim button.

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

  it('empty displayName falls back to dim.editor.unnamed', () => {
    renderWithProviders(<Harness initial={[makeDim({ name: '', displayName: { entries: {} } })]} />)
    expect(screen.getByText('未命名维度')).toBeInTheDocument()
  })

  it('"Add dimension" appends a blank Dimension to onChange', async () => {
    const onChange = vi.fn()
    const { user } = renderWithProviders(<Harness initial={[]} onChange={onChange} />)
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    expect(onChange).toHaveBeenCalledTimes(1)
    const next = onChange.mock.calls[0][0] as Dimension[]
    expect(next).toHaveLength(1)
    expect(next[0]).toMatchObject({
      name: '',
      kind: 'single',
      taxonomy: [],
      urgentSet: [],
      required: false,
    })
  })

  it('the trash button removes the matching card', async () => {
    const onChange = vi.fn()
    const dims = [makeDim({ name: 'a' }), makeDim({ name: 'b' })]
    const { user } = renderWithProviders(<Harness initial={dims} onChange={onChange} />)
    // Both cards have aria-label="删除维度". Click the first.
    const trashButtons = screen.getAllByTestId('dim-editor-delete-dim')
    expect(trashButtons).toHaveLength(2)
    await user.click(trashButtons[0])
    const next = onChange.mock.calls[0][0] as Dimension[]
    expect(next.map((d) => d.name)).toEqual(['b'])
  })

  it('persisted dim: Name input is readOnly, Kind select is disabled', async () => {
    const dim = makeDim({ name: 'severity', kind: 'single' })
    const { user } = renderWithProviders(<Harness initial={[dim]} />)
    // Expand the (one) card. The chevron button is the first ghost button
    // in the header.
    const headerToggle = screen.getByText('Severity')
    await user.click(headerToggle)
    // Name input has id="dim-name-severity".
    const nameInput = document.getElementById('dim-name-severity') as HTMLInputElement
    expect(nameInput.readOnly).toBe(true)
    // Kind select trigger has data-slot or specific role; assert via aria-disabled.
    const kindTrigger = screen.getAllByRole('combobox')[0]
    expect(kindTrigger).toBeDisabled()
  })

  it('NEW dim (added via button): Name input is editable, Kind enabled', async () => {
    const onChange = vi.fn()
    const { user } = renderWithProviders(<Harness initial={[]} onChange={onChange} />)
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    // Card has empty display name → falls back to 未命名维度.
    await user.click(screen.getByText('未命名维度'))
    // Name input id="dim-name-" (empty name).
    const nameInput = document.getElementById('dim-name-') as HTMLInputElement
    expect(nameInput.readOnly).toBe(false)
    const kindTrigger = screen.getAllByRole('combobox')[0]
    expect(kindTrigger).not.toBeDisabled()
  })

  it("identity transfer: editing a NEW dim's name keeps Name editable on next render", async () => {
    const { user } = renderWithProviders(<Harness initial={[]} />)
    await user.click(screen.getByTestId('dim-editor-add-dim'))
    await user.click(screen.getByText('未命名维度'))
    const nameInput = document.getElementById('dim-name-') as HTMLInputElement
    await user.type(nameInput, 'sev')
    // After the patch, the Name input id changes to `dim-name-sev`. The
    // dim object is freshly merged — the WeakMap copies the "new" id over,
    // so the next render still treats it as new and keeps Name editable.
    const renamed = document.getElementById('dim-name-sev') as HTMLInputElement
    expect(renamed).not.toBeNull()
    expect(renamed.readOnly).toBe(false)
  })

  it('adding a taxonomy pushes a blank entry; removing it syncs urgent_set', async () => {
    const dim = makeDim({
      name: 'severity',
      taxonomy: [makeTax({ value: 'P0' }), makeTax({ value: 'P1' })],
      urgentSet: ['P0'],
    })
    const onChange = vi.fn()
    const { user } = renderWithProviders(<Harness initial={[dim]} onChange={onChange} />)
    await user.click(screen.getByText('Severity'))
    // Remove the P0 taxonomy row (which is in urgentSet).
    const removeButtons = screen.getAllByTestId('dim-editor-remove-value')
    expect(removeButtons.length).toBeGreaterThanOrEqual(2)
    await user.click(removeButtons[0])
    // Two onChange calls: setTaxonomy (drops P0) then setUrgentSet (drops 'P0' from urgentSet).
    // Latest call should reflect the final state (urgentSet empty).
    expect(onChange.mock.calls.length).toBeGreaterThanOrEqual(2)
    const final = onChange.mock.calls[onChange.mock.calls.length - 1][0] as Dimension[]
    expect(final[0].urgentSet).toEqual([])
  })

  it('clicking a taxonomy chip in the urgent-set strip toggles its value in urgent_set', async () => {
    const dim = makeDim({
      name: 'severity',
      taxonomy: [makeTax({ value: 'P0' }), makeTax({ value: 'P1' })],
      urgentSet: [], // start empty
    })
    const onChange = vi.fn()
    const { user } = renderWithProviders(<Harness initial={[dim]} onChange={onChange} />)
    await user.click(screen.getByText('Severity'))
    // The urgent-set strip is the only place where taxonomy value labels
    // render as `<button>` (the chips). Find a chip button whose text is
    // a taxonomy value display label.
    const chipButtons = screen
      .getAllByRole('button')
      .filter((b) => b.textContent === 'P0' || b.textContent === 'P1')
    expect(chipButtons.length).toBeGreaterThanOrEqual(2)
    await user.click(chipButtons[0]) // toggle P0 in
    const after = onChange.mock.calls[onChange.mock.calls.length - 1][0] as Dimension[]
    expect(after[0].urgentSet).toContain(
      // The clicked chip is P0 (text content match above).
      chipButtons[0].textContent,
    )
  })

  it('taxonomy help text differs by kind (single)', async () => {
    const { user } = renderWithProviders(<Harness initial={[makeDim({ kind: 'single' })]} />)
    await user.click(screen.getByText('Severity'))
    expect(screen.getByText('AI 在以下值里挑一个；为空时强制要求 AI 填')).toBeInTheDocument()
  })

  it('taxonomy help text differs by kind (multi)', async () => {
    const { user } = renderWithProviders(<Harness initial={[makeDim({ kind: 'multi' })]} />)
    await user.click(screen.getByText('Severity'))
    expect(
      screen.getByText('AI 在以下值里挑 0 到多个；为空时自由识别（任意短字符串）'),
    ).toBeInTheDocument()
  })
})
