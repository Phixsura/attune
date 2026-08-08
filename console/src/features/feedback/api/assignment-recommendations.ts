import { useMutation, useQueryClient } from '@tanstack/react-query'
import { feedbackAssignmentEscalationsQueryKey } from '@/features/feedback/api/get-feedback-assignment-escalations'
import { feedbackTriageCommandCenterQueryKey } from '@/features/feedback/api/get-feedback-triage-command-center'
import { api } from '@/lib/api-client'
import { auditQueryKey } from '@/lib/feedback-audit-api'
import type {
  ApplyFeedbackAssignmentRecommendationsRequest,
  ApplyFeedbackAssignmentRecommendationsResponse,
  RecommendFeedbackAssignmentRequest,
  RecommendFeedbackAssignmentResponse,
} from '@/proto/attune/v1/ingest'

export function useRecommendFeedbackAssignment() {
  return useMutation({
    mutationFn: (request: RecommendFeedbackAssignmentRequest) =>
      api<RecommendFeedbackAssignmentResponse>('/fb/v1/console/feedback/assignment:recommend', {
        method: 'POST',
        body: request,
      }),
  })
}

export function useApplyFeedbackAssignmentRecommendations() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (request: ApplyFeedbackAssignmentRecommendationsRequest) =>
      api<ApplyFeedbackAssignmentRecommendationsResponse>(
        '/fb/v1/console/feedback/assignment:apply-recommendations',
        {
          method: 'POST',
          body: request,
        },
      ),
    onSettled: (_data, _error, variables) => {
      void qc.invalidateQueries({ queryKey: ['console', 'feedback'] })
      void qc.invalidateQueries({ queryKey: feedbackAssignmentEscalationsQueryKey })
      void qc.invalidateQueries({ queryKey: feedbackTriageCommandCenterQueryKey })
      for (const feedbackID of variables.feedbackIds) {
        void qc.invalidateQueries({ queryKey: ['console', 'feedback', 'detail', feedbackID] })
        void qc.invalidateQueries({ queryKey: auditQueryKey(feedbackID) })
      }
    },
  })
}
