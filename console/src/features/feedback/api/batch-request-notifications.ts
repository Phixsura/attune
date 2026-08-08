import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  BatchPreviewRequestNotificationsRequest,
  BatchPreviewRequestNotificationsResponse,
  BatchPublishRequestUpdatesRequest,
  BatchPublishRequestUpdatesResponse,
} from '@/proto/attune/v1/request_notification'

const endpoint = '/fb/v1/console/request-notifications'
const requestNotificationsQueryKey = ['console', 'request-notifications'] as const

export function useBatchPreviewFeedbackRequestNotifications() {
  return useMutation({
    mutationFn: (body: BatchPreviewRequestNotificationsRequest) =>
      api<BatchPreviewRequestNotificationsResponse>(`${endpoint}:batch-preview`, {
        method: 'POST',
        body,
      }),
  })
}

export function useBatchPublishFeedbackRequestUpdates() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: BatchPublishRequestUpdatesRequest) =>
      api<BatchPublishRequestUpdatesResponse>(`${endpoint}:batch-publish`, {
        method: 'POST',
        body,
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: requestNotificationsQueryKey })
    },
  })
}
