import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import { workflowStatesQueryKey } from './list-states'
import { workflowTransitionsQueryKey } from './list-transitions'

export function useSeedDefaults() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api('/fb/v1/console/workflow/seed', { method: 'POST' }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: workflowStatesQueryKey })
      void qc.invalidateQueries({ queryKey: workflowTransitionsQueryKey })
    },
  })
}
