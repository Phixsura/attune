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
  source?: string
  type?: string
  accountKey?: string
  urgent?: boolean
  tag?: string
  workflowState?: string
  // Filter by enrichment status: "pending" | "enriching" | "done" | "failed"
  enrichmentStatus?: string
  // If true, only return terminal failures (failed + attempts >= 5 + no next_retry)
  terminalFailedOnly?: boolean
  ids?: string[]
  confidenceLte?: number
  createdFrom?: string
  createdTo?: string
  enrichedFrom?: string
  enrichedTo?: string
  qualitySignal?: string
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
      if (filters.source) params.set('source', filters.source)
      if (filters.type) params.set('type', filters.type)
      if (filters.accountKey) params.set('account_key', filters.accountKey)
      if (filters.tag) params.set('tag', filters.tag)
      if (filters.workflowState) params.set('workflow_state', filters.workflowState)
      if (filters.urgent != null) params.set('urgent', String(filters.urgent))
      if (filters.enrichmentStatus) params.set('enrichment_status', filters.enrichmentStatus)
      if (filters.terminalFailedOnly) params.set('terminal_failed_only', 'true')
      if (filters.ids && filters.ids.length > 0) params.set('ids', filters.ids.join(','))
      if (filters.confidenceLte != null) params.set('confidence_lte', String(filters.confidenceLte))
      if (filters.createdFrom) params.set('created_from', filters.createdFrom)
      if (filters.createdTo) params.set('created_to', filters.createdTo)
      if (filters.enrichedFrom) params.set('enriched_from', filters.enrichedFrom)
      if (filters.enrichedTo) params.set('enriched_to', filters.enrichedTo)
      if (filters.qualitySignal) params.set('quality_signal', filters.qualitySignal)
      if (pageParam) params.set('cursor', pageParam)
      const qs = params.toString()
      const url = `/fb/v1/console/feedback${qs ? `?${qs}` : ''}`
      return api<ListFeedbackResponse>(url, { signal })
    },
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.nextCursor ?? null,
    staleTime: 15_000,
  })
