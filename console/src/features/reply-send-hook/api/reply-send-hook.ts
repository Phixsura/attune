import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import { type ApiError, api } from '@/lib/api-client'
import type {
  ListReplySendHookDeliveriesResponse,
  ReplySendHook,
  ReplySendHookDelivery,
  ReplySendHookHealth,
} from '@/proto/attune/v1/ingest'

const queryKey = ['console', 'reply-send-hook'] as const
const endpoint = '/fb/v1/console/reply-send-hook'
export const replySendHookDeliveriesQueryKey = ['console', 'reply-send-hook', 'deliveries'] as const
export const replySendHookHealthQueryKey = ['console', 'reply-send-hook', 'health'] as const

export interface ReplySendHookUpsert {
  enabled: boolean
  name?: string
  secret?: string
  url: string
}

export function replySendHookQuery() {
  return queryOptions({
    queryKey,
    queryFn: async ({ signal }) => {
      try {
        return await api<ReplySendHook>(endpoint, { signal })
      } catch (err) {
        const apiErr = err as ApiError
        if (apiErr.status === 404 || apiErr.status === 409) return null
        throw err
      }
    },
  })
}

export function useUpsertReplySendHook() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: ReplySendHookUpsert) =>
      api<ReplySendHook>(endpoint, {
        method: 'PUT',
        body,
      }),
    onSuccess: (hook) => {
      qc.setQueryData(queryKey, hook)
      invalidateReplySendHookObservability(qc)
    },
  })
}

export function useDisableReplySendHook() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<ReplySendHook>(endpoint, { method: 'DELETE' }),
    onSuccess: (hook) => {
      qc.setQueryData(queryKey, hook)
      invalidateReplySendHookObservability(qc)
    },
  })
}

function invalidateReplySendHookObservability(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: replySendHookHealthQueryKey })
  qc.invalidateQueries({ queryKey: replySendHookDeliveriesQueryKey })
}

export function replySendHookDeliveriesQuery(limit = 25) {
  return queryOptions({
    queryKey: [...replySendHookDeliveriesQueryKey, limit],
    queryFn: async ({ signal }) => {
      const params = new URLSearchParams({ limit: String(limit) })
      const res = await api<ListReplySendHookDeliveriesResponse>(
        `${endpoint}/deliveries?${params.toString()}`,
        { signal },
      )
      return res.items ?? []
    },
  })
}

export function replySendHookHealthQuery() {
  return queryOptions({
    queryKey: replySendHookHealthQueryKey,
    queryFn: ({ signal }) => api<ReplySendHookHealth>(`${endpoint}/health`, { signal }),
  })
}

export function useTestReplySendHook() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      api<ReplySendHookDelivery>(`${endpoint}/test`, {
        method: 'POST',
        body: {},
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey })
      qc.invalidateQueries({ queryKey: replySendHookHealthQueryKey })
      qc.invalidateQueries({ queryKey: replySendHookDeliveriesQueryKey })
    },
  })
}

export function useRedeliverReplySendHookDelivery() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<ReplySendHookDelivery>(`${endpoint}/deliveries/${encodeURIComponent(id)}/redeliver`, {
        method: 'POST',
        body: {},
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey })
      qc.invalidateQueries({ queryKey: replySendHookHealthQueryKey })
      qc.invalidateQueries({ queryKey: replySendHookDeliveriesQueryKey })
    },
  })
}
