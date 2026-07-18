import { describe, expect, it } from 'vitest'
import { renderWithProviders, screen } from '@/testing/test-utils'
import { UsageBarChart } from './bar-chart'
import { UsageSparkline } from './sparkline'

function buckets(count: number, valueFor: (index: number) => number) {
  return Array.from({ length: count }, (_, index) => ({
    bucket: new Date(Date.UTC(2026, 0, index + 1)).toISOString(),
    value: valueFor(index),
  }))
}

describe('usage charts', () => {
  it('renders nothing for empty bar and sparkline series', () => {
    const bar = renderWithProviders(<UsageBarChart series={[]} />)
    expect(bar.container.firstChild).toBeNull()

    const sparkline = renderWithProviders(<UsageSparkline series={[]} />)
    expect(sparkline.container.firstChild).toBeNull()
  })

  it('renders compact bar labels for short daily series', () => {
    const { container } = renderWithProviders(
      <UsageBarChart series={buckets(3, (index) => index + 1)} />,
    )

    expect(screen.getByRole('img', { name: 'Daily ingest counts' })).toBeInTheDocument()
    expect(container.querySelectorAll('rect')).toHaveLength(3)
    expect(screen.getByText('1/1')).toBeInTheDocument()
    expect(screen.getByText('1/2')).toBeInTheDocument()
    expect(screen.getByText('1/3')).toBeInTheDocument()
  })

  it('uses dense spacing and endpoint ticks for long daily series', () => {
    const { container } = renderWithProviders(
      <UsageBarChart series={buckets(50, (index) => (index % 5) + 1)} />,
    )

    const bars = container.querySelectorAll('rect')
    expect(bars).toHaveLength(50)
    expect(bars[0]).toHaveAttribute('rx', '2')
    expect(screen.getByText('1/1')).toBeInTheDocument()
    expect(screen.getByText('2/19')).toBeInTheDocument()
    expect(screen.queryByText('1/2')).not.toBeInTheDocument()
  })

  it('keeps zero-valued sparkline buckets measurable against the fallback max', () => {
    const { container } = renderWithProviders(<UsageSparkline series={buckets(2, () => 0)} />)

    expect(screen.getByRole('img', { name: 'Daily ingest sparkline' })).toBeInTheDocument()
    const bars = container.querySelectorAll('rect')
    expect(bars).toHaveLength(2)
    expect(bars[0]).toHaveAttribute('height', '0')
    expect(bars[1]).toHaveAttribute('height', '0')
  })
})
