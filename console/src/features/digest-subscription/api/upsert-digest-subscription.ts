import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  DigestSubscription,
  UpsertDigestSubscriptionRequest,
} from '@/proto/attune/v1/digest_subscription'

// Feature-stable alias for the upsert payload.
export type DigestSubscriptionUpsert = UpsertDigestSubscriptionRequest

export function useUpsertDigestSubscription() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: DigestSubscriptionUpsert) =>
      api<DigestSubscription>('/fb/v1/console/digest-subscription', { method: 'PUT', body }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'digest-subscription'] })
    },
  })
}
