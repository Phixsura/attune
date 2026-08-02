import { useMutation, useQueryClient } from '@tanstack/react-query'
import { feedbackAssignmentEscalationsQueryKey } from '@/features/feedback/api/get-feedback-assignment-escalations'
import { api } from '@/lib/api-client'
import { auditQueryKey } from '@/lib/feedback-audit-api'
import type {
  BatchAssignFeedbackRequest,
  BatchAssignFeedbackResponse,
} from '@/proto/attune/v1/ingest'

export function useBatchAssignFeedback() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (request: BatchAssignFeedbackRequest) =>
      api<BatchAssignFeedbackResponse>('/fb/v1/console/feedback/assignment:batch', {
        method: 'POST',
        body: request,
      }),
    onSettled: (_data, _error, variables) => {
      void qc.invalidateQueries({ queryKey: ['console', 'feedback'] })
      void qc.invalidateQueries({ queryKey: feedbackAssignmentEscalationsQueryKey })
      for (const feedbackID of variables.feedbackIds) {
        void qc.invalidateQueries({ queryKey: ['console', 'feedback', 'detail', feedbackID] })
        void qc.invalidateQueries({ queryKey: auditQueryKey(feedbackID) })
      }
    },
  })
}
