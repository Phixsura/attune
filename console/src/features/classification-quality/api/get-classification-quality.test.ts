import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import {
  classificationQualityQuery,
  classificationQualitySearchParams,
  normalizeClassificationQualityFilters,
  qualityWindowForRange,
} from '@/features/classification-quality/api/get-classification-quality'
import { server } from '@/testing/mocks/server'

function makeQc() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
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
})
