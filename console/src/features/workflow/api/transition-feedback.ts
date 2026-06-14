import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  BatchTransitionFeedbackRequest,
  BatchTransitionFeedbackResponse,
  TransitionFeedbackResponse,
} from '@/proto/attune/v1/workflow'

export function useTransitionFeedback() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (req: { feedbackId: number; toStateId: string; comment?: string }) =>
      api<TransitionFeedbackResponse>(`/fb/v1/console/feedback/${req.feedbackId}/transition`, {
        method: 'POST',
        body: { toStateId: req.toStateId, comment: req.comment ?? '' },
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'feedback'] })
    },
  })
}

export function useBatchTransitionFeedback() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (req: BatchTransitionFeedbackRequest) =>
      api<BatchTransitionFeedbackResponse>('/fb/v1/console/feedback/transition/batch', {
        method: 'POST',
        body: req,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'feedback'] })
    },
  })
}
