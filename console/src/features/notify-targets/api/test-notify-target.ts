import { useMutation } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { TestNotifyTargetResponse } from '@/proto/attune/v1/notify_target'

// Feature-stable alias for the connectivity-test result.
export type NotifyTestResult = TestNotifyTargetResponse

export function useTestNotifyTarget() {
  return useMutation({
    mutationFn: (id: string) =>
      api<NotifyTestResult>(`/fb/v1/console/notify-targets/${id}/test`, { method: 'POST' }),
  })
}
