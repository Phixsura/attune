import { useMutation, useQueryClient } from '@tanstack/react-query'
import { feedbackAssignmentEscalationsQueryKey } from '@/features/feedback/api/get-feedback-assignment-escalations'
import { api } from '@/lib/api-client'
import { auditQueryKey } from '@/lib/feedback-audit-api'
import type {
  AssignFeedbackRequest,
  FeedbackAssignment,
  FeedbackDetail,
} from '@/proto/attune/v1/ingest'

export function useAssignFeedback(feedbackId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (request: AssignFeedbackRequest) =>
      api<FeedbackAssignment>(`/fb/v1/console/feedback/${feedbackId}/assignment`, {
        method: 'PATCH',
        body: request,
      }),
    onSuccess: (assignment) => {
      qc.setQueryData<FeedbackDetail>(['console', 'feedback', 'detail', feedbackId], (current) =>
        current ? { ...current, assignment } : current,
      )
    },
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'feedback'] })
      void qc.invalidateQueries({ queryKey: feedbackAssignmentEscalationsQueryKey })
      void qc.invalidateQueries({ queryKey: auditQueryKey(feedbackId) })
    },
  })
}
