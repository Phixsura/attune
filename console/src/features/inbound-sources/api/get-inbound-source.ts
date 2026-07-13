import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { InboundSource } from '@/proto/attune/v1/inbound_source'

// inboundSourceQuery fetches one source row so the operator detail panel can
// stay in sync with the latest persisted state.
export const inboundSourceQuery = (id: string) =>
  queryOptions({
    queryKey: ['console', 'inbound-sources', 'detail', id],
    queryFn: async ({ signal }) =>
      api<InboundSource>(`/fb/v1/console/inbound/sources/${id}`, { signal }),
    staleTime: 15_000,
  })
