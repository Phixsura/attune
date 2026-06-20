import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  PromoteSuggestedValueRequest,
  PromoteSuggestedValueResponse,
} from '@/proto/attune/v1/enrich_config'

// usePromoteSuggestedValue adds an LLM-suggested off-list value to a
// dimension's taxonomy. On success it invalidates both the enrich-config
// query (the taxonomy changed) and the eval-suggestions query (the promoted
// value is no longer off-list, so coverage rises and it drops off the list).
export function usePromoteSuggestedValue() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: PromoteSuggestedValueRequest) =>
      api<PromoteSuggestedValueResponse>('/fb/v1/console/enrich-config/promote', {
        method: 'POST',
        body,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'enrich-config'] })
      void qc.invalidateQueries({ queryKey: ['console', 'eval-suggestions'] })
    },
  })
}
