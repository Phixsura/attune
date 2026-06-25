import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { MCPClient, UpdateMCPClientRequest, UpdateMCPClientResponse } from './types'

export const useUpdateMCPClient = () => {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      id,
      body,
    }: {
      id: string
      body: UpdateMCPClientRequest
    }): Promise<MCPClient> => {
      const resp = await api<UpdateMCPClientResponse>(`/fb/v1/console/mcp/clients/${id}`, {
        method: 'PATCH',
        body,
      })
      return resp.client
    },
    onSuccess: (_client, { id }) => {
      queryClient.invalidateQueries({ queryKey: ['console', 'mcp-clients'] })
      queryClient.invalidateQueries({ queryKey: ['console', 'mcp-clients', id] })
    },
  })
}
