import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { clustersInfiniteQuery, clustersQuery } from '@/features/feedback/api/list-clusters'
import { server } from '@/testing/mocks/server'

function makeQueryClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

describe('cluster list query options', () => {
  it('builds a bare clusters URL when no filters are provided', async () => {
    const requests: string[] = []
    server.use(
      http.get('/fb/v1/console/clusters', ({ request }) => {
        requests.push(request.url)
        return HttpResponse.json({ items: [], nextCursor: null, clusteringEnabled: true })
      }),
    )

    await makeQueryClient().fetchQuery(clustersQuery())

    const url = new URL(requests[0] ?? '')
    expect(url.pathname).toBe('/fb/v1/console/clusters')
    expect(url.search).toBe('')
  })

  it('serializes all bounded cluster filters', async () => {
    const requests: string[] = []
    server.use(
      http.get('/fb/v1/console/clusters', ({ request }) => {
        requests.push(request.url)
        return HttpResponse.json({ items: [], nextCursor: null, clusteringEnabled: true })
      }),
    )

    await makeQueryClient().fetchQuery(
      clustersQuery({
        recencyDays: 14,
        minCount: 3,
        limit: 25,
        sort: 'latest_at',
        q: 'billing',
      }),
    )

    const params = new URL(requests[0] ?? '').searchParams
    expect(params.get('recency_days')).toBe('14')
    expect(params.get('min_count')).toBe('3')
    expect(params.get('limit')).toBe('25')
    expect(params.get('sort')).toBe('latest_at')
    expect(params.get('q')).toBe('billing')
  })

  it('uses the default infinite size and forwards cursors', async () => {
    const requests: string[] = []
    server.use(
      http.get('/fb/v1/console/clusters', ({ request }) => {
        requests.push(request.url)
        const cursor = new URL(request.url).searchParams.get('cursor')
        return HttpResponse.json({
          items: [],
          nextCursor: cursor ? null : 'next-cluster',
          clusteringEnabled: true,
        })
      }),
    )

    await makeQueryClient().fetchInfiniteQuery({
      ...clustersInfiniteQuery({ recencyDays: 30, minCount: 2, sort: 'count', q: 'login' }),
      pages: 2,
    })

    const first = new URL(requests[0] ?? '').searchParams
    const second = new URL(requests[1] ?? '').searchParams
    expect(first.get('limit')).toBe('50')
    expect(first.get('recency_days')).toBe('30')
    expect(first.get('min_count')).toBe('2')
    expect(first.get('sort')).toBe('count')
    expect(first.get('q')).toBe('login')
    expect(first.has('cursor')).toBe(false)
    expect(second.get('limit')).toBe('50')
    expect(second.get('cursor')).toBe('next-cluster')
  })

  it('returns undefined next page params when the backend omits a cursor', () => {
    const options = clustersInfiniteQuery()
    const getNextPageParam = options.getNextPageParam as (page: {
      nextCursor?: string | null
    }) => string | undefined

    expect(getNextPageParam({ nextCursor: 'cur-1' })).toBe('cur-1')
    expect(getNextPageParam({ nextCursor: null })).toBeUndefined()
    expect(getNextPageParam({})).toBeUndefined()
  })
})
