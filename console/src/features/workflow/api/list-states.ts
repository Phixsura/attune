import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { ListStatesResponse, WorkflowState } from '@/proto/attune/v1/workflow'

export type { WorkflowState }

export const workflowStatesQueryKey = ['console', 'workflow-states'] as const

export const workflowStatesQuery = (includeArchived = false) =>
  queryOptions({
    queryKey: [...workflowStatesQueryKey, { includeArchived }],
    queryFn: async ({ signal }) => {
      const qs = includeArchived ? '?include_archived=true' : ''
      const resp = await api<ListStatesResponse>(`/fb/v1/console/workflow/states${qs}`, { signal })
      return resp.states ?? []
    },
    staleTime: 30_000,
  })
