import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

export function useDeleteDigestSubscription() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<void>('/fb/v1/console/digest-subscription', { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'digest-subscription'] })
    },
  })
}
