import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { createElement } from 'react'
import {
  classificationQualityQuery,
  classificationQualityQueryKey,
  classificationQualitySearchParams,
  classificationReviewLearningQuery,
  classificationReviewLearningQueryKey,
  classificationReviewLearningSearchParams,
  normalizeClassificationQualityFilters,
  normalizeReviewLearningFilters,
  qualityWindowForRange,
  useRecordClassificationReview,
} from '@/features/classification-quality/api/get-classification-quality'
import { server } from '@/testing/mocks/server'
import { renderHook, waitFor } from '@/testing/test-utils'

function makeQc() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
}

function wrapperFor(queryClient: QueryClient) {
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: queryClient }, children)
}

describe('classificationQualityQuery', () => {
  it('builds the classification quality query string', async () => {
    let seen: URL | undefined
    server.use(
      http.get('/fb/v1/console/classification-quality', ({ request }) => {
        seen = new URL(request.url)
        return HttpResponse.json({ series: [], dimensions: [], warnings: [], samples: [] })
      }),
    )

    await makeQc().fetchQuery(
      classificationQualityQuery({
        range: '7d',
        bucketWidth: 'hour',
        severity: 'alert',
        dimensionName: 'severity',
        source: 'api',
        logicalModel: 'enrich-default',
        providerModel: 'gpt-4.1-mini',
        channelId: 'primary',
      }),
    )

    expect(seen?.searchParams.get('bucket_width')).toBe('hour')
    expect(seen?.searchParams.get('severity')).toBe('alert')
    expect(seen?.searchParams.get('dimension_name')).toBe('severity')
    expect(seen?.searchParams.get('source')).toBe('api')
    expect(seen?.searchParams.get('logical_model')).toBe('enrich-default')
    expect(seen?.searchParams.get('provider_model')).toBe('gpt-4.1-mini')
    expect(seen?.searchParams.get('channel_id')).toBe('primary')
    expect(seen?.searchParams.get('current_from')).toBeTruthy()
    expect(seen?.searchParams.get('baseline_from')).toBeTruthy()
  })

  it('derives adjacent current and baseline windows', () => {
    const window = qualityWindowForRange('7d', new Date('2026-07-02T18:00:00Z'))

    expect(window.currentFrom).toBe('2026-06-25T00:00:00.000Z')
    expect(window.currentTo).toBe('2026-07-02T00:00:00.000Z')
    expect(window.baselineFrom).toBe('2026-05-28T00:00:00.000Z')
    expect(window.baselineTo).toBe('2026-06-25T00:00:00.000Z')
  })

  it('omits all-severity and empty optional filters', () => {
    const qs = classificationQualitySearchParams({
      range: '7d',
      bucketWidth: 'day',
      severity: 'all',
      providerModel: ' ',
    })

    expect(qs.has('severity')).toBe(false)
    expect(qs.has('provider_model')).toBe(false)
  })

  it('coerces hourly buckets to daily buckets for long windows', () => {
    const normalized = normalizeClassificationQualityFilters({
      range: '30d',
      bucketWidth: 'hour',
      severity: 'all',
    })
    const qs = classificationQualitySearchParams({
      range: '30d',
      bucketWidth: 'hour',
      severity: 'all',
    })

    expect(normalized.bucketWidth).toBe('day')
    expect(qs.get('bucket_width')).toBe('day')
  })

  it('builds review learning params and fetches the learning summary', async () => {
    let seen: URL | undefined
    server.use(
      http.get('/fb/v1/console/classification-quality/review-learning', ({ request }) => {
        seen = new URL(request.url)
        return HttpResponse.json({
          totalReviews: '3',
          accepted: '1',
          edited: '1',
          dismissed: '1',
          trainingCandidateCount: '2',
          reasonBuckets: [],
          recentEvents: [],
        })
      }),
    )

    const data = await makeQc().fetchQuery(
      classificationReviewLearningQuery({
        range: '30d',
        signalReason: 'low_confidence_rate_spike',
        limit: 12,
      }),
    )

    expect(seen?.searchParams.get('signal_reason')).toBe('low_confidence_rate_spike')
    expect(seen?.searchParams.get('limit')).toBe('12')
    expect(seen?.searchParams.get('current_from')).toBeTruthy()
    expect(data.trainingCandidateCount).toBe('2')
    expect(normalizeReviewLearningFilters({ limit: 99 })).toEqual({
      range: '7d',
      signalReason: undefined,
      limit: 50,
    })
    expect(classificationReviewLearningSearchParams({ limit: -1 }).get('limit')).toBe('10')
  })

  it('records review feedback and invalidates quality caches', async () => {
    let posted: unknown
    server.use(
      http.post('/fb/v1/console/classification-quality/reviews', async ({ request }) => {
        posted = await request.json()
        return HttpResponse.json({
          event: { eventId: '77', feedbackId: '101', outcome: 'edited' },
          learning: { totalReviews: '1', reasonBuckets: [], recentEvents: [] },
        })
      }),
    )

    const qc = makeQc()
    qc.setQueryData([...classificationQualityQueryKey, { range: '7d' }], { samples: [] })
    qc.setQueryData([...classificationReviewLearningQueryKey, { range: '7d' }], {
      totalReviews: '0',
    })
    const { result } = renderHook(() => useRecordClassificationReview(), {
      wrapper: wrapperFor(qc),
    })
    result.current.mutate({
      feedbackId: '101',
      outcome: 'edited',
      signalReason: 'low_confidence_rate_spike',
      correctionJson: '{"severity":"bug"}',
      note: 'corrected',
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(posted).toMatchObject({
      feedbackId: '101',
      outcome: 'edited',
      correctionJson: '{"severity":"bug"}',
    })
    expect(
      qc
        .getQueryCache()
        .findAll({ queryKey: classificationQualityQueryKey })
        .some((query) => query.state.isInvalidated),
    ).toBe(true)
    expect(
      qc
        .getQueryCache()
        .findAll({ queryKey: classificationReviewLearningQueryKey })
        .some((query) => query.state.isInvalidated),
    ).toBe(true)
  })
})
