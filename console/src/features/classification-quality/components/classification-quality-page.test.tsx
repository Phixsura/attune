import { HttpResponse, http } from 'msw'
import { defaultClassificationQuality } from '@/testing/mocks/handlers'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'
import { ClassificationQualityPage } from './classification-quality-page'

test('renders classification quality drift, warnings, trend, and samples', async () => {
  server.use(
    http.get('/fb/v1/console/classification-quality', () =>
      HttpResponse.json({
        ...defaultClassificationQuality,
        summary: {
          classificationEvents: '1200',
          failedAttempts: '12',
          averageConfidence: 0.82,
          lowConfidenceRate: 0.14,
          offListRate: 0.06,
          unknownDimensionRate: 0.01,
          parseFailureRate: 0.02,
          terminalFailureRate: 0.01,
          worstSeverity: 'alert',
        },
        series: [
          {
            bucket: '2026-06-30T00:00:00Z',
            classificationEvents: '600',
            lowConfidenceRate: 0.08,
            offListRate: 0.03,
            parseFailureRate: 0.01,
            terminalFailureRate: 0,
          },
          {
            bucket: '2026-07-01T00:00:00Z',
            classificationEvents: '600',
            lowConfidenceRate: 0.14,
            offListRate: 0.06,
            parseFailureRate: 0.02,
            terminalFailureRate: 0.01,
          },
        ],
        warnings: [
          {
            reason: 'low_confidence_rate_spike',
            severity: 'alert',
            dimensionName: 'priority',
            value: 0.14,
            sampleFeedbackIds: ['fb-1'],
          },
          {
            reason: 'unknown_reason',
            severity: 'watch',
            dimensionName: '',
            value: 0.06,
            sampleFeedbackIds: [],
          },
        ],
        dimensions: [
          {
            dimensionName: 'priority',
            severity: 'alert',
            currentCount: '1200',
            baselineCount: '900',
            jsDistance: 0.17,
            psi: 0.31,
            lowConfidenceRate: 0.14,
            offListRate: 0.06,
            values: [
              {
                valueHash: 'urgent',
                valueDisplay: 'Urgent',
                valueStatus: 'configured',
                shareDeltaPp: 12.4,
              },
              {
                valueHash: 'custom',
                valueDisplay: '',
                valueStatus: 'off_list',
                shareDeltaPp: -6.2,
              },
              {
                valueHash: 'unknown-dim',
                valueDisplay: 'Unknown',
                valueStatus: 'unknown_dimension',
                shareDeltaPp: 4.1,
              },
            ],
          },
        ],
        samples: [
          {
            id: 'fb-1',
            title: 'Billing import misclassified',
            createdAt: '2026-07-01T08:30:00Z',
            source: 'api',
            classificationConfidence: 0.42,
            enrichmentStatus: 'done',
            signalReason: 'off_list_rate_spike',
          },
          {
            id: 'fb-2',
            title: '',
            createdAt: '',
            source: '',
            classificationConfidence: undefined,
            enrichmentStatus: '',
            signalReason: 'dimension_distribution_drift',
          },
        ],
      }),
    ),
  )

  renderWithProviders(<ClassificationQualityPage />)

  expect(await screen.findByText('质量率趋势')).toBeInTheDocument()
  expect(screen.getByText('低置信度升高')).toBeInTheDocument()
  expect(screen.getByText('unknown_reason')).toBeInTheDocument()
  expect(screen.getByText('priority')).toBeInTheDocument()
  expect(screen.getByText('Urgent')).toBeInTheDocument()
  expect(screen.getByText('越界')).toBeInTheDocument()
  expect(screen.getByText('Billing import misclassified')).toBeInTheDocument()
  expect(screen.getByText('fb-2')).toBeInTheDocument()
  expect(screen.getAllByText('查看反馈').length).toBeGreaterThanOrEqual(3)
})

test('sends advanced filters and clears them without changing the base range', async () => {
  const seen: URL[] = []
  server.use(
    http.get('/fb/v1/console/classification-quality', ({ request }) => {
      seen.push(new URL(request.url))
      return HttpResponse.json(defaultClassificationQuality)
    }),
  )
  const { user } = renderWithProviders(<ClassificationQualityPage />)

  await screen.findByText('暂无趋势数据')
  await user.type(screen.getByLabelText('维度'), 'priority')
  await user.type(screen.getByLabelText('来源'), 'api')
  await user.type(screen.getByLabelText('逻辑模型'), 'enrich-default')
  await user.type(screen.getByLabelText('提供商模型'), 'gpt-4.1-mini')
  await user.type(screen.getByLabelText('通道 ID'), 'primary')

  await waitFor(() => {
    const latest = seen.at(-1)
    expect(latest?.searchParams.get('dimension_name')).toBe('priority')
    expect(latest?.searchParams.get('source')).toBe('api')
    expect(latest?.searchParams.get('logical_model')).toBe('enrich-default')
    expect(latest?.searchParams.get('provider_model')).toBe('gpt-4.1-mini')
    expect(latest?.searchParams.get('channel_id')).toBe('primary')
  })

  await user.click(screen.getByRole('button', { name: '清除高级筛选' }))

  await waitFor(() => {
    expect(screen.getByLabelText('维度')).toHaveValue('')
    expect(screen.getByLabelText('来源')).toHaveValue('')
  })
  expect(screen.queryByRole('button', { name: '清除高级筛选' })).not.toBeInTheDocument()
})
