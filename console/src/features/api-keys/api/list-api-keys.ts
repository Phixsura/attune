import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { ApiKey, ListApiKeysResponse } from '@/proto/attune/v1/api_key'

// Re-export the proto type under a feature-stable alias so consumers
// (table rows, dialog props) don't follow ts-proto rename churn.
export type { ApiKey }

export const apiKeysQuery = () =>
  queryOptions({
    queryKey: ['console', 'api-keys'],
    queryFn: async ({ signal }) => {
      const resp = await api<ListApiKeysResponse>('/fb/v1/console/api-keys', { signal })
      return resp.items
    },
    staleTime: 30_000,
  })
