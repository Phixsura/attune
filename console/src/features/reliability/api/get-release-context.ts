import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  ReleaseContextResponse as SystemReleaseContextResponse,
  SemanticDescriptor as SystemSemanticDescriptor,
} from '@/proto/attune/v1/system'

export type ReleaseContextResponse = SystemReleaseContextResponse
export type SemanticDescriptor = SystemSemanticDescriptor

export const releaseContextQuery = () =>
  queryOptions({
    queryKey: ['console', 'system', 'release'],
    queryFn: async ({ signal }) => {
      return api<ReleaseContextResponse>('/fb/v1/console/system/release', { signal })
    },
    staleTime: 5 * 60 * 1000,
    refetchOnWindowFocus: false,
  })
