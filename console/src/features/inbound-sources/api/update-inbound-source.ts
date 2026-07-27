import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { InboundSource, UpdateInboundSourceRequest } from '@/proto/attune/v1/inbound_source'

// useUpdateInboundSource PATCHes a source's mutable settings in place —
// name and channel-specific config — preserving the sync watermark and
// feedback linkage a delete/recreate would destroy.
export function useUpdateInboundSource() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...body }: UpdateInboundSourceRequest) =>
      api<InboundSource>(`/fb/v1/console/inbound/sources/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        body: { id, ...body },
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['console', 'inbound-sources'] })
    },
  })
}
