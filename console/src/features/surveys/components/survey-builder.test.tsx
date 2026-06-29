import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '@/testing/test-utils'
import type { SurveyDefinition } from './survey-builder'
import { SurveyBuilder } from './survey-builder'

const SURVEYS: SurveyDefinition[] = [
  {
    id: '1',
    name: 'Post-support NPS',
    surveyType: 'nps',
    question: 'How likely are you to recommend us?',
    enabled: true,
  },
  {
    id: '2',
    name: 'Onboarding CSAT',
    surveyType: 'csat',
    question: 'How satisfied were you?',
    enabled: false,
  },
]

describe('SurveyBuilder', () => {
  it('renders survey rows with type badges', () => {
    renderWithProviders(
      <SurveyBuilder surveys={SURVEYS} onAdd={vi.fn()} onRemove={vi.fn()} onToggle={vi.fn()} />,
    )
    expect(screen.getByText('Post-support NPS')).toBeInTheDocument()
    expect(screen.getByText('NPS')).toBeInTheDocument()
    expect(screen.getByText('CSAT')).toBeInTheDocument()
  })

  it('shows empty state when no surveys', () => {
    renderWithProviders(
      <SurveyBuilder surveys={[]} onAdd={vi.fn()} onRemove={vi.fn()} onToggle={vi.fn()} />,
    )
    expect(screen.getByText('暂无调查问卷')).toBeInTheDocument()
  })

  it('calls onRemove when delete clicked', async () => {
    const onRemove = vi.fn()
    const { container } = renderWithProviders(
      <SurveyBuilder surveys={SURVEYS} onAdd={vi.fn()} onRemove={onRemove} onToggle={vi.fn()} />,
    )
    const iconButtons = container.querySelectorAll('button[data-size="icon"]')
    if (iconButtons.length === 0) {
      const allButtons = screen.getAllByRole('button')
      const svgButtons = allButtons.filter((b) => b.querySelector('svg') && !b.textContent?.trim())
      await userEvent.click(svgButtons[0])
    } else {
      await userEvent.click(iconButtons[0])
    }
    expect(onRemove).toHaveBeenCalledWith('1')
  })
})
