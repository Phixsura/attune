import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '@/testing/test-utils'
import { ScoringPanel } from './scoring-panel'

describe('ScoringPanel', () => {
  it('shows RICE score by default', () => {
    renderWithProviders(<ScoringPanel onApply={vi.fn()} />)
    const score = screen.getByTestId('score-value')
    // default: (100 * 3 * 80) / 5 = 4800
    expect(score.textContent).toBe('4800.0')
  })

  it('switches to ICE model', async () => {
    renderWithProviders(<ScoringPanel onApply={vi.fn()} />)
    await userEvent.click(screen.getByRole('button', { name: 'ICE 评分' }))
    const score = screen.getByTestId('score-value')
    // ICE: 3 * 80 * (10/5) = 480
    expect(score.textContent).toBe('480.0')
  })

  it('calls onApply with model and score', async () => {
    const onApply = vi.fn()
    renderWithProviders(<ScoringPanel onApply={onApply} />)
    await userEvent.click(screen.getByRole('button', { name: '应用' }))
    expect(onApply).toHaveBeenCalledWith('rice', 4800, expect.any(Object))
  })
})
