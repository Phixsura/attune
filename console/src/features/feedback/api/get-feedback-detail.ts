import { queryOptions } from '@tanstack/react-query'
import { api } from '@/api/client'
import type { FeedbackDetail } from '@/proto/attune/v1/ingest'

// Re-export so consumers (sheet, future detail panels) get the type from the
// same module that produces it.
export type { FeedbackDetail }

export const feedbackDetailQuery = (id: string) =>
  queryOptions({
    queryKey: ['console', 'feedback', 'detail', id],
    queryFn: ({ signal }) => api<FeedbackDetail>(`/fb/v1/console/feedback/${id}`, { signal }),
    staleTime: 30_000,
  })
