import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

export function useUpdateMemberRole() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, role }: { id: string; role: string }) =>
      api<void>(`/fb/v1/console/members/${id}`, { method: 'PATCH', body: { role } }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'members'] })
    },
  })
}
