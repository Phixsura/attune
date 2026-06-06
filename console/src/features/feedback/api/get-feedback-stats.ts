import { queryOptions } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { GetFeedbackStatsResponse } from '@/proto/attune/v1/ingest'

// Feature-stable alias over the ts-proto response type.
export type FeedbackStats = GetFeedbackStatsResponse

export const feedbackStatsQuery = () =>
  queryOptions({
    queryKey: ['console', 'feedback', 'stats'],
    queryFn: ({ signal }) => api<FeedbackStats>('/fb/v1/console/feedback/stats', { signal }),
    staleTime: 60_000,
  })
