import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { FeedbackTriageCommandCenter } from '@/proto/attune/v1/ingest'

export type FeedbackTriageCommandCenterData = FeedbackTriageCommandCenter

export const feedbackTriageCommandCenterQueryKey = [
  'console',
  'feedback',
  'triage-command-center',
] as const

export const feedbackTriageCommandCenterQuery = () =>
  queryOptions({
    queryKey: feedbackTriageCommandCenterQueryKey,
    queryFn: ({ signal }) =>
      api<FeedbackTriageCommandCenterData>('/fb/v1/console/feedback/triage-command-center', {
        signal,
      }),
    staleTime: 30_000,
  })
