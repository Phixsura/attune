import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

export function useRemoveMember() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api<void>(`/fb/v1/console/members/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'members'] })
    },
  })
}
