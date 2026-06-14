import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { ListTransitionsResponse, WorkflowTransition } from '@/proto/attune/v1/workflow'

export type { WorkflowTransition }

export const workflowTransitionsQueryKey = ['console', 'workflow-transitions'] as const

export const workflowTransitionsQuery = () =>
  queryOptions({
    queryKey: [...workflowTransitionsQueryKey],
    queryFn: async ({ signal }) => {
      const resp = await api<ListTransitionsResponse>('/fb/v1/console/workflow/transitions', {
        signal,
      })
      return resp.transitions ?? []
    },
    staleTime: 30_000,
  })
