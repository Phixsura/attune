import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import type { AnomalyEvent, SeriesPoint } from '@/features/anomalies/api/anomalies'
import { AnomalyCard } from '@/features/anomalies/components/anomaly-card'
import { AnomalySeriesChart } from '@/features/anomalies/components/anomaly-series-chart'
import { ContributionBars } from '@/features/anomalies/components/contribution-bars'

// Adversarial props: extremes the wire can legally deliver.
const base: AnomalyEvent = {
  eventId: 'e1',
  sliceType: 'total',
  sliceKey: 'total',
  sliceDisplay: 'All feedback',
  direction: 'spike',
  firstBucketDate: '2026-08-01',
  lastBucketDate: '2026-08-11',
  observed: '9007199254740993', // > MAX_SAFE_INTEGER as int64-string
  expectedMed: 1e-9,
  expectedLow: 0,
  expectedHigh: 1e308,
  zScore: 1e308,
  status: 'open',
  createdAt: '',
  resolvedAt: '',
}

describe('adversarial rendering', () => {
  it('card survives >MAX_SAFE_INTEGER observed and huge z', () => {
    render(<AnomalyCard event={base} />)
    expect(screen.getByTestId('anomaly-card')).toBeInTheDocument()
  })

  it('card renders 10000-char display without layout crash', () => {
    render(<AnomalyCard event={{ ...base, sliceDisplay: 'x'.repeat(10000) }} />)
    expect(screen.getByTestId('anomaly-card')).toBeInTheDocument()
  })

  it('chart survives single point, zero counts, and Infinity-adjacent bands', () => {
    const pts: SeriesPoint[] = [
      {
        date: '2026-08-11',
        count: '0',
        expectedMed: 0,
        expectedLow: 0,
        expectedHigh: 1e308,
        isAnomalous: true,
        insufficient: false,
      },
    ]
    const { container } = render(<AnomalySeriesChart points={pts} />)
    const svg = container.querySelector('svg')
    expect(svg).not.toBeNull()
    // No NaN leaked into any path/circle attribute.
    expect(container.innerHTML).not.toContain('NaN')
  })

  it('chart with all-insufficient points renders line but no band', () => {
    const pts: SeriesPoint[] = Array.from({ length: 5 }, (_, i) => ({
      date: `2026-08-0${i + 1}`,
      count: String(i),
      expectedMed: 0,
      expectedLow: 0,
      expectedHigh: 0,
      isAnomalous: false,
      insufficient: true,
    }))
    const { container } = render(<AnomalySeriesChart points={pts} />)
    expect(container.querySelector('[data-testid="expected-band"]')).toBeNull()
    expect(container.querySelector('[data-testid="count-line"]')).not.toBeNull()
    expect(container.innerHTML).not.toContain('NaN')
  })

  it('contribution bars clamp bar width for >100% and negative shares', () => {
    // share >1 is mathematically reachable: one value overshoots the total
    // deviation when siblings moved the other way. The bar clamps; the
    // percent label reports the true value.
    const { container } = render(
      <ContributionBars
        evidence={{
          contributions: [
            { dim: 'source', value: 'zendesk', share: 5.5 },
            { dim: 'source', value: 'api', share: -0.4 },
          ],
          spread: false,
          feedbackIds: [],
        }}
      />,
    )
    expect(screen.getByTestId('contribution-bars')).toBeInTheDocument()
    const bars = container.querySelectorAll('[style]')
    let checked = 0
    for (const el of bars) {
      const w = (el as HTMLElement).style.width
      if (w.endsWith('%')) {
        const pct = Number.parseFloat(w)
        expect(pct).toBeLessThanOrEqual(100)
        expect(pct).toBeGreaterThanOrEqual(0)
        checked++
      }
    }
    expect(checked).toBe(2)
    // Labels report truth (550%, -40%) — informative, not clamped.
    expect(screen.getByText('550%')).toBeInTheDocument()
  })
})
