import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { CreateApiKeyResponse } from '@/proto/attune/v1/api_key'

// Feature-stable alias for the create-key response. The secret is returned
// ONCE here at creation; subsequent GET /api-keys never exposes it again.
export type NewApiKey = CreateApiKeyResponse

export interface CreateApiKeyParams {
  label: string
  scopes?: string[]
}

export function useCreateApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ label, scopes }: CreateApiKeyParams) =>
      api<NewApiKey>('/fb/v1/console/api-keys', { method: 'POST', body: { label, scopes } }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'api-keys'] })
    },
  })
}
