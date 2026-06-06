import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'

export function useDeleteNotifyTarget() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<void>(`/fb/v1/console/notify-targets/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'notify-targets'] })
    },
  })
}
