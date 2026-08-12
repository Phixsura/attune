import { screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { AnomalyEvent, SeriesPoint } from '@/features/anomalies/api/anomalies'
import { AnomalyCard } from '@/features/anomalies/components/anomaly-card'
import { AnomalySeriesChart } from '@/features/anomalies/components/anomaly-series-chart'
import { ContributionBars } from '@/features/anomalies/components/contribution-bars'
import { renderWithProviders } from '@/testing/test-utils'

vi.mock('@tanstack/react-router', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@tanstack/react-router')>()
  return {
    ...actual,
    createFileRoute: () => (opts: unknown) => opts,
    Link: ({ children }: { children: React.ReactNode }) => <a href="/feedback">{children}</a>,
  }
})

function sampleEvent(overrides: Partial<AnomalyEvent> = {}): AnomalyEvent {
  return {
    createdAt: '2026-08-10T04:00:00Z',
    direction: 'spike',
    eventId: 'e1',
    expectedHigh: 21.4,
    expectedLow: 6.2,
    expectedMed: 12,
    firstBucketDate: '2026-08-09',
    lastBucketDate: '2026-08-09',
    observed: '40',
    resolvedAt: '',
    sliceDisplay: 'All feedback',
    sliceKey: 'total',
    sliceType: 'total',
    status: 'open',
    zScore: 8.1,
    ...overrides,
  }
}

function seriesPoint(overrides: Partial<SeriesPoint> = {}): SeriesPoint {
  return {
    count: '12',
    date: '2026-08-01',
    expectedHigh: 21,
    expectedLow: 6,
    expectedMed: 12,
    insufficient: false,
    isAnomalous: false,
    ...overrides,
  }
}

const renderWithQuery = renderWithProviders

describe('AnomalyCard', () => {
  it('renders the magnitude sentence and slice display', () => {
    renderWithQuery(<AnomalyCard event={sampleEvent()} />)
    expect(screen.getByText('All feedback')).toBeInTheDocument()
    expect(screen.getByText(/观测 40，预期 12/)).toBeInTheDocument()
  })

  it('shows the ongoing badge for multi-day events', () => {
    renderWithQuery(
      <AnomalyCard
        event={sampleEvent({ firstBucketDate: '2026-08-07', lastBucketDate: '2026-08-09' })}
      />,
    )
    expect(screen.getByText(/持续 3 天/)).toBeInTheDocument()
  })

  it('shows the retracted badge', () => {
    renderWithQuery(<AnomalyCard event={sampleEvent({ status: 'retracted' })} />)
    expect(screen.getByText(/数据修正后已撤销/)).toBeInTheDocument()
  })

  it('invokes onSelect when clicked', async () => {
    const onSelect = vi.fn()
    renderWithQuery(<AnomalyCard event={sampleEvent()} onSelect={onSelect} />)
    screen.getByTestId('anomaly-card').click()
    await waitFor(() => expect(onSelect).toHaveBeenCalledOnce())
  })
})

describe('AnomalySeriesChart', () => {
  it('renders band, line, and red dots on anomalous days', () => {
    const points = [
      seriesPoint({ date: '2026-08-05' }),
      seriesPoint({ date: '2026-08-06' }),
      seriesPoint({ count: '40', date: '2026-08-07', isAnomalous: true }),
    ]
    const { container } = renderWithQuery(<AnomalySeriesChart points={points} />)
    expect(container.querySelector('[data-testid="expected-band"]')).not.toBeNull()
    expect(container.querySelector('[data-testid="count-line"]')).not.toBeNull()
    expect(container.querySelectorAll('[data-testid="anomaly-dot"]')).toHaveLength(1)
  })

  it('renders nothing for an empty series', () => {
    const { container } = renderWithQuery(<AnomalySeriesChart points={[]} />)
    expect(container.querySelector('svg')).toBeNull()
  })

  it('skips the band when all points are insufficient', () => {
    const points = [
      seriesPoint({ insufficient: true }),
      seriesPoint({ date: '2026-08-02', insufficient: true }),
    ]
    const { container } = renderWithQuery(<AnomalySeriesChart points={points} />)
    expect(container.querySelector('[data-testid="expected-band"]')).toBeNull()
  })
})

describe('ContributionBars', () => {
  it('renders top contributions with percentages', () => {
    renderWithQuery(
      <ContributionBars
        evidence={{
          contributions: [
            { dim: 'source', share: 0.65, value: 'zendesk' },
            { dim: 'source', share: 0.25, value: 'api' },
          ],
          feedbackIds: [],
          spread: false,
        }}
      />,
    )
    expect(screen.getByTestId('contribution-bars')).toBeInTheDocument()
    expect(screen.getByText('source=zendesk')).toBeInTheDocument()
    expect(screen.getByText('65%')).toBeInTheDocument()
  })

  it('renders the spread empty state', () => {
    renderWithQuery(
      <ContributionBars evidence={{ contributions: [], feedbackIds: [], spread: true }} />,
    )
    expect(screen.getByTestId('contribution-spread')).toBeInTheDocument()
  })
})
