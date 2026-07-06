import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { ListServiceAccountsResponse, ServiceAccount } from '@/proto/attune/v1/api_key'

export type { ServiceAccount }

export const serviceAccountsQueryKey = ['console', 'service-accounts'] as const

export const serviceAccountsQuery = () =>
  queryOptions({
    queryKey: serviceAccountsQueryKey,
    queryFn: async ({ signal }) => {
      const resp = await api<ListServiceAccountsResponse>('/fb/v1/console/service-accounts', {
        signal,
      })
      return resp.items
    },
    staleTime: 30_000,
  })
