import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  CreateInboundSourceRequest,
  CreateInboundSourceResponse,
} from '@/proto/attune/v1/inbound_source'

// Feature-stable alias for the create payload.
export type InboundSourceCreate = CreateInboundSourceRequest

// useCreateInboundSource posts a webhook, email, or slack source. On success
// the response carries a fresh InboundSource row and, for webhook channels,
// a webhookSecretReveal with the one-shot secret. Callers must surface the
// reveal immediately (the secret is unrecoverable).
export function useCreateInboundSource() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: InboundSourceCreate) =>
      api<CreateInboundSourceResponse>('/fb/v1/console/inbound/sources', {
        method: 'POST',
        body,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'inbound-sources'] })
    },
  })
}
