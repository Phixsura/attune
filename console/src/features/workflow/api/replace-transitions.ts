import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  ReplaceTransitionsRequest,
  ReplaceTransitionsResponse,
} from '@/proto/attune/v1/workflow'
import { workflowTransitionsQueryKey } from './list-transitions'

export function useReplaceTransitions() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (req: ReplaceTransitionsRequest) =>
      api<ReplaceTransitionsResponse>('/fb/v1/console/workflow/transitions', {
        method: 'PUT',
        body: req,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: workflowTransitionsQueryKey })
    },
  })
}
