import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { InboundSource } from '@/proto/attune/v1/inbound_source'

// useResumeInboundSource flips the enabled flag back to true.
export function useResumeInboundSource() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<InboundSource>(`/fb/v1/console/inbound/sources/${id}/resume`, { method: 'POST' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'inbound-sources'] })
    },
  })
}
