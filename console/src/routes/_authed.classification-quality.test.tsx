import { QueryClient } from '@tanstack/react-query'
import { isRedirect } from '@tanstack/react-router'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { ClassificationQualityPage } from '@/features/classification-quality/components/classification-quality-page'
import { Route as AnalyticsClassificationQualityRoute } from '@/routes/_authed.analytics.classification-quality'
import { Route as LegacyClassificationQualityRoute } from '@/routes/_authed.classification-quality'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

const qualityResponseFixture = {
  generatedAt: '2026-07-02T00:00:00Z',
  dataThrough: '2026-07-02T00:00:00Z',
  rollupLagSeconds: '0',
  currentFrom: '2026-06-25T00:00:00Z',
  currentTo: '2026-07-02T00:00:00Z',
  baselineFrom: '2026-05-28T00:00:00Z',
  baselineTo: '2026-06-25T00:00:00Z',
  bucketWidth: 'day',
  summary: {
    classificationEvents: '100',
    failedAttempts: '6',
    averageConfidence: 0.73,
    lowConfidenceRate: 0.12,
    offListRate: 0.06,
    unknownDimensionRate: 0,
    parseFailureRate: 0.03,
    terminalFailureRate: 0.02,
    worstSeverity: 'alert',
  },
  series: [
    {
      bucket: '2026-07-01T00:00:00Z',
      classificationEvents: '100',
      failedAttempts: '6',
      averageConfidence: 0.73,
      lowConfidenceRate: 0.12,
      offListRate: 0.06,
      unknownDimensionRate: 0,
      parseFailureRate: 0.03,
      terminalFailureRate: 0.02,
    },
  ],
  dimensions: [
    {
      dimensionName: 'severity',
      severity: 'alert',
      status: 'normal',
      currentCount: '100',
      baselineCount: '100',
      jsDistance: 0.24,
      psi: 0.3,
      lowConfidenceRate: 0.12,
      offListRate: 0.06,
      unknownDimensionRate: 0,
      sampleFeedbackIds: ['101'],
      values: [
        {
          valueHash: 'bug-hash',
          valueDisplay: 'bug',
          valueStatus: 'configured',
          currentCount: '70',
          baselineCount: '40',
          currentShare: 0.7,
          baselineShare: 0.4,
          shareDeltaPp: 30,
          contribution: 0.3,
          sampleFeedbackIds: ['101'],
        },
      ],
    },
  ],
  warnings: [
    {
      reason: 'low_confidence_rate_spike',
      severity: 'alert',
      dimensionName: '',
      valueDisplay: '',
      value: 0.12,
      threshold: 0.1,
      message: 'Quality signal crossed threshold',
      sampleFeedbackIds: ['101'],
    },
  ],
  samples: [
    {
      id: '101',
      createdAt: '2026-07-01T00:00:00Z',
      source: 'api',
      title: 'checkout broken',
      enrichmentStatus: 'done',
      classificationConfidence: 0.42,
      signalReason: 'low_confidence_rate_spike',
    },
  ],
}

interface ThrownRedirect {
  options: { to: string; statusCode?: number }
}

describe('_authed.analytics.classification-quality route', () => {
  it('preloads the default dashboard query from the canonical route loader', async () => {
    let seen: URL | undefined
    server.use(
      http.get('/fb/v1/console/classification-quality', ({ request }) => {
        seen = new URL(request.url)
        return HttpResponse.json(qualityResponseFixture)
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = AnalyticsClassificationQualityRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toMatchObject({
      summary: { worstSeverity: 'alert' },
    })
    expect(AnalyticsClassificationQualityRoute.options.component).toBeTypeOf('function')
    expect(seen?.searchParams.get('bucket_width')).toBe('day')
    expect(seen?.searchParams.get('severity')).toBeNull()
  })

  it('redirects the legacy shortcut route to analytics classification quality', () => {
    const beforeLoad = LegacyClassificationQualityRoute.options.beforeLoad as () => void
    let thrown: unknown

    try {
      beforeLoad()
    } catch (err) {
      thrown = err
    }

    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/analytics/classification-quality')
  })

  it('renders quality warnings, drift rows, and samples from the dashboard query', async () => {
    let seen: URL | undefined
    server.use(
      http.get('/fb/v1/console/classification-quality', ({ request }) => {
        seen = new URL(request.url)
        return HttpResponse.json(qualityResponseFixture)
      }),
    )

    renderWithProviders(<ClassificationQualityPage />)

    await waitFor(() => {
      expect(screen.getByText('checkout broken')).toBeInTheDocument()
    })
    expect(seen?.searchParams.get('bucket_width')).toBe('day')
    expect(screen.getByText('分类质量')).toBeInTheDocument()
    expect(screen.getByText('低置信度升高')).toBeInTheDocument()
    expect(screen.getByText('severity')).toBeInTheDocument()
    expect(screen.getByText('bug')).toBeInTheDocument()
    expect(screen.getAllByText('查看反馈').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByRole('link', { name: '查看反馈' })[0]).toHaveAttribute(
      'href',
      '/feedback?ids=101&quality_signal=low_confidence',
    )

    const sourceFilter = screen.getByLabelText('来源')
    await userEvent.type(sourceFilter, 'api')
    await waitFor(() => {
      expect(seen?.searchParams.get('source')).toBe('api')
    })
    await userEvent.click(screen.getByLabelText('清除高级筛选'))
    expect(sourceFilter).toHaveValue('')
    expect(screen.queryByLabelText('清除高级筛选')).not.toBeInTheDocument()
  }, 20_000)
})
