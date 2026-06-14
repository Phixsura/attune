import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { AuditEntry, ListAuditResponse } from '@/proto/attune/v1/workflow'

export type { AuditEntry }

export const auditQueryKey = (feedbackId: number) =>
  ['console', 'feedback', feedbackId, 'audit'] as const

export const auditQuery = (feedbackId: number) =>
  queryOptions({
    queryKey: auditQueryKey(feedbackId),
    queryFn: async ({ signal }) => {
      const resp = await api<ListAuditResponse>(`/fb/v1/console/feedback/${feedbackId}/audit`, {
        signal,
      })
      return resp.entries ?? []
    },
    staleTime: 15_000,
  })
