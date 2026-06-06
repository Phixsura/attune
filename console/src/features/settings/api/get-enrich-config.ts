import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { EnrichConfig, GetEnrichConfigResponse } from '@/proto/attune/v1/enrich_config'

export type { EnrichConfig }

export const enrichConfigQuery = () =>
  queryOptions({
    queryKey: ['console', 'enrich-config'],
    queryFn: async ({ signal }) => {
      const resp = await api<GetEnrichConfigResponse>('/fb/v1/console/enrich-config', { signal })
      return resp.config
    },
    staleTime: 30_000,
  })
