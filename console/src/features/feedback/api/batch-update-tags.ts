import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { BatchFeedbackRequest, BatchFeedbackResponse } from '@/proto/attune/v1/batch'
import type {
  BatchUpdateFeedbackTagsRequest,
  BatchUpdateFeedbackTagsResponse,
} from '@/proto/attune/v1/tag'

export function useBatchUpdateTags() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (
      req: BatchUpdateFeedbackTagsRequest,
    ): Promise<BatchUpdateFeedbackTagsResponse> => {
      const batchReq: BatchFeedbackRequest = {
        feedbackIds: req.feedbackIds,
        dryRun: false,
        operation: {
          tag: {
            addTagIds: req.addTagIds,
            removeTagIds: req.removeTagIds,
          },
        },
      }
      const resp = await api<BatchFeedbackResponse>('/fb/v1/console/feedback/batch', {
        method: 'POST',
        body: batchReq,
      })
      return { affected: resp.succeeded }
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'feedback'] })
      void qc.invalidateQueries({ queryKey: ['console', 'tags'] })
    },
  })
}
