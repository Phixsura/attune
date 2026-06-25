import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { MCPClientDetailResponse } from './types'

export const mcpClientDetailQuery = (id: string | null) =>
  queryOptions({
    queryKey: ['console', 'mcp-clients', id],
    queryFn: async ({ signal }) => {
      return api<MCPClientDetailResponse>(`/fb/v1/console/mcp/clients/${id}`, { signal })
    },
    enabled: Boolean(id),
    staleTime: 15_000,
  })
