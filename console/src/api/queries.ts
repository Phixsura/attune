import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import type { ApiKey, CreateApiKeyResponse, ListApiKeysResponse } from '../proto/attune/v1/api_key'
import type {
  Feedback,
  FeedbackDetail,
  GetFeedbackStatsResponse,
  ListFeedbackResponse,
} from '../proto/attune/v1/ingest'
import type {
  CreateNotifyTargetRequest,
  ListNotifyTargetsResponse,
  NotifyTarget,
  TestNotifyTargetResponse,
  UpdateNotifyTargetRequest,
} from '../proto/attune/v1/notify_target'
import type { GetMeResponse } from '../proto/attune/v1/session'
import type { GetUsageResponse } from '../proto/attune/v1/usage'
import { api, setCsrfToken } from './client'

// Stable aliases over the ts-proto-generated types so the rest of the console
// imports names that don't change when the proto evolves.
export type SessionMe = GetMeResponse
export type { ApiKey }
export type NewApiKey = CreateApiKeyResponse
export type { NotifyTarget }
export type NotifyTargetCreate = CreateNotifyTargetRequest
export type NotifyTargetPatch = Omit<UpdateNotifyTargetRequest, 'id'>
export type NotifyTestResult = TestNotifyTargetResponse
export type { Feedback, FeedbackDetail }
export type FeedbackStats = GetFeedbackStatsResponse
export type Usage = GetUsageResponse

export interface FeedbackListFilters {
  kind?: string
  severity?: string
  q?: string
}

// Query options for the current session. Used by route loaders + UI.
//
// `retry: false` so a 401 on first boot doesn't double-flash the login
// page; bare 401 = "not logged in" not "transient network error".
export const meQuery = () =>
  queryOptions({
    queryKey: ['console', 'me'],
    queryFn: async ({ signal }) => {
      const me = await api<SessionMe>('/fb/v1/console/me', { signal })
      setCsrfToken(me.csrfToken)
      return me
    },
    retry: false,
    staleTime: 60_000,
  })

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<void>('/fb/v1/console/logout', { method: 'POST' }),
    onSettled: () => {
      setCsrfToken(null)
      qc.removeQueries({ queryKey: ['console', 'me'] })
    },
  })
}

// ── API keys ────────────────────────────────────────────────────────

export const apiKeysQuery = () =>
  queryOptions({
    queryKey: ['console', 'api-keys'],
    queryFn: async ({ signal }) => {
      const resp = await api<ListApiKeysResponse>('/fb/v1/console/api-keys', { signal })
      return resp.items
    },
    staleTime: 30_000,
  })

export function useCreateApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (label: string) =>
      api<NewApiKey>('/fb/v1/console/api-keys', { method: 'POST', body: { label } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'api-keys'] })
    },
  })
}

export function useRevokeApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api<void>(`/fb/v1/console/api-keys/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'api-keys'] })
    },
  })
}

// ── Notify targets ─────────────────────────────────────────────────

export const notifyTargetsQuery = () =>
  queryOptions({
    queryKey: ['console', 'notify-targets'],
    queryFn: async ({ signal }) => {
      const resp = await api<ListNotifyTargetsResponse>('/fb/v1/console/notify-targets', {
        signal,
      })
      return resp.items
    },
    staleTime: 30_000,
  })

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

export function useDeleteNotifyTarget() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<void>(`/fb/v1/console/notify-targets/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'notify-targets'] })
    },
  })
}

// PATCH is sparse — pass only the fields you want to change. Server-side
// the omitted keys stay as-is; empty `secret` string explicitly clears it.
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

export function useTestNotifyTarget() {
  return useMutation({
    mutationFn: (id: string) =>
      api<NotifyTestResult>(`/fb/v1/console/notify-targets/${id}/test`, { method: 'POST' }),
  })
}

// ── Feedback ─────────────────────────────────────────────────────────

// Infinite query for cursor pagination. Each page is one server call;
// TanStack Query handles page concatenation + de-dup via getPageParam.
export const feedbackListInfiniteQuery = (filters: FeedbackListFilters) => ({
  queryKey: ['console', 'feedback', filters] as const,
  queryFn: async ({ pageParam, signal }: { pageParam: string | null; signal: AbortSignal }) => {
    const params = new URLSearchParams()
    if (filters.kind) params.set('kind', filters.kind)
    if (filters.severity) params.set('severity', filters.severity)
    if (filters.q) params.set('q', filters.q)
    if (pageParam) params.set('cursor', pageParam)
    const qs = params.toString()
    const url = `/fb/v1/console/feedback${qs ? `?${qs}` : ''}`
    return api<ListFeedbackResponse>(url, { signal })
  },
  initialPageParam: null as string | null,
  getNextPageParam: (last: ListFeedbackResponse) => last.nextCursor ?? null,
  staleTime: 15_000,
})

export const feedbackStatsQuery = () =>
  queryOptions({
    queryKey: ['console', 'feedback', 'stats'],
    queryFn: ({ signal }) => api<FeedbackStats>('/fb/v1/console/feedback/stats', { signal }),
    staleTime: 60_000,
  })

export const feedbackDetailQuery = (id: string) =>
  queryOptions({
    queryKey: ['console', 'feedback', 'detail', id],
    queryFn: ({ signal }) => api<FeedbackDetail>(`/fb/v1/console/feedback/${id}`, { signal }),
    staleTime: 30_000,
  })

// ── Usage ────────────────────────────────────────────────────────────

export const usageQuery = () =>
  queryOptions({
    queryKey: ['console', 'usage'],
    queryFn: ({ signal }) => api<Usage>('/fb/v1/console/usage', { signal }),
    // 1 min — usage updates as ingest comes in; no need to thrash refetch.
    staleTime: 60_000,
  })
