import { infiniteQueryOptions, useMutation, useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  CancelGdprRequestResponse,
  GdprOperationsResponse,
  ListGdprRequestsResponse,
  VerifyGdprStepUpResponse,
} from '@/proto/attune/v1/gdpr'

export function gdprOperationsQuery() {
  return {
    queryKey: ['console', 'gdpr', 'operations'] as const,
    queryFn: ({ signal }: { signal: AbortSignal }) =>
      api<GdprOperationsResponse>('/fb/v1/console/gdpr/operations', { signal }),
    staleTime: 10_000,
  }
}

export function gdprRequestsInfiniteQuery(requestType: '' | 'delete' | 'export') {
  return infiniteQueryOptions({
    queryKey: ['console', 'gdpr', 'requests', requestType] as const,
    queryFn: async ({ pageParam, signal }) => {
      const params = new URLSearchParams()
      params.set('limit', '25')
      if (pageParam) params.set('cursor', pageParam)
      if (requestType) params.set('request_type', requestType)
      return api<ListGdprRequestsResponse>(`/fb/v1/console/gdpr/requests?${params.toString()}`, {
        signal,
      })
    },
    initialPageParam: null as null | string,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? null,
    staleTime: 10_000,
  })
}

export function useVerifyGdprStepUp() {
  return useMutation({
    mutationFn: (password: string) =>
      api<VerifyGdprStepUpResponse>('/fb/v1/console/gdpr/step-up/verify', {
        method: 'POST',
        body: password ? { password } : {},
      }),
  })
}

export function useCancelGdprRequest() {
  return useMutation({
    mutationFn: (requestId: string) =>
      api<CancelGdprRequestResponse>(`/fb/v1/console/gdpr/requests/${requestId}/cancel`, {
        method: 'POST',
        body: {},
      }),
  })
}

export function useGdprOperations() {
  return useQuery(gdprOperationsQuery())
}
