import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import {
  type FeedbackListFilters,
  feedbackListInfiniteQuery,
} from '@/features/feedback/api/list-feedback-infinite'
import { server } from '@/testing/mocks/server'

// The feedback list URL builder turns (attrs + q + urgent + cursor)
// into a per-dim querystring the backend translates into JSONB
// containment clauses. The test asserts the exact querystring shape
// MSW sees, since that IS the wire contract.

function makeQc() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function captureRequestURL(captured: { url?: string }) {
  server.use(
    http.get('/fb/v1/console/feedback', ({ request }) => {
      captured.url = request.url
      return HttpResponse.json({ items: [], nextCursor: null })
    }),
  )
}

describe('feedbackListInfiniteQuery URL builder', () => {
  it('empty filters → GET /fb/v1/console/feedback with no querystring', async () => {
    const captured: { url?: string } = {}
    captureRequestURL(captured)
    const qc = makeQc()
    const filters: FeedbackListFilters = { attrs: [] }
    await qc.fetchInfiniteQuery(feedbackListInfiniteQuery(filters))
    expect(captured.url).toBeDefined()
    expect(new URL(captured.url ?? '').search).toBe('')
  })

  it('attrs entries with empty dim or empty value are skipped (per :34-36)', async () => {
    const captured: { url?: string } = {}
    captureRequestURL(captured)
    const filters: FeedbackListFilters = {
      attrs: [
        { dim: 'severity', value: 'P0' },
        { dim: '', value: 'shouldNotAppear' },
        { dim: 'type', value: '' },
        { dim: 'modules', value: 'payment' },
      ],
    }
    await makeQc().fetchInfiniteQuery(feedbackListInfiniteQuery(filters))
    const url = new URL(captured.url ?? '')
    expect(url.searchParams.get('severity')).toBe('P0')
    expect(url.searchParams.get('modules')).toBe('payment')
    expect(url.searchParams.has('type')).toBe(false)
    expect(url.searchParams.has('')).toBe(false)
  })

  it('q and urgent map to their query params; urgent: false sent as "false"', async () => {
    const captured: { url?: string } = {}
    captureRequestURL(captured)
    const filters: FeedbackListFilters = { attrs: [], q: 'hello', urgent: false }
    await makeQc().fetchInfiniteQuery(feedbackListInfiniteQuery(filters))
    const url = new URL(captured.url ?? '')
    expect(url.searchParams.get('q')).toBe('hello')
    expect(url.searchParams.get('urgent')).toBe('false') // string, not omitted
  })

  it('accountKey maps to the account_key backend filter', async () => {
    const captured: { url?: string } = {}
    captureRequestURL(captured)
    const filters: FeedbackListFilters = { attrs: [], accountKey: 'acct:acme' }
    await makeQc().fetchInfiniteQuery(feedbackListInfiniteQuery(filters))
    const url = new URL(captured.url ?? '')
    expect(url.searchParams.get('account_key')).toBe('acct:acme')
  })

  it('urgent: true sent as "true"', async () => {
    const captured: { url?: string } = {}
    captureRequestURL(captured)
    await makeQc().fetchInfiniteQuery(feedbackListInfiniteQuery({ attrs: [], urgent: true }))
    expect(new URL(captured.url ?? '').searchParams.get('urgent')).toBe('true')
  })

  it('classification quality drilldown filters map to backend params', async () => {
    const captured: { url?: string } = {}
    captureRequestURL(captured)
    await makeQc().fetchInfiniteQuery(
      feedbackListInfiniteQuery({
        attrs: [],
        ids: ['101', '102'],
        confidenceLte: 0.6,
        createdFrom: '2026-07-01T00:00:00Z',
        createdTo: '2026-07-02T00:00:00Z',
        qualitySignal: 'low_confidence',
      }),
    )
    const url = new URL(captured.url ?? '')
    expect(url.searchParams.get('ids')).toBe('101,102')
    expect(url.searchParams.get('confidence_lte')).toBe('0.6')
    expect(url.searchParams.get('created_from')).toBe('2026-07-01T00:00:00Z')
    expect(url.searchParams.get('created_to')).toBe('2026-07-02T00:00:00Z')
    expect(url.searchParams.get('quality_signal')).toBe('low_confidence')
  })

  it('enriched date drilldown filters map to backend params', async () => {
    const captured: { url?: string } = {}
    captureRequestURL(captured)
    await makeQc().fetchInfiniteQuery(
      feedbackListInfiniteQuery({
        attrs: [],
        enrichedFrom: '2026-07-03T00:00:00Z',
        enrichedTo: '2026-07-04T00:00:00Z',
      }),
    )
    const url = new URL(captured.url ?? '')
    expect(url.searchParams.get('enriched_from')).toBe('2026-07-03T00:00:00Z')
    expect(url.searchParams.get('enriched_to')).toBe('2026-07-04T00:00:00Z')
  })

  it('getNextPageParam returns nextCursor; null ends pagination', () => {
    const options = feedbackListInfiniteQuery({ attrs: [] })
    const fn = options.getNextPageParam as (page: { nextCursor?: string | null }) => string | null
    expect(fn({ nextCursor: 'cur-2' })).toBe('cur-2')
    expect(fn({ nextCursor: null })).toBeNull()
    expect(fn({})).toBeNull()
  })

  it('pageParam → second-page request uses ?cursor=<token>', async () => {
    const captured: { url?: string } = {}
    server.use(
      http.get('/fb/v1/console/feedback', ({ request }) => {
        captured.url = request.url
        const cur = new URL(request.url).searchParams.get('cursor')
        return HttpResponse.json({ items: [], nextCursor: cur ? null : 'cur-2' })
      }),
    )
    const qc = makeQc()
    const filters: FeedbackListFilters = { attrs: [] }
    // First fetch returns nextCursor="cur-2"; the second page should send ?cursor=cur-2.
    await qc.fetchInfiniteQuery({ ...feedbackListInfiniteQuery(filters), pages: 2 })
    expect(new URL(captured.url ?? '').searchParams.get('cursor')).toBe('cur-2')
  })
})
