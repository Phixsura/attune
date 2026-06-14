import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  BatchUpdateFeedbackTagsRequest,
  BatchUpdateFeedbackTagsResponse,
} from '@/proto/attune/v1/tag'

export function useBatchUpdateTags() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (req: BatchUpdateFeedbackTagsRequest) =>
      api<BatchUpdateFeedbackTagsResponse>('/fb/v1/console/feedback/tags/batch', {
        method: 'POST',
        body: req,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'feedback'] })
      void qc.invalidateQueries({ queryKey: ['console', 'tags'] })
    },
  })
}
