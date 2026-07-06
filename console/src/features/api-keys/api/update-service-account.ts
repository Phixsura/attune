import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { ServiceAccount, UpdateServiceAccountRequest } from '@/proto/attune/v1/api_key'
import { serviceAccountsQueryKey } from './list-service-accounts'

export type UpdateServiceAccountParams = UpdateServiceAccountRequest

export function useUpdateServiceAccount() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async ({ id, isActive }: UpdateServiceAccountParams): Promise<ServiceAccount> => {
      const resp = await api<ServiceAccount>(`/fb/v1/console/service-accounts/${id}`, {
        method: 'PATCH',
        body: { isActive },
      })
      return resp
    },
    onSuccess: (serviceAccount) => {
      queryClient.setQueryData<ServiceAccount[]>(serviceAccountsQueryKey, (current) => {
        const next = [
          ...(current ?? []).filter((item) => item.id !== serviceAccount.id),
          serviceAccount,
        ]
        return next.sort((a, b) => a.name.localeCompare(b.name))
      })
      queryClient.invalidateQueries({ queryKey: serviceAccountsQueryKey })
    },
  })
}
