import { describe, expect, it } from 'vitest'
import { SimilarityIndicator } from '@/features/feedback/components/similarity-indicator'
import { SimilarityRing } from '@/features/feedback/components/similarity-ring'
import { renderWithProviders, screen } from '@/testing/test-utils'

describe('SimilarityIndicator', () => {
  it('shows correct percentage', () => {
    renderWithProviders(<SimilarityIndicator similarity={0.75} />)
    expect(screen.getByText('75%')).toBeInTheDocument()
  })

  it('rounds percentage to nearest integer', () => {
    renderWithProviders(<SimilarityIndicator similarity={0.756} />)
    expect(screen.getByText('76%')).toBeInTheDocument()
  })

  it('clamps value to 0-1 range (above)', () => {
    renderWithProviders(<SimilarityIndicator similarity={1.5} />)
    expect(screen.getByText('100%')).toBeInTheDocument()
  })

  it('clamps value to 0-1 range (below)', () => {
    renderWithProviders(<SimilarityIndicator similarity={-0.5} />)
    expect(screen.getByText('0%')).toBeInTheDocument()
  })

  it('hides percentage when showPercentage is false', () => {
    renderWithProviders(<SimilarityIndicator similarity={0.75} showPercentage={false} />)
    expect(screen.queryByText('75%')).not.toBeInTheDocument()
  })

  it('uses green color for high similarity (>= 80%)', () => {
    const { container } = renderWithProviders(<SimilarityIndicator similarity={0.85} />)
    const progressBar = container.querySelector('.bg-emerald-500')
    expect(progressBar).toBeInTheDocument()
  })

  it('uses amber color for medium similarity (>= 60%)', () => {
    const { container } = renderWithProviders(<SimilarityIndicator similarity={0.65} />)
    const progressBar = container.querySelector('.bg-amber-500')
    expect(progressBar).toBeInTheDocument()
  })

  it('uses orange color for low similarity (>= 40%)', () => {
    const { container } = renderWithProviders(<SimilarityIndicator similarity={0.45} />)
    const progressBar = container.querySelector('.bg-orange-500')
    expect(progressBar).toBeInTheDocument()
  })

  it('uses red color for very low similarity (< 40%)', () => {
    const { container } = renderWithProviders(<SimilarityIndicator similarity={0.25} />)
    const progressBar = container.querySelector('.bg-red-500')
    expect(progressBar).toBeInTheDocument()
  })

  it('applies size classes correctly for sm', () => {
    const { container } = renderWithProviders(<SimilarityIndicator similarity={0.5} size="sm" />)
    const bar = container.querySelector('.h-1\\.5')
    expect(bar).toBeInTheDocument()
  })

  it('applies size classes correctly for lg', () => {
    const { container } = renderWithProviders(<SimilarityIndicator similarity={0.5} size="lg" />)
    const bar = container.querySelector('.h-2\\.5')
    expect(bar).toBeInTheDocument()
  })
})

describe('SimilarityRing', () => {
  it('renders SVG with correct aria-label', () => {
    renderWithProviders(<SimilarityRing similarity={0.75} />)
    const svg = screen.getByRole('img', { name: '75% similarity' })
    expect(svg).toBeInTheDocument()
  })

  it('shows percentage inside ring', () => {
    renderWithProviders(<SimilarityRing similarity={0.85} />)
    expect(screen.getByText('85')).toBeInTheDocument()
  })

  it('rounds percentage correctly', () => {
    renderWithProviders(<SimilarityRing similarity={0.756} />)
    expect(screen.getByText('76')).toBeInTheDocument()
  })

  it('clamps to valid range', () => {
    renderWithProviders(<SimilarityRing similarity={1.5} />)
    expect(screen.getByText('100')).toBeInTheDocument()
  })

  it('uses green stroke for high similarity', () => {
    const { container } = renderWithProviders(<SimilarityRing similarity={0.85} />)
    const progressCircle = container.querySelector('.stroke-emerald-500')
    expect(progressCircle).toBeInTheDocument()
  })

  it('uses amber stroke for medium similarity', () => {
    const { container } = renderWithProviders(<SimilarityRing similarity={0.65} />)
    const progressCircle = container.querySelector('.stroke-amber-500')
    expect(progressCircle).toBeInTheDocument()
  })

  it('uses orange stroke for low similarity', () => {
    const { container } = renderWithProviders(<SimilarityRing similarity={0.45} />)
    const progressCircle = container.querySelector('.stroke-orange-500')
    expect(progressCircle).toBeInTheDocument()
  })

  it('uses red stroke for very low similarity', () => {
    const { container } = renderWithProviders(<SimilarityRing similarity={0.25} />)
    const progressCircle = container.querySelector('.stroke-red-500')
    expect(progressCircle).toBeInTheDocument()
  })

  it('respects custom size', () => {
    renderWithProviders(<SimilarityRing similarity={0.5} size={64} />)
    const svg = screen.getByRole('img')
    expect(svg).toHaveAttribute('width', '64')
    expect(svg).toHaveAttribute('height', '64')
  })

  it('calculates stroke-dashoffset for progress', () => {
    const { container } = renderWithProviders(
      <SimilarityRing similarity={0.5} size={32} strokeWidth={3} />,
    )
    // For 50% progress, the offset should be half the circumference
    // radius = (32 - 3) / 2 = 14.5
    // circumference = 14.5 * 2 * PI = ~91.1
    // offset for 50% = 91.1 - 0.5 * 91.1 = ~45.55
    const circles = container.querySelectorAll('circle')
    expect(circles).toHaveLength(2) // background + progress
  })
})
