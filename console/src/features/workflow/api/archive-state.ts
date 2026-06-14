import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import { workflowStatesQueryKey } from './list-states'
import { workflowTransitionsQueryKey } from './list-transitions'

export function useArchiveState() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api(`/fb/v1/console/workflow/states/${id}/archive`, { method: 'POST' }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: workflowStatesQueryKey })
      void qc.invalidateQueries({ queryKey: workflowTransitionsQueryKey })
    },
  })
}
