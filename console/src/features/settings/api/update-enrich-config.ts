import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  EnrichConfig,
  UpdateEnrichConfigRequest,
  UpdateEnrichConfigResponse,
} from '@/proto/attune/v1/enrich_config'

export function useUpdateEnrichConfig() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: UpdateEnrichConfigRequest) =>
      api<UpdateEnrichConfigResponse>('/fb/v1/console/enrich-config', {
        method: 'PUT',
        body,
      }),
    onSuccess: (resp) => {
      qc.setQueryData(['console', 'enrich-config'], resp.config)
    },
  })
}

export type { EnrichConfig }
