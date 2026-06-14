import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { AddFeedbackTagRequest, AddFeedbackTagResponse } from '@/proto/attune/v1/tag'

export function useAddFeedbackTag(feedbackId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (req: Omit<AddFeedbackTagRequest, 'feedbackId'>) =>
      api<AddFeedbackTagResponse>(`/fb/v1/console/feedback/${feedbackId}/tags`, {
        method: 'POST',
        body: { feedbackId, ...req },
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'feedback', feedbackId] })
      void qc.invalidateQueries({ queryKey: ['console', 'tags'] })
    },
  })
}
