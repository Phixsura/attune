import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

export function useSyncNow() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<{ id: string }>(`/fb/v1/console/inbound/sources/${id}/sync-now`, {
        method: 'POST',
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['console', 'inbound-sources'] })
    },
  })
}
