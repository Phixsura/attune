import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { CreateApiKeyResponse } from '@/proto/attune/v1/api_key'

// Feature-stable alias for the create-key response. The secret is returned
// ONCE here at creation; subsequent GET /api-keys never exposes it again.
export type NewApiKey = CreateApiKeyResponse

export function useCreateApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (label: string) =>
      api<NewApiKey>('/fb/v1/console/api-keys', { method: 'POST', body: { label } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'api-keys'] })
    },
  })
}
