import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import {
  clusterMembersInfiniteQuery,
  clusterMembersQuery,
} from '@/features/feedback/api/get-cluster-members'
import { server } from '@/testing/mocks/server'

function makeQueryClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

describe('cluster member query options', () => {
  it('builds the bounded members URL and disables empty cluster IDs', async () => {
    const captured: { url?: string } = {}
    server.use(
      http.get('/fb/v1/console/clusters/cluster-1/members', ({ request }) => {
        captured.url = request.url
        return HttpResponse.json({ members: [], nextCursor: null })
      }),
    )

    await makeQueryClient().fetchQuery(clusterMembersQuery({ clusterId: 'cluster-1', limit: 25 }))

    const url = new URL(captured.url ?? '')
    expect(url.pathname).toBe('/fb/v1/console/clusters/cluster-1/members')
    expect(url.searchParams.get('limit')).toBe('25')
    expect(clusterMembersQuery({ clusterId: '' }).enabled).toBe(false)
  })

  it('uses default infinite-page size and forwards the next cursor', async () => {
    const requests: string[] = []
    server.use(
      http.get('/fb/v1/console/clusters/cluster-1/members', ({ request }) => {
        requests.push(request.url)
        const cursor = new URL(request.url).searchParams.get('cursor')
        return HttpResponse.json({ members: [], nextCursor: cursor ? null : 'cur-2' })
      }),
    )

    await makeQueryClient().fetchInfiniteQuery({
      ...clusterMembersInfiniteQuery('cluster-1'),
      pages: 2,
    })

    expect(requests).toHaveLength(2)
    expect(new URL(requests[0] ?? '').searchParams.get('limit')).toBe('50')
    expect(new URL(requests[0] ?? '').searchParams.has('cursor')).toBe(false)
    expect(new URL(requests[1] ?? '').searchParams.get('limit')).toBe('50')
    expect(new URL(requests[1] ?? '').searchParams.get('cursor')).toBe('cur-2')
    expect(clusterMembersInfiniteQuery('').enabled).toBe(false)
  })

  it('returns undefined next page params when the backend omits a cursor', () => {
    const options = clusterMembersInfiniteQuery('cluster-1')
    const getNextPageParam = options.getNextPageParam as (page: {
      nextCursor?: string | null
    }) => string | undefined

    expect(getNextPageParam({ nextCursor: 'cur-3' })).toBe('cur-3')
    expect(getNextPageParam({ nextCursor: null })).toBeUndefined()
    expect(getNextPageParam({})).toBeUndefined()
  })
})
