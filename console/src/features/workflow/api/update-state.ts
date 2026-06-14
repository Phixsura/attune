import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { UpdateStateRequest, UpdateStateResponse } from '@/proto/attune/v1/workflow'
import { workflowStatesQueryKey } from './list-states'

export function useUpdateState() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (req: UpdateStateRequest) =>
      api<UpdateStateResponse>(`/fb/v1/console/workflow/states/${req.id}`, {
        method: 'PUT',
        body: req,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: workflowStatesQueryKey })
    },
  })
}
