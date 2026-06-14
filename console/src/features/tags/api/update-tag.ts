import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { Tag, UpdateTagRequest } from '@/proto/attune/v1/tag'
import { tagsQueryKey } from './list-tags'

export function useUpdateTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (req: UpdateTagRequest) =>
      api<Tag>(`/fb/v1/console/tags/${req.id}`, { method: 'PATCH', body: req }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: tagsQueryKey })
    },
  })
}
