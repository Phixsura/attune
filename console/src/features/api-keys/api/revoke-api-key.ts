import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

// "Revoke" is the user-visible action; the underlying HTTP verb is DELETE.
// File name matches the hook name (revoke-*) for internal consistency.
export function useRevokeApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api<void>(`/fb/v1/console/api-keys/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'api-keys'] })
    },
  })
}
