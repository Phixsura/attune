import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { GetUsageResponse } from '@/proto/attune/v1/usage'

// Feature-stable alias over the ts-proto response type.
export type Usage = GetUsageResponse

export const usageQuery = () =>
  queryOptions({
    queryKey: ['console', 'usage'],
    queryFn: ({ signal }) => api<Usage>('/fb/v1/console/usage', { signal }),
    // 1 min — usage updates as ingest comes in; no need to thrash refetch.
    staleTime: 60_000,
  })
