import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  ActivateEnrichPromptVersionResponse,
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

export function useActivateEnrichPromptVersion() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (versionId: string) =>
      api<ActivateEnrichPromptVersionResponse>(
        `/fb/v1/console/enrich-config/versions/${encodeURIComponent(versionId)}:activate`,
        {
          method: 'POST',
          body: { versionId },
        },
      ),
    onSuccess: (resp) => {
      qc.setQueryData(['console', 'enrich-config'], resp.config)
    },
  })
}

export type { EnrichConfig }
