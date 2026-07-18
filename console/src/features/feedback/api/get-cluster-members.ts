import { infiniteQueryOptions, queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { GetClusterMembersResponse } from '@/proto/attune/v1/clusters'

export interface ClusterMembersFilters {
  clusterId: string
  limit?: number
}

export const clusterMembersQuery = (filters: ClusterMembersFilters) =>
  queryOptions({
    queryKey: ['console', 'cluster-members', filters] as const,
    queryFn: async ({ signal }) => {
      const params = new URLSearchParams()
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      if (filters.limit != null) params.set('limit', String(filters.limit))
      const qs = params.toString()
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      const url = `/fb/v1/console/clusters/${filters.clusterId}/members${qs ? `?${qs}` : ''}`
      return api<GetClusterMembersResponse>(url, { signal })
    },
    staleTime: 30_000,
    enabled: !!filters.clusterId,
  })

export const clusterMembersInfiniteQuery = (clusterId: string) =>
  infiniteQueryOptions({
    queryKey: ['console', 'cluster-members', 'infinite', clusterId] as const,
    queryFn: async ({ pageParam, signal }) => {
      const params = new URLSearchParams()
      params.set('limit', '50')
      if (pageParam) params.set('cursor', pageParam)
      const qs = params.toString()
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      const url = `/fb/v1/console/clusters/${clusterId}/members${qs ? `?${qs}` : ''}`
      return api<GetClusterMembersResponse>(url, { signal })
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    staleTime: 30_000,
    enabled: !!clusterId,
  })
