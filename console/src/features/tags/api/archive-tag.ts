import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import { tagsQueryKey } from './list-tags'

export function useArchiveTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => api(`/fb/v1/console/tags/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: tagsQueryKey })
    },
  })
}
