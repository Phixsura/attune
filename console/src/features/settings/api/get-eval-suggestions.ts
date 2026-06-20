import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { GetEvalSuggestionsResponse } from '@/proto/attune/v1/enrich_config'

export type {
  GetEvalSuggestionsResponse,
  SuggestedCandidate,
  SuggestedRecommendation,
} from '@/proto/attune/v1/enrich_config'

// evalSuggestionsQuery fetches off-list values the LLM suggested during a
// quick consistency eval. The eval re-runs LLM classification on a sample of
// rows, so it is expensive and slow — callers gate it behind an explicit
// "Analyze" action (enabled: false until the operator opts in) rather than
// fetching on mount. staleTime keeps a fetched result around while the
// operator reviews + promotes without re-spending the LLM budget.
export const evalSuggestionsQuery = (enabled: boolean) =>
  queryOptions({
    queryKey: ['console', 'eval-suggestions'],
    queryFn: ({ signal }) =>
      api<GetEvalSuggestionsResponse>('/fb/v1/console/enrich-config/eval-suggestions', { signal }),
    enabled,
    staleTime: 5 * 60_000,
    gcTime: 5 * 60_000,
    retry: false,
  })
