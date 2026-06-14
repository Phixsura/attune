import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { CreateStateRequest, CreateStateResponse } from '@/proto/attune/v1/workflow'
import { workflowStatesQueryKey } from './list-states'

export function useCreateState() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (req: CreateStateRequest) =>
      api<CreateStateResponse>('/fb/v1/console/workflow/states', {
        method: 'POST',
        body: req,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: workflowStatesQueryKey })
    },
  })
}
