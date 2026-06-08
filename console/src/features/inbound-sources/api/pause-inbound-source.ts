import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { InboundSource } from '@/proto/attune/v1/inbound_source'

// usePauseInboundSource flips the enabled flag to false. Returns the
// freshly-reloaded row so the table can show the new state without a
// follow-up refetch (the cache invalidation handles other tabs).
export function usePauseInboundSource() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<InboundSource>(`/fb/v1/console/inbound/sources/${id}/pause`, { method: 'POST' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'inbound-sources'] })
    },
  })
}
