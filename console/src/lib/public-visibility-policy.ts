import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { PublicVisibilityPolicy } from '@/proto/attune/v1/public_visibility'

export const publicVisibilityPolicyQueryKey = ['console', 'public-visibility', 'policy'] as const

export function publicVisibilityPolicyQuery() {
  return queryOptions({
    queryKey: publicVisibilityPolicyQueryKey,
    queryFn: ({ signal }) =>
      api<PublicVisibilityPolicy>('/fb/v1/console/public-visibility/policy', { signal }),
    staleTime: 20_000,
  })
}
