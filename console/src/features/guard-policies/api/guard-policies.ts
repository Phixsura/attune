import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  CreateGuardPolicyRequest,
  DeleteGuardPolicyResponse,
  GuardPolicy,
  ListGuardPoliciesResponse,
  PatchGuardPolicyRequest,
  ResolveGuardPolicyRequest,
  ResolveGuardPolicyResponse,
  UpdateGuardPoliciesRequest,
  UpdateGuardPoliciesResponse,
} from '@/proto/attune/v1/guard_policy'

export type { GuardPolicy, ResolveGuardPolicyRequest, ResolveGuardPolicyResponse }

export const guardPoliciesQuery = () =>
  queryOptions({
    queryKey: ['console', 'guard-policies'],
    queryFn: async ({ signal }) => {
      const resp = await api<ListGuardPoliciesResponse>('/fb/v1/console/guard-policies', {
        signal,
      })
      return resp.items
    },
    staleTime: 30_000,
  })

export function useResolveGuardPolicy() {
  return useMutation({
    mutationFn: (body: ResolveGuardPolicyRequest) =>
      api<ResolveGuardPolicyResponse>('/fb/v1/console/guard-policies/effective', {
        method: 'POST',
        body,
      }),
  })
}

export function useCreateGuardPolicy() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (policy: GuardPolicy) =>
      api<GuardPolicy>('/fb/v1/console/guard-policies', {
        method: 'POST',
        body: { policy } satisfies CreateGuardPolicyRequest,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'guard-policies'] })
    },
  })
}

export function usePatchGuardPolicy() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, policy }: PatchGuardPolicyRequest) =>
      api<GuardPolicy>(`/fb/v1/console/guard-policies/${id}`, {
        method: 'PATCH',
        body: { policy } satisfies Omit<PatchGuardPolicyRequest, 'id'>,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'guard-policies'] })
    },
  })
}

export function useDeleteGuardPolicy() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<DeleteGuardPolicyResponse>(`/fb/v1/console/guard-policies/${id}`, {
        method: 'DELETE',
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'guard-policies'] })
    },
  })
}

export function useUpdateGuardPolicies() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (policies: GuardPolicy[]) =>
      api<UpdateGuardPoliciesResponse>('/fb/v1/console/guard-policies', {
        method: 'PUT',
        body: { policies } satisfies UpdateGuardPoliciesRequest,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'guard-policies'] })
    },
  })
}
