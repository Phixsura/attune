import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { RetryEnrichmentResponse } from '@/proto/attune/v1/ingest'

export interface BatchRetryResult {
  succeeded: number
  failed: Array<{ id: string; error: string }>
}

export function useBatchRetryEnrichment() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (ids: string[]): Promise<BatchRetryResult> => {
      const results = await Promise.allSettled(
        ids.map((id) =>
          api<RetryEnrichmentResponse>(`/fb/v1/console/feedback/${id}/retry-enrichment`, {
            method: 'POST',
          }).then(() => ({ id, success: true as const })),
        ),
      )

      const succeeded: string[] = []
      const failed: Array<{ id: string; error: string }> = []

      for (let i = 0; i < results.length; i++) {
        const result = results[i]
        const id = ids[i]
        if (result.status === 'fulfilled') {
          succeeded.push(id)
        } else {
          failed.push({
            id,
            /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
            error: result.reason instanceof Error ? result.reason.message : 'Unknown error',
          })
        }
      }

      return { succeeded: succeeded.length, failed }
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['console', 'feedback'] })
    },
  })
}
