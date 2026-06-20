import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  GetEvalSuggestionsResponse,
  PromoteSuggestedValueRequest,
  PromoteSuggestedValueResponse,
} from '@/proto/attune/v1/enrich_config'

// usePromoteSuggestedValue adds an LLM-suggested off-list value to a
// dimension's taxonomy.
//
// On success it surgically drops the promoted candidate (and any matching
// recommendation) from the cached eval-suggestions data via setQueryData —
// NOT invalidateQueries. Invalidating would refetch, and the eval-suggestions
// query re-runs an LLM eval on every fetch, so invalidating per promote would
// spend LLM budget on each click and trip the endpoint's rate limit when
// promoting several values in a row. Coverage is left as-is until the operator
// re-runs "Analyze". The enrich-config query is invalidated (cheap, no LLM).
export function usePromoteSuggestedValue() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: PromoteSuggestedValueRequest) =>
      api<PromoteSuggestedValueResponse>('/fb/v1/console/enrich-config/promote', {
        method: 'POST',
        body,
      }),
    onSuccess: (_data, variables) => {
      void qc.invalidateQueries({ queryKey: ['console', 'enrich-config'] })
      qc.setQueryData<GetEvalSuggestionsResponse>(['console', 'eval-suggestions'], (prev) => {
        if (!prev) return prev
        const matches = (dim: string, value: string) =>
          dim === variables.dimensionName && value === variables.value
        return {
          ...prev,
          candidates: prev.candidates.filter((c) => !matches(c.dim, c.value)),
          recommendations: prev.recommendations.filter((r) => !matches(r.dim, r.value)),
        }
      })
    },
  })
}
