import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  ListQualityActionsResponse,
  QualityAction,
  UpdateQualityActionRequest,
  UpdateQualityActionResponse,
} from '@/proto/attune/v1/quality_action'

export type { QualityAction, UpdateQualityActionRequest }

export type QualityActionStatus = 'open' | 'acknowledged' | 'resolved' | 'dismissed'

export interface QualityActionsFilters {
  status?: QualityActionStatus | 'all'
  limit?: number
}

export const qualityActionsQueryKey = ['console', 'quality-actions'] as const

export const qualityActionsQuery = (filters: QualityActionsFilters = {}) =>
  queryOptions({
    queryKey: [...qualityActionsQueryKey, normalizeQualityActionsFilters(filters)],
    queryFn: async ({ signal }) => {
      const qs = qualityActionsSearchParams(filters)
      const suffix = qs.size > 0 ? `?${qs.toString()}` : ''
      const resp = await api<ListQualityActionsResponse>(
        `/fb/v1/console/quality-actions${suffix}`,
        {
          signal,
        },
      )
      return resp.actions
    },
    staleTime: 30_000,
  })

export function useUpdateQualityAction() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: UpdateQualityActionRequest) =>
      api<UpdateQualityActionResponse>('/fb/v1/console/quality-actions/update', {
        method: 'POST',
        body,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qualityActionsQueryKey })
    },
  })
}

export function qualityActionsSearchParams(filters: QualityActionsFilters) {
  const merged = normalizeQualityActionsFilters(filters)
  const qs = new URLSearchParams()
  if (merged.status !== 'all') qs.set('status', merged.status)
  qs.set('limit', String(merged.limit))
  return qs
}

export function normalizeQualityActionsFilters(filters: QualityActionsFilters) {
  return {
    status: filters.status ?? 'all',
    limit: clampQualityActionsLimit(filters.limit),
  }
}

function clampQualityActionsLimit(limit: number | undefined) {
  if (!limit || limit < 1) return 100
  return Math.min(Math.floor(limit), 200)
}
