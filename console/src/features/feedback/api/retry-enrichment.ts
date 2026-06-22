import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { RetryEnrichmentResponse } from '@/proto/attune/v1/ingest'

export function useRetryEnrichment(id: string) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () =>
      api<RetryEnrichmentResponse>(`/fb/v1/console/feedback/${id}/retry-enrichment`, {
        method: 'POST',
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['console', 'feedback', 'detail', id] })
      queryClient.invalidateQueries({ queryKey: ['console', 'feedback', 'list'] })
      queryClient.invalidateQueries({ queryKey: ['console', 'feedback', 'stats'] })
    },
  })
}
