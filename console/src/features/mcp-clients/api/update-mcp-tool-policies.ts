import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  MCPClientTool,
  ReplaceMCPToolPoliciesRequest,
  ReplaceMCPToolPoliciesResponse,
} from './types'

export const useUpdateMCPToolPolicies = () => {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      id,
      body,
    }: {
      id: string
      body: ReplaceMCPToolPoliciesRequest
    }): Promise<MCPClientTool[]> => {
      const resp = await api<ReplaceMCPToolPoliciesResponse>(
        `/fb/v1/console/mcp/clients/${id}/tool-policies`,
        {
          method: 'PUT',
          body,
        },
      )
      return resp.tools
    },
    onSuccess: (_tools, { id }) => {
      queryClient.invalidateQueries({ queryKey: ['console', 'mcp-clients', id] })
    },
  })
}
