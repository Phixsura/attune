import { useMutation } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  PreviewEnrichPromptRequest,
  PreviewEnrichPromptResponse,
} from '@/proto/attune/v1/enrich_config'

export function usePreviewEnrichPrompt() {
  return useMutation({
    mutationFn: (body: PreviewEnrichPromptRequest) =>
      api<PreviewEnrichPromptResponse>('/fb/v1/console/enrich-config/preview', {
        method: 'POST',
        body,
      }),
  })
}
