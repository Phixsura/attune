import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

export interface BatchDeleteResult {
  succeeded: number
  failed: Array<{ id: string; error: string }>
}

export function useBatchDeleteFeedback() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (ids: string[]): Promise<BatchDeleteResult> => {
      const res = await api<{ affected_count: number }>('/fb/v1/console/feedback/batch', {
        method: 'POST',
        body: JSON.stringify({
          feedback_ids: ids.map((id) => Number(id)),
          operation: { delete: {} },
        }),
      })
      return { succeeded: res.affected_count, failed: [] }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['console', 'feedback'] })
    },
  })
}
