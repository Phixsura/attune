import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { ServiceAccount } from '@/proto/attune/v1/api_key'
import { serviceAccountsQueryKey } from './list-service-accounts'

export function useDeleteServiceAccount() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (id: string): Promise<void> => {
      await api<void>(`/fb/v1/console/service-accounts/${id}`, { method: 'DELETE' })
    },
    onSuccess: (_data, id) => {
      queryClient.setQueryData<ServiceAccount[]>(serviceAccountsQueryKey, (current) =>
        (current ?? []).filter((item) => item.id !== id),
      )
      queryClient.invalidateQueries({ queryKey: serviceAccountsQueryKey })
    },
  })
}
