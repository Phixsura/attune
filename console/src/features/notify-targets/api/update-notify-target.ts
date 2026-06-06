import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { NotifyTarget, UpdateNotifyTargetRequest } from '@/proto/attune/v1/notify_target'

// PATCH is sparse — pass only the fields you want to change. Server-side the
// omitted keys stay as-is; empty `secret` string explicitly clears it.
export type NotifyTargetPatch = Omit<UpdateNotifyTargetRequest, 'id'>

export function useUpdateNotifyTarget() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: NotifyTargetPatch }) =>
      api<NotifyTarget>(`/fb/v1/console/notify-targets/${id}`, {
        method: 'PATCH',
        body: patch,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'notify-targets'] })
    },
  })
}
