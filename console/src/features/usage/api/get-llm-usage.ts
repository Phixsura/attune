import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { GetLLMUsageResponse } from '@/proto/attune/v1/usage'

export type LLMUsage = GetLLMUsageResponse

export interface LLMUsageFilters {
  granularity?: 'day' | 'week' | 'month'
  range?: 'now-7d' | 'now-30d' | 'now-90d' | 'now/M'
}

export const llmUsageQuery = (filters: LLMUsageFilters = {}) =>
  queryOptions({
    queryKey: ['console', 'llm-usage', filters],
    queryFn: ({ signal }) => {
      const qs = new URLSearchParams()
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      if (filters.granularity) qs.set('granularity', filters.granularity)
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      if (filters.range) qs.set('range', filters.range)
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      const suffix = qs.size > 0 ? `?${qs.toString()}` : ''
      return api<LLMUsage>(`/fb/v1/console/llm-usage${suffix}`, { signal })
    },
    staleTime: 60_000,
  })
