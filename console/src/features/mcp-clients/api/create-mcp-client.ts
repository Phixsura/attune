import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { CreateMCPClientRequest, CreateMCPClientResponse, MCPClient } from './types'

export const useCreateMCPClient = () => {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (params: CreateMCPClientRequest): Promise<MCPClient> => {
      const resp = await api<CreateMCPClientResponse>('/fb/v1/console/mcp/clients', {
        method: 'POST',
        body: JSON.stringify(params),
      })
      return resp.client
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['console', 'mcp-clients'] })
    },
  })
}
