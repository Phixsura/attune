import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { ListMCPClientsResponse, MCPClient } from './types'

export type { MCPClient }

export const mcpClientsQuery = () =>
  queryOptions({
    queryKey: ['console', 'mcp-clients'],
    queryFn: async ({ signal }) => {
      const resp = await api<ListMCPClientsResponse>('/fb/v1/console/mcp/clients', { signal })
      return resp.clients
    },
    staleTime: 30_000,
  })
