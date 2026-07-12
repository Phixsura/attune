import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { InboundSource, ListInboundSourcesResponse } from '@/proto/attune/v1/inbound_source'

// Re-export the proto type under a feature-stable alias so callers
// (table rows, dialog props, route file) don't follow ts-proto rename
// churn.
export type { InboundSource }

// inboundSourcesQuery — list of webhook + email + slack sources for the
// current tenant. The console handler returns rows regardless of enabled, so
// paused sources show up in the table.
export const inboundSourcesQuery = () =>
  queryOptions({
    queryKey: ['console', 'inbound-sources'],
    queryFn: async ({ signal }) => {
      const resp = await api<ListInboundSourcesResponse>('/fb/v1/console/inbound/sources', {
        signal,
      })
      return resp.items
    },
    staleTime: 30_000,
  })
