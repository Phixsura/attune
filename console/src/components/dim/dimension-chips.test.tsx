import { describe, expect, it } from 'vitest'
import {
  DimensionChips,
  DimensionValue,
  displayForTaxonomy,
  rendererToneBarClass,
  UrgentDot,
} from '@/components/dim/dimension-chips'
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
    const { container } = renderWithProviders(<DimensionChips dim={toneDim()} value="frustrated" />)

    const badge = screen.getByText('Frustrated').closest('span')
    expect(badge).toHaveClass('text-destructive')
    expect(container.querySelector('svg')).toBeInTheDocument()
  })

  it('renders success enum badges with the smile icon', () => {
    const { container } = renderWithProviders(<DimensionChips dim={toneDim()} value="positive" />)

    expect(screen.getByText('Positive').closest('span')).toHaveClass('border-emerald-500/30')
    expect(container.querySelector('svg')).toBeInTheDocument()
  })

  it('falls back to the ordinary badge when renderer value is absent', () => {
    renderWithProviders(<DimensionChips dim={toneDim()} value="unknown" />)

    expect(screen.getByText('unknown')).toBeInTheDocument()
  })

  it.each([
    ['warning', 'frown', 'border-amber-500/30', true],
    ['muted', 'minus', 'text-muted-foreground', true],
    ['custom', 'unknown', 'border-border', false],
  ])('renders %s enum badge variants', (tone, icon, expectedClass, hasIcon) => {
    const dim: Dimension = {
      ...toneDim(),
      taxonomy: [{ value: tone, displayName: { entries: { default: tone } }, examples: [] }],
      renderer: {
        kind: 'enum_badge',
        values: {
          [tone]: { icon, tone },
        },
      },
    }
    const { container } = renderWithProviders(<DimensionChips dim={dim} value={tone} />)

    expect(screen.getByText(tone).closest('span')).toHaveClass(expectedClass)
    expect(container.querySelector('svg') != null).toBe(hasIcon)
  })

  it('renders multi values as taxonomy chips and ignores non-string entries', () => {
    const dim: Dimension = {
      ...toneDim(),
      kind: 'multi',
      taxonomy: [
        { value: 'positive', displayName: { entries: { default: 'Positive' } }, examples: [] },
        { value: 'frustrated', displayName: { entries: { default: 'Frustrated' } }, examples: [] },
      ],
    }

    renderWithProviders(
      <DimensionChips dim={dim} value={['positive', 0, '', 'legacy']} className="custom-gap" />,
    )

    expect(screen.getByText('Positive')).toBeInTheDocument()
    expect(screen.getByText('legacy')).toBeInTheDocument()
    expect(screen.queryByText('0')).not.toBeInTheDocument()
    expect(screen.getByText('Positive').parentElement).toHaveClass('custom-gap')
  })

  it('renders a dash for empty values and can suppress the placeholder', () => {
    const { rerender } = renderWithProviders(<DimensionChips dim={toneDim()} value="" />)

    expect(screen.getByText('—')).toBeInTheDocument()

    rerender(<DimensionChips dim={toneDim()} value="" emptyDash={false} />)
    expect(screen.queryByText('—')).not.toBeInTheDocument()
  })

  it('renders a single string through the multi-value path', () => {
    const dim: Dimension = {
      ...toneDim(),
      kind: 'multi',
    }

    renderWithProviders(<DimensionChips dim={dim} value="positive" />)

    expect(screen.getByText('Positive')).toBeInTheDocument()
  })

  it('renders a dash for empty multi values', () => {
    const dim: Dimension = {
      ...toneDim(),
      kind: 'multi',
    }

    renderWithProviders(<DimensionChips dim={dim} value={[]} />)

    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it('can suppress the empty placeholder for non-string multi values', () => {
    const dim: Dimension = {
      ...toneDim(),
      kind: 'multi',
    }

    renderWithProviders(<DimensionChips dim={dim} value={123} emptyDash={false} />)

    expect(screen.queryByText('—')).not.toBeInTheDocument()
  })

  it('handles non-string single values without rendering a badge', () => {
    renderWithProviders(<DimensionValue dim={toneDim()} value={123} emptyDash={false} />)

    expect(screen.queryByText('—')).not.toBeInTheDocument()
    expect(screen.queryByText('123')).not.toBeInTheDocument()
  })

  it('maps renderer tones to bar classes', () => {
    expect(rendererToneBarClass('success')).toBe('bg-emerald-500/70')
    expect(rendererToneBarClass('warning')).toBe('bg-amber-500/70')
    expect(rendererToneBarClass('danger')).toBe('bg-destructive/75')
    expect(rendererToneBarClass('muted')).toBe('bg-muted-foreground/40')
    expect(rendererToneBarClass(undefined)).toBe('bg-foreground/55')
    expect(rendererToneBarClass('custom')).toBe('bg-foreground/55')
  })

  it('falls back to stable values when display names are absent', () => {
    const taxonomy = [{ value: 'raw', displayName: { entries: {} }, examples: [] }]
    const displayOf = () => ''

    expect(displayForTaxonomy(taxonomy, 'raw', displayOf)).toBe('raw')
    expect(displayForTaxonomy(taxonomy, 'missing', displayOf)).toBe('missing')
  })

  it('renders urgent dot only for urgent rows', () => {
    const { rerender } = renderWithProviders(<UrgentDot urgent={true} />)

    expect(screen.getByLabelText('urgent')).toHaveClass('bg-destructive')

    rerender(<UrgentDot urgent={false} />)
    expect(screen.queryByLabelText('urgent')).not.toBeInTheDocument()
  })
})
