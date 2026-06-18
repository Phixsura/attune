import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { RetryDeliveryResponse } from '@/proto/attune/v1/outbox'

// Feature-stable alias for the manual re-arm result.
export type RetryResult = RetryDeliveryResponse

// useRetryDelivery manually re-arms one dead/failed delivery in place.
// The backend answers 202 (the worker redelivers on its next poll), so
// on success we invalidate every outbox-deliveries cache entry — both
// the dead and failed lists — to pull the row's refreshed state.
//
// Error envelopes surface as ApiError: 409 (in-flight / not retryable),
// 404 (gone), 400 (bad id). The caller reads err.status + err.message
// to toast the server's message verbatim.
export function useRetryDelivery() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<RetryResult>(`/fb/v1/console/outbox/${id}/retry`, { method: 'POST' }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['console', 'outbox-deliveries'] })
    },
  })
}
