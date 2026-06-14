import { infiniteQueryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { Feedback, ListFeedbackResponse } from '@/proto/attune/v1/ingest'

// Re-export the proto item type under a feature-stable alias so consumers
// (table rows, filter chips) don't follow ts-proto rename churn.
export type { Feedback }

// One per-dim filter the SPA collects from the URL/UI. The backend
// translates these into JSONB containment clauses; the SPA just sends
// stable (dim.name, taxonomy.value) pairs.
export interface AttrFilterEntry {
  dim: string
  value: string
}

export interface FeedbackListFilters {
  // map<dim.name, taxonomy.value> — at most one filter per dim from the
  // dropdown UI. Multiple values per dim are supported by the backend
  // (AND-composed) but the SPA's per-dim Select only ever sends one.
  attrs: AttrFilterEntry[]
  q?: string
  urgent?: boolean
  tag?: string
  workflowState?: string
}

// Infinite query for cursor pagination. Each `attrs` entry becomes a
// `?<dim>=<value>` query param; the backend resolves dim → kind via the
// tenant config and produces the right JSONB containment shape.
export const feedbackListInfiniteQuery = (filters: FeedbackListFilters) =>
  infiniteQueryOptions({
    queryKey: ['console', 'feedback', filters] as const,
    queryFn: async ({ pageParam, signal }) => {
      const params = new URLSearchParams()
      for (const { dim, value } of filters.attrs) {
        if (dim && value) params.append(dim, value)
      }
      if (filters.q) params.set('q', filters.q)
      if (filters.tag) params.set('tag', filters.tag)
      if (filters.workflowState) params.set('workflow_state', filters.workflowState)
      if (filters.urgent != null) params.set('urgent', String(filters.urgent))
      if (pageParam) params.set('cursor', pageParam)
      const qs = params.toString()
      const url = `/fb/v1/console/feedback${qs ? `?${qs}` : ''}`
      return api<ListFeedbackResponse>(url, { signal })
    },
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.nextCursor ?? null,
    staleTime: 15_000,
  })
