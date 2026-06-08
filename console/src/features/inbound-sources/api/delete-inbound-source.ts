import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

// useDeleteInboundSource removes the row outright. The webhook URL stops
// serving 200 immediately; in-flight outbox deliveries are unaffected.
export function useDeleteInboundSource() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<void>(`/fb/v1/console/inbound/sources/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'inbound-sources'] })
    },
  })
}
