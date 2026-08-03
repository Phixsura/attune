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
    http.get('/fb/v1/console/classification-quality/review-learning', () =>
      HttpResponse.json({
        totalReviews: '7',
        accepted: '3',
        edited: '2',
        dismissed: '2',
        trainingCandidateCount: '4',
        reviewedFeedbackCount: '7',
        classifiedFeedbackCount: '100',
        reviewCoverageRate: 0.07,
        reasonBuckets: [
          {
            signalReason: 'low_confidence_rate_spike',
            totalReviews: '4',
            accepted: '1',
            edited: '2',
            dismissed: '1',
            trainingCandidateCount: '3',
            lastReviewedAt: '2026-07-02T09:00:00Z',
          },
        ],
        recentEvents: [],
      }),
    ),
  )

  renderWithProviders(<ClassificationQualityPage />)

  expect(await screen.findByText('质量率趋势')).toBeInTheDocument()
  expect(screen.getAllByText('低置信度升高').length).toBeGreaterThanOrEqual(1)
  expect(screen.getByText('unknown_reason')).toBeInTheDocument()
  expect(screen.getByText('priority')).toBeInTheDocument()
  expect(screen.getByText('Urgent')).toBeInTheDocument()
  expect(screen.getByText('越界')).toBeInTheDocument()
  expect(screen.getByText('Billing import misclassified')).toBeInTheDocument()
  expect(screen.getByText('fb-2')).toBeInTheDocument()
  expect(screen.getByText('AI 审核学习')).toBeInTheDocument()
  expect(screen.getByText('4 个训练候选')).toBeInTheDocument()
  expect(screen.getAllByText('查看反馈').length).toBeGreaterThanOrEqual(2)
  expect(screen.getByRole('link', { name: '查看反馈 fb-1' })).toBeInTheDocument()
})

test('records AI review feedback from classification samples', async () => {
  const posted: unknown[] = []
  server.use(
    http.get('/fb/v1/console/classification-quality', () =>
      HttpResponse.json({
        ...defaultClassificationQuality,
        samples: [
          {
            id: '101',
            title: 'Low confidence sample',
            createdAt: '2026-07-01T08:30:00Z',
            source: 'api',
            classificationConfidence: 0.42,
            enrichmentStatus: 'done',
            signalReason: 'low_confidence_rate_spike',
          },
        ],
      }),
    ),
    http.get('/fb/v1/console/classification-quality/review-learning', () =>
      HttpResponse.json({
        totalReviews: '0',
        accepted: '0',
        edited: '0',
        dismissed: '0',
        trainingCandidateCount: '0',
        reviewedFeedbackCount: '0',
        classifiedFeedbackCount: '1',
        reviewCoverageRate: 0,
        reasonBuckets: [],
        recentEvents: [],
      }),
    ),
    http.post('/fb/v1/console/classification-quality/reviews', async ({ request }) => {
      posted.push(await request.json())
      return HttpResponse.json({
        event: { eventId: String(posted.length), feedbackId: '101', outcome: 'accepted' },
        learning: {
          totalReviews: String(posted.length),
          accepted: '1',
          edited: posted.length > 1 ? '1' : '0',
          dismissed: '0',
          trainingCandidateCount: posted.length > 1 ? '1' : '0',
          reasonBuckets: [],
          recentEvents: [],
        },
      })
    }),
  )

  const { user } = renderWithProviders(<ClassificationQualityPage />)

  expect(await screen.findByText('Low confidence sample')).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: '接受反馈 101 的分类' }))
  await waitFor(() => expect(posted).toHaveLength(1))
  await user.click(screen.getByRole('button', { name: '修正反馈 101 的分类' }))
  await user.click(screen.getByRole('button', { name: '保存修正' }))
  await waitFor(() => expect(posted).toHaveLength(2))

  expect(posted[0]).toMatchObject({
    feedbackId: '101',
    outcome: 'accepted',
    signalReason: 'low_confidence_rate_spike',
  })
  expect(posted[1]).toMatchObject({
    feedbackId: '101',
    outcome: 'edited',
    correctionJson: '{\n  \n}',
  })
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

