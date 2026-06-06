import { infiniteQueryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { Feedback, ListFeedbackResponse } from '@/proto/attune/v1/ingest'

// Re-export the proto item type under a feature-stable alias so consumers
// (table rows, filter chips) don't follow ts-proto rename churn.
export type { Feedback }

export interface FeedbackListFilters {
  kind?: string
  severity?: string
  q?: string
}

// Infinite query for cursor pagination. Uses TanStack v5's typed
// `infiniteQueryOptions` for type inference + consistency with sibling
// queries that wrap with `queryOptions`. Each page is one server call;
// TanStack Query handles page concatenation + de-dup via getPageParam.
export const feedbackListInfiniteQuery = (filters: FeedbackListFilters) =>
  infiniteQueryOptions({
    queryKey: ['console', 'feedback', filters] as const,
    queryFn: async ({ pageParam, signal }) => {
      const params = new URLSearchParams()
      if (filters.kind) params.set('kind', filters.kind)
      if (filters.severity) params.set('severity', filters.severity)
      if (filters.q) params.set('q', filters.q)
      if (pageParam) params.set('cursor', pageParam)
      const qs = params.toString()
      const url = `/fb/v1/console/feedback${qs ? `?${qs}` : ''}`
      return api<ListFeedbackResponse>(url, { signal })
    },
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.nextCursor ?? null,
    staleTime: 15_000,
  })
