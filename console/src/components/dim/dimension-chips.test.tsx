import { describe, expect, it } from 'vitest'
import { DimensionChips } from '@/components/dim/dimension-chips'
import type { Dimension } from '@/proto/attune/v1/common'
import { renderWithProviders, screen } from '@/testing/test-utils'

function toneDim(): Dimension {
  return {
    name: 'sentiment',
    displayName: { entries: { default: 'Customer tone' } },
    kind: 'single',
    taxonomy: [
      { value: 'positive', displayName: { entries: { default: 'Positive' } }, examples: [] },
      { value: 'frustrated', displayName: { entries: { default: 'Frustrated' } }, examples: [] },
    ],
    urgentSet: [],
    required: false,
    examples: [],
    extractionHint: '',
    renderer: {
      kind: 'enum_badge',
      values: {
        positive: { icon: 'smile', tone: 'success' },
        frustrated: { icon: 'flame', tone: 'danger' },
      },
    },
  }
}

describe('DimensionChips', () => {
  it('renders generic enum_badge renderer metadata without knowing the dim name', () => {
    renderWithProviders(<DimensionChips dim={toneDim()} value="frustrated" />)

    const badge = screen.getByText('Frustrated').closest('span')
    expect(badge).toHaveClass('text-destructive')
  })

  it('falls back to the ordinary badge when renderer value is absent', () => {
    renderWithProviders(<DimensionChips dim={toneDim()} value="unknown" />)

    expect(screen.getByText('unknown')).toBeInTheDocument()
  })
})
