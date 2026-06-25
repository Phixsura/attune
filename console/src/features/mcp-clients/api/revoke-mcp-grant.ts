import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

export const useRevokeMCPGrant = () => {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({
      clientId,
      grantId,
    }: {
      clientId: string
      grantId: string
    }): Promise<void> => {
      await api(`/fb/v1/console/mcp/clients/${clientId}/grants/${grantId}`, {
        method: 'DELETE',
      })
    },
    onSuccess: (_void, { clientId }) => {
      queryClient.invalidateQueries({ queryKey: ['console', 'mcp-clients', clientId] })
    },
  })
}
