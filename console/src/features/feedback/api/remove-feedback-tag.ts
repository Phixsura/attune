import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

export function useRemoveFeedbackTag(feedbackId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (tagId: string) =>
      api(`/fb/v1/console/feedback/${feedbackId}/tags/${tagId}`, { method: 'DELETE' }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'feedback'] })
      void qc.invalidateQueries({ queryKey: ['console', 'tags'] })
    },
  })
}
