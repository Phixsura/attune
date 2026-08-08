import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import { feedbackAssignmentEscalationsQueryKey } from '@/features/feedback/api/get-feedback-assignment-escalations'
import { feedbackTriageCommandCenterQueryKey } from '@/features/feedback/api/get-feedback-triage-command-center'
import { api } from '@/lib/api-client'
import type {
  DryRunFeedbackAssignmentPolicyRequest,
  DryRunFeedbackAssignmentPolicyResponse,
  FeedbackAssignmentPolicy,
  ListFeedbackAssignmentPolicyRevisionsResponse,
  RestoreFeedbackAssignmentPolicyRequest,
  UpdateFeedbackAssignmentPolicyRequest,
} from '@/proto/attune/v1/ingest'

export const feedbackAssignmentPolicyQueryKey = [
  'console',
  'feedback',
  'assignment-policy',
] as const

export const feedbackAssignmentPolicyQuery = () =>
  queryOptions({
    queryKey: feedbackAssignmentPolicyQueryKey,
    queryFn: ({ signal }) =>
      api<FeedbackAssignmentPolicy>('/fb/v1/console/feedback/assignment/policy', { signal }),
    staleTime: 30_000,
  })

export const feedbackAssignmentPolicyRevisionsQueryKey = [
  ...feedbackAssignmentPolicyQueryKey,
  'revisions',
] as const

export const feedbackAssignmentPolicyRevisionsQuery = () =>
  queryOptions({
    queryKey: feedbackAssignmentPolicyRevisionsQueryKey,
    queryFn: ({ signal }) =>
      api<ListFeedbackAssignmentPolicyRevisionsResponse>(
        '/fb/v1/console/feedback/assignment/policy/revisions',
        { signal },
      ),
    staleTime: 30_000,
  })

export function useUpdateFeedbackAssignmentPolicy() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (request: UpdateFeedbackAssignmentPolicyRequest) =>
      api<FeedbackAssignmentPolicy>('/fb/v1/console/feedback/assignment/policy', {
        method: 'PUT',
        body: request,
      }),
    onSuccess: (policy) => {
      qc.setQueryData(feedbackAssignmentPolicyQueryKey, policy)
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: feedbackAssignmentPolicyQueryKey })
      void qc.invalidateQueries({ queryKey: feedbackAssignmentPolicyRevisionsQueryKey })
      void qc.invalidateQueries({ queryKey: feedbackAssignmentEscalationsQueryKey })
      void qc.invalidateQueries({ queryKey: feedbackTriageCommandCenterQueryKey })
    },
  })
}

export function useDryRunFeedbackAssignmentPolicy() {
  return useMutation({
    mutationFn: (request: DryRunFeedbackAssignmentPolicyRequest) =>
      api<DryRunFeedbackAssignmentPolicyResponse>(
        '/fb/v1/console/feedback/assignment/policy:dry-run',
        {
          method: 'POST',
          body: request,
        },
      ),
  })
}

export function useRestoreFeedbackAssignmentPolicy() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (request: RestoreFeedbackAssignmentPolicyRequest) =>
      api<FeedbackAssignmentPolicy>('/fb/v1/console/feedback/assignment/policy:restore', {
        method: 'POST',
        body: request,
      }),
    onSuccess: (policy) => {
      qc.setQueryData(feedbackAssignmentPolicyQueryKey, policy)
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: feedbackAssignmentPolicyQueryKey })
      void qc.invalidateQueries({ queryKey: feedbackAssignmentPolicyRevisionsQueryKey })
      void qc.invalidateQueries({ queryKey: feedbackAssignmentEscalationsQueryKey })
      void qc.invalidateQueries({ queryKey: feedbackTriageCommandCenterQueryKey })
    },
  })
}
