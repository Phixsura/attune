import { describe, expect, it, vi } from 'vitest'
import { I18nInput } from '@/components/dim/i18n-input'
import { renderWithProviders, screen } from '@/testing/test-utils'

describe('I18nInput', () => {
  it('renders the default + zh + en quick-pick rows plus any existing locales', () => {
    renderWithProviders(<I18nInput value={{ default: 'D', fr: 'French' }} onChange={vi.fn()} />)
    // The label column is small + monospace; assert via text presence.
    expect(screen.getByText('default')).toBeInTheDocument()
    expect(screen.getByText('zh')).toBeInTheDocument()
    expect(screen.getByText('en')).toBeInTheDocument()
    expect(screen.getByText('fr')).toBeInTheDocument()
  })

  it('typing in a row calls onChange with the per-keystroke locale map', async () => {
    const onChange = vi.fn()
    const { user } = renderWithProviders(<I18nInput value={{ default: '' }} onChange={onChange} />)
    const inputs = screen.getAllByRole('textbox') as HTMLInputElement[]
    // The component is controlled by `value` — without a re-render
    // loop, typing 'H' fires onChange({default:'H'}); typing 'i' next
    // again uses the unchanged value{default:''}, firing
    // onChange({default:'i'}). Assert the FIRST call captures the
    // controlled-edit contract (a parent that re-passes value would
    // get cumulative typing).
    await user.type(inputs[0], 'H')
    expect(onChange).toHaveBeenCalledWith({ default: 'H' })
  })

  it('"Add" button appends a new empty entry for the typed locale tag', async () => {
    const onChange = vi.fn()
    const { user } = renderWithProviders(<I18nInput value={{ default: 'D' }} onChange={onChange} />)
    const tagFields = screen.getAllByRole('textbox') as HTMLInputElement[]
    const addTagInput = tagFields[tagFields.length - 1]
    await user.type(addTagInput, 'ja')
    await user.click(screen.getByTestId('i18n-input-add-locale'))
    expect(onChange).toHaveBeenLastCalledWith({ default: 'D', ja: '' })
  })

  it('emptying an existing row removes that locale from the map', async () => {
    const onChange = vi.fn()
    const { user } = renderWithProviders(
      <I18nInput value={{ default: 'Default', fr: 'French' }} onChange={onChange} />,
    )

    const frInput = screen.getByDisplayValue('French')
    await user.clear(frInput)

    expect(onChange).toHaveBeenLastCalledWith({ default: 'Default' })
  })

  it('remove buttons delete non-default locales only', async () => {
    const onChange = vi.fn()
    const { user } = renderWithProviders(
      <I18nInput value={{ default: 'Default', fr: 'French' }} onChange={onChange} />,
    )

    expect(screen.queryByLabelText(/default/)).not.toBeInTheDocument()
    await user.click(screen.getByLabelText(/fr/))

    expect(onChange).toHaveBeenCalledWith({ default: 'Default' })
  })

  it('enter adds a trimmed locale and duplicate tags only clear the input', async () => {
    const onChange = vi.fn()
    const { user } = renderWithProviders(<I18nInput value={{ default: 'D' }} onChange={onChange} />)
    const addTagInput = screen.getAllByRole('textbox').at(-1) as HTMLInputElement

    await user.type(addTagInput, '  ko  {Enter}')
    expect(onChange).toHaveBeenLastCalledWith({ default: 'D', ko: '' })

    onChange.mockClear()
    await user.type(addTagInput, 'zh')
    await user.click(screen.getByTestId('i18n-input-add-locale'))
    expect(onChange).not.toHaveBeenCalled()
    expect(addTagInput).toHaveValue('')
  })

  it('disabled mode hides locale controls and disables text fields', () => {
    renderWithProviders(
      <I18nInput value={{ default: 'Default', fr: 'French' }} onChange={vi.fn()} disabled />,
    )

    expect(screen.queryByTestId('i18n-input-add-locale')).not.toBeInTheDocument()
    expect(screen.queryByLabelText(/fr/)).not.toBeInTheDocument()
    for (const input of screen.getAllByRole('textbox')) {
      expect(input).toBeDisabled()
    }
  })
})
