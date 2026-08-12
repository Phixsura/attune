import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  AnomalyConfig,
  AnomalyEvent,
  GetAnomalyConfigResponse,
  GetAnomalyEvidenceResponse,
  GetAnomalySeriesResponse,
  ListAnomaliesResponse,
  SeriesPoint,
  UpdateAnomalyConfigRequest,
  UpdateAnomalyConfigResponse,
} from '@/proto/attune/v1/anomaly'

export type { AnomalyConfig, AnomalyEvent, SeriesPoint }

export type AnomalyStatus = 'open' | 'resolved' | 'retracted'

export const anomaliesQueryKey = ['console', 'anomalies'] as const

export interface AnomaliesFilters {
  status?: AnomalyStatus | 'all'
  limit?: number
}

export const anomaliesQuery = (filters: AnomaliesFilters = {}) =>
  queryOptions({
    queryKey: [...anomaliesQueryKey, filters.status ?? 'open', filters.limit ?? 50],
    queryFn: async ({ signal }) => {
      const qs = new URLSearchParams()
      const status = filters.status ?? 'open'
      if (status !== 'all') qs.set('status', status)
      qs.set('limit', String(filters.limit ?? 50))
      const resp = await api<ListAnomaliesResponse>(`/fb/v1/console/anomalies?${qs.toString()}`, {
        signal,
      })
      return resp.events
    },
    staleTime: 30_000,
  })

export const anomalySeriesQuery = (sliceType: string, sliceKey: string, days = 90) =>
  queryOptions({
    queryKey: [...anomaliesQueryKey, 'series', sliceType, sliceKey, days],
    queryFn: async ({ signal }) => {
      const qs = new URLSearchParams({
        days: String(days),
        slice_key: sliceKey,
        slice_type: sliceType,
      })
      return api<GetAnomalySeriesResponse>(`/fb/v1/console/anomalies/series?${qs.toString()}`, {
        signal,
      })
    },
    staleTime: 60_000,
  })

export const anomalyEvidenceQuery = (eventId: string) =>
  queryOptions({
    queryKey: [...anomaliesQueryKey, 'evidence', eventId],
    queryFn: async ({ signal }) =>
      api<GetAnomalyEvidenceResponse>(`/fb/v1/console/anomalies/${eventId}/evidence`, { signal }),
    staleTime: 60_000,
  })

export const anomalyConfigQueryKey = ['console', 'anomaly-config'] as const

export const anomalyConfigQuery = () =>
  queryOptions({
    queryKey: anomalyConfigQueryKey,
    queryFn: async ({ signal }) => {
      const resp = await api<GetAnomalyConfigResponse>('/fb/v1/console/anomaly-config', { signal })
      return resp.config
    },
    staleTime: 30_000,
  })

export function useUpdateAnomalyConfig() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: UpdateAnomalyConfigRequest) =>
      api<UpdateAnomalyConfigResponse>('/fb/v1/console/anomaly-config', {
        body,
        method: 'POST',
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: anomalyConfigQueryKey })
      void qc.invalidateQueries({ queryKey: anomaliesQueryKey })
    },
  })
}
