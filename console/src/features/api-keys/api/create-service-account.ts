import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  CreateServiceAccountRequest,
  CreateServiceAccountResponse,
  ServiceAccount,
} from '@/proto/attune/v1/api_key'
import { serviceAccountsQueryKey } from './list-service-accounts'

export type CreateServiceAccountParams = CreateServiceAccountRequest

export function useCreateServiceAccount() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (body: CreateServiceAccountParams): Promise<ServiceAccount> => {
      const resp = await api<CreateServiceAccountResponse>('/fb/v1/console/service-accounts', {
        method: 'POST',
        body,
      })
      if (!resp.serviceAccount) {
        throw new Error('service account response missing serviceAccount')
      }
      return resp.serviceAccount
    },
    onSuccess: (serviceAccount) => {
      queryClient.setQueryData<ServiceAccount[]>(serviceAccountsQueryKey, (current) => {
        const next = [
          /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
          ...(current ?? []).filter((item) => item.id !== serviceAccount.id),
          serviceAccount,
        ]
        return next.sort((a, b) => a.name.localeCompare(b.name))
      })
      queryClient.invalidateQueries({ queryKey: serviceAccountsQueryKey })
    },
  })
}
