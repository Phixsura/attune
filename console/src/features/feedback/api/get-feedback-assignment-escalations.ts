import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { FeedbackAssignmentEscalationQueue } from '@/proto/attune/v1/ingest'

export type FeedbackAssignmentEscalationQueueData = FeedbackAssignmentEscalationQueue

export const feedbackAssignmentEscalationsQueryKey = [
  'console',
  'feedback',
  'assignment-escalations',
] as const

export const feedbackAssignmentEscalationsQuery = (limit = 25) =>
  queryOptions({
    queryKey: [...feedbackAssignmentEscalationsQueryKey, limit],
    queryFn: ({ signal }) =>
      api<FeedbackAssignmentEscalationQueueData>(
        `/fb/v1/console/feedback/assignment/escalations?limit=${limit}`,
        { signal },
      ),
    staleTime: 30_000,
  })
