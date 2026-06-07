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
    // The add button text is the resolved `dim.i18n.add_locale` ("添加语言").
    await user.click(screen.getByRole('button', { name: /添加语言/ }))
    expect(onChange).toHaveBeenLastCalledWith({ default: 'D', ja: '' })
  })
})
