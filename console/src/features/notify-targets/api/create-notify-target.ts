import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { CreateNotifyTargetRequest, NotifyTarget } from '@/proto/attune/v1/notify_target'

// Feature-stable alias for the create payload.
export type NotifyTargetCreate = CreateNotifyTargetRequest

export function useCreateNotifyTarget() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: NotifyTargetCreate) =>
      api<NotifyTarget>('/fb/v1/console/notify-targets', { method: 'POST', body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'notify-targets'] })
    },
  })
}
