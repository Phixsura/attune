import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

export const useRevokeMCPSession = () => {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      clientId,
      sessionId,
    }: {
      clientId: string
      sessionId: string
    }): Promise<void> => {
      await api(`/fb/v1/console/mcp/clients/${clientId}/sessions/${sessionId}`, {
        method: 'DELETE',
      })
    },
    onSuccess: (_void, { clientId }) => {
      queryClient.invalidateQueries({ queryKey: ['console', 'mcp-clients', clientId] })
    },
  })
}
