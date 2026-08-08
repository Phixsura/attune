import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { FeedbackSignalTrace } from '@/proto/attune/v1/ingest'

export type { FeedbackSignalTrace }

export const feedbackSignalTraceQuery = (id: string, limit = 80) =>
  queryOptions({
    queryKey: ['console', 'feedback', 'signal-trace', id, limit] as const,
    queryFn: ({ signal }) =>
      api<FeedbackSignalTrace>(`/fb/v1/console/feedback/${id}/signal-trace?limit=${limit}`, {
        signal,
      }),
    staleTime: 20_000,
  })
