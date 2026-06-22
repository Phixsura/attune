import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { GetEvalSuggestionsResponse } from '@/proto/attune/v1/enrich_config'

export type {
  GetEvalSuggestionsResponse,
  SuggestedCandidate,
  SuggestedRecommendation,
} from '@/proto/attune/v1/enrich_config'

// useAnalyzeEvalSuggestions runs an LLM-backed consistency eval. This is a
// mutating/costly operation even though it returns analysis data, so it is a
// POST mutation gated behind the explicit Analyze button rather than a query
// that could be refetched by focus, retry, or cache invalidation.
export function useAnalyzeEvalSuggestions() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      api<GetEvalSuggestionsResponse>('/fb/v1/console/enrich-config/eval-suggestions:analyze', {
        method: 'POST',
        body: {},
      }),
    onSuccess: (resp) => {
      qc.setQueryData(['console', 'eval-suggestions'], resp)
    },
  })
}
