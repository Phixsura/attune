import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  CreateRequestNotificationWebhookTargetRequest,
  ListRequestNotificationDeliveriesResponse,
  ListRequestNotificationWebhookTargetsResponse,
  ListRequestSubscribersResponse,
  PreviewRequestNotificationRequest,
  PreviewRequestNotificationResponse,
  PublishRequestUpdateRequest,
  RecordRequestNotificationProviderEventRequest,
  RequestNotificationDelivery,
  RequestNotificationEvent,
  RequestNotificationSender,
  RequestNotificationSettings,
  RequestNotificationWebhookTarget,
  RequestNotificationWebhookTestResult,
  RequestSubscriber,
  UpdateRequestNotificationSettingsRequest,
  UpdateRequestNotificationWebhookTargetRequest,
  UpsertRequestNotificationSenderRequest,
} from '@/proto/attune/v1/request_notification'

const endpoint = '/fb/v1/console/request-notifications'

export const requestNotificationSettingsQueryKey = [
  'console',
  'request-notifications',
  'settings',
] as const
export const requestNotificationWebhookTargetsQueryKey = [
  'console',
  'request-notifications',
  'webhook-targets',
] as const
export const requestNotificationSenderQueryKey = [
  'console',
  'request-notifications',
  'sender',
] as const
export const requestNotificationDeliveriesQueryKey = [
  'console',
  'request-notifications',
  'deliveries',
] as const

export function requestNotificationSettingsQuery() {
  return queryOptions({
    queryKey: requestNotificationSettingsQueryKey,
    queryFn: ({ signal }) =>
      api<RequestNotificationSettings>(`${endpoint}/settings`, {
        signal,
      }),
  })
}

export function requestNotificationSenderQuery() {
  return queryOptions({
    queryKey: requestNotificationSenderQueryKey,
    queryFn: async ({ signal }) => {
      try {
        return await api<RequestNotificationSender>(`${endpoint}/sender`, { signal })
      } catch (err) {
        const apiErr = err as { status?: number }
        if (apiErr.status === 404) return null
        throw err
      }
    },
  })
}

export function requestNotificationWebhookTargetsQuery() {
  return queryOptions({
    queryKey: requestNotificationWebhookTargetsQueryKey,
    queryFn: async ({ signal }) => {
      const res = await api<ListRequestNotificationWebhookTargetsResponse>(
        `${endpoint}/webhook-targets`,
        { signal },
      )
      return res.targets ?? []
    },
  })
}

export function requestNotificationDeliveriesQuery(limit = 25) {
  return queryOptions({
    queryKey: [...requestNotificationDeliveriesQueryKey, limit],
    queryFn: async ({ signal }) => {
      const params = new URLSearchParams({ limit: String(limit) })
      const res = await api<ListRequestNotificationDeliveriesResponse>(
        `${endpoint}/deliveries?${params.toString()}`,
        { signal },
      )
      return res.deliveries ?? []
    },
  })
}

export function useUpdateRequestNotificationSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: UpdateRequestNotificationSettingsRequest) =>
      api<RequestNotificationSettings>(`${endpoint}/settings`, {
        method: 'PUT',
        body,
      }),
    onSuccess: (settings) => {
      qc.setQueryData(requestNotificationSettingsQueryKey, settings)
    },
  })
}

export function useUpsertRequestNotificationSender() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: UpsertRequestNotificationSenderRequest) =>
      api<RequestNotificationSender>(`${endpoint}/sender`, {
        method: 'PUT',
        body,
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: requestNotificationSettingsQueryKey })
      qc.invalidateQueries({ queryKey: requestNotificationSenderQueryKey })
    },
  })
}

export function useVerifyRequestNotificationSender() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<RequestNotificationSender>(`${endpoint}/sender:verify`, {
        method: 'POST',
        body: { id },
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: requestNotificationSettingsQueryKey })
      qc.invalidateQueries({ queryKey: requestNotificationSenderQueryKey })
    },
  })
}

export function useCreateRequestNotificationWebhookTarget() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: CreateRequestNotificationWebhookTargetRequest) =>
      api<RequestNotificationWebhookTarget>(`${endpoint}/webhook-targets`, {
        method: 'POST',
        body,
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: requestNotificationWebhookTargetsQueryKey })
    },
  })
}

export function useUpdateRequestNotificationWebhookTarget() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: UpdateRequestNotificationWebhookTargetRequest) =>
      api<RequestNotificationWebhookTarget>(
        `${endpoint}/webhook-targets/${encodeURIComponent(body.id)}`,
        {
          method: 'PATCH',
          body,
        },
      ),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: requestNotificationWebhookTargetsQueryKey })
    },
  })
}

export function useDeleteRequestNotificationWebhookTarget() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<void>(`${endpoint}/webhook-targets/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: requestNotificationWebhookTargetsQueryKey })
    },
  })
}

export function useTestRequestNotificationWebhookTarget() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<RequestNotificationWebhookTestResult>(
        `${endpoint}/webhook-targets/${encodeURIComponent(id)}:test`,
        {
          method: 'POST',
          body: {},
        },
      ),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: requestNotificationWebhookTargetsQueryKey })
      qc.invalidateQueries({ queryKey: requestNotificationDeliveriesQueryKey })
    },
  })
}

export function usePreviewRequestNotification() {
  return useMutation({
    mutationFn: (body: PreviewRequestNotificationRequest) =>
      api<PreviewRequestNotificationResponse>(`${endpoint}/preview`, {
        method: 'POST',
        body,
      }),
  })
}

export function usePublishRequestUpdate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: PublishRequestUpdateRequest) =>
      api<RequestNotificationEvent>(`${endpoint}/publish`, {
        method: 'POST',
        body,
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: requestNotificationDeliveriesQueryKey })
    },
  })
}

export function useRetryRequestNotificationDelivery() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<RequestNotificationDelivery>(`${endpoint}/deliveries/${encodeURIComponent(id)}:retry`, {
        method: 'POST',
        body: {},
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: requestNotificationDeliveriesQueryKey })
    },
  })
}

export function useListRequestNotificationSubscribers() {
  return useMutation({
    mutationFn: async (requestId: string) => {
      const res = await api<ListRequestSubscribersResponse>(
        `${endpoint}/requests/${encodeURIComponent(requestId)}/subscribers`,
      )
      return res.subscribers ?? []
    },
  })
}

export function useSuppressRequestNotificationSubscriber() {
  return useMutation({
    mutationFn: ({ contactId, reason }: { contactId: string; reason: string }) =>
      api<RequestSubscriber>(`${endpoint}/subscribers/${encodeURIComponent(contactId)}:suppress`, {
        method: 'POST',
        body: { reason },
      }),
  })
}

export function useRecordRequestNotificationProviderEvent() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: RecordRequestNotificationProviderEventRequest) =>
      api<RequestSubscriber>(`${endpoint}/provider-events:suppress`, {
        method: 'POST',
        body,
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: requestNotificationDeliveriesQueryKey })
    },
  })
}
