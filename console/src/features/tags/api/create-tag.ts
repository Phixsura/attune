import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { CreateTagRequest, Tag } from '@/proto/attune/v1/tag'
import { tagsQueryKey } from './list-tags'

export function useCreateTag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (req: CreateTagRequest) =>
      api<Tag>('/fb/v1/console/tags', { method: 'POST', body: req }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: tagsQueryKey })
    },
  })
}
