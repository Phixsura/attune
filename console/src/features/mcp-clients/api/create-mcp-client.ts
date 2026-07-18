import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { CreateMCPClientRequest, CreateMCPClientResponse, MCPClient } from './types'

export const useCreateMCPClient = () => {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (params: CreateMCPClientRequest): Promise<MCPClient> => {
      const resp = await api<CreateMCPClientResponse>('/fb/v1/console/mcp/clients', {
        method: 'POST',
        body: params,
      })
      return resp.client
    },
    onSuccess: (client) => {
      queryClient.setQueryData<MCPClient[]>(['console', 'mcp-clients'], (current) => {
        /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
        const withoutCreated = (current ?? []).filter((item) => item.id !== client.id)
        return [client, ...withoutCreated]
      })
      queryClient.invalidateQueries({ queryKey: ['console', 'mcp-clients'] })
    },
  })
}
