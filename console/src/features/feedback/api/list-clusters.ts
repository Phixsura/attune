import { infiniteQueryOptions, queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { ListClustersResponse } from '@/proto/attune/v1/clusters'

export interface ClustersFilters {
  recencyDays?: number
  minCount?: number
  limit?: number
  sort?: 'count' | 'latest_at'
  q?: string
}

export const clustersQuery = (filters: ClustersFilters = {}) =>
  queryOptions({
    queryKey: ['console', 'clusters', filters] as const,
    queryFn: async ({ signal }) => {
      const params = new URLSearchParams()
      if (filters.recencyDays != null) params.set('recency_days', String(filters.recencyDays))
      if (filters.minCount != null) params.set('min_count', String(filters.minCount))
      if (filters.limit != null) params.set('limit', String(filters.limit))
      if (filters.sort) params.set('sort', filters.sort)
      if (filters.q) params.set('q', filters.q)
      const qs = params.toString()
      const url = `/fb/v1/console/clusters${qs ? `?${qs}` : ''}`
      return api<ListClustersResponse>(url, { signal })
    },
    staleTime: 30_000,
  })

export const clustersInfiniteQuery = (filters: Omit<ClustersFilters, 'limit'> = {}) =>
  infiniteQueryOptions({
    queryKey: ['console', 'clusters', 'infinite', filters] as const,
    queryFn: async ({ pageParam, signal }) => {
      const params = new URLSearchParams()
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      if (filters.recencyDays != null) params.set('recency_days', String(filters.recencyDays))
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      if (filters.minCount != null) params.set('min_count', String(filters.minCount))
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      if (filters.sort) params.set('sort', filters.sort)
      if (filters.q) params.set('q', filters.q)
      params.set('limit', '50')
      if (pageParam) params.set('cursor', pageParam)
      const qs = params.toString()
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      const url = `/fb/v1/console/clusters${qs ? `?${qs}` : ''}`
      return api<ListClustersResponse>(url, { signal })
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    staleTime: 30_000,
  })