test('updates select filters and maps quality reasons to feedback links', async () => {
  const seen: URL[] = []
  server.use(
    http.get('/fb/v1/console/classification-quality', ({ request }) => {
      seen.push(new URL(request.url))
      return HttpResponse.json({
        ...defaultClassificationQuality,
        summary: {
          classificationEvents: '42',
          failedAttempts: '1',
          averageConfidence: 0.91,
          lowConfidenceRate: 0.06,
          offListRate: 0.01,
          unknownDimensionRate: 0,
          parseFailureRate: 0.01,
          terminalFailureRate: 0,
          worstSeverity: 'insufficient_data',
        },
        warnings: [
          {
            reason: 'dimension_distribution_drift',
            severity: 'insufficient_data',
            dimensionName: 'priority',
            value: 0.04,
            sampleFeedbackIds: ['fb-drift'],
          },
          {
            reason: 'off_list_rate_spike',
            severity: 'watch',
            dimensionName: 'topic',
            value: 0.03,
            sampleFeedbackIds: ['fb-off-list'],
          },
          {
            reason: 'parse_failure_rate_spike',
            severity: 'alert',
            dimensionName: '',
            value: 0.02,
            sampleFeedbackIds: ['fb-parse'],
          },
          {
            reason: 'terminal_failure_rate_spike',
            severity: 'alert',
            dimensionName: '',
            value: 0.01,
            sampleFeedbackIds: ['fb-terminal'],
          },
        ],
        dimensions: [
          {
            dimensionName: 'topic',
            severity: 'watch',
            currentCount: '42',
            baselineCount: '40',
            jsDistance: 0.03,
            psi: 0.04,
            lowConfidenceRate: 0.06,
            offListRate: 0.01,
            values: [
              {
                valueHash: 'new-topic',
                valueDisplay: 'New topic',
                valueStatus: 'custom_status',
                shareDeltaPp: 2.5,
              },
            ],
          },
        ],
        samples: [
          {
            id: 'fb-parse',
            title: 'Parser could not read JSON',
            createdAt: '2026-07-01T08:30:00Z',
            source: 'api',
            classificationConfidence: 0.73,
            enrichmentStatus: 'failed',
            signalReason: 'parse_failure_rate_spike',
          },
          {
            id: 'fb-terminal',
            title: 'Classification terminal failure',
            createdAt: '2026-07-01T09:30:00Z',
            source: 'api',
            classificationConfidence: 0.81,
            enrichmentStatus: 'failed',
            signalReason: 'terminal_failure_rate_spike',
          },
        ],
      })
    }),
  )

  const { user } = renderWithProviders(<ClassificationQualityPage />)

  expect(await screen.findByText('维度分布漂移')).toBeInTheDocument()
  expect(screen.getByText('越界值升高')).toBeInTheDocument()
  expect(screen.getByText('解析失败升高')).toBeInTheDocument()
  expect(screen.getByText('终态失败升高')).toBeInTheDocument()
  expect(screen.getAllByText('数据不足').length).toBeGreaterThan(0)
  expect(screen.getByText('custom_status')).toBeInTheDocument()

  await user.click(screen.getAllByRole('combobox')[1])
  await user.click(await screen.findByRole('option', { name: '按小时' }))
  await waitFor(() => {
    expect(seen.some((url) => url.searchParams.get('bucket_width') === 'hour')).toBe(true)
  })

  await user.click(screen.getAllByRole('combobox')[2])
  await user.click(await screen.findByRole('option', { name: '关注' }))
  await waitFor(() => {
    expect(seen.at(-1)?.searchParams.get('severity')).toBe('watch')
  })

  await user.click(screen.getAllByRole('combobox')[0])
  await user.click(await screen.findByRole('option', { name: '30 天' }))
  await waitFor(() => {
    expect(seen.at(-1)?.searchParams.get('bucket_width')).toBe('day')
  })

  const feedbackLinks = screen.getAllByRole('link', { name: '查看反馈' })
  expect(
    feedbackLinks.some((link) =>
      link.getAttribute('href')?.includes('quality_signal=parse_failure'),
    ),
  ).toBe(true)
  expect(
    feedbackLinks.some((link) =>
      link.getAttribute('href')?.includes('quality_signal=terminal_failure'),
    ),
  ).toBe(true)
})
