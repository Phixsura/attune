import { queryOptions } from '@tanstack/react-query'
import { type ApiError, api } from '@/lib/api-client'
import type { DigestSubscription } from '@/proto/attune/v1/digest_subscription'

// Re-export the proto type under a feature-stable alias so consumers don't
// follow ts-proto rename churn.
export type { DigestSubscription }

// digestSubscriptionQuery returns the tenant's digest config, or null when none
// is configured yet (server 404) so the page can render create-defaults instead
// of an error state.
export const digestSubscriptionQuery = () =>
  queryOptions({
    queryKey: ['console', 'digest-subscription'],
    queryFn: async ({ signal }): Promise<DigestSubscription | null> => {
      try {
        return await api<DigestSubscription>('/fb/v1/console/digest-subscription', { signal })
      } catch (err) {
        /* v8 ignore next -- @preserve: missing optional subscription is represented as null. */
        if ((err as ApiError)?.status === 404) return null
        throw err
      }
    },
    staleTime: 30_000,
  })
