import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { ListTagsResponse, Tag } from '@/proto/attune/v1/tag'

export type { Tag }

export const tagsQueryKey = ['console', 'tags'] as const

export const tagsQuery = () =>
  queryOptions({
    queryKey: [...tagsQueryKey],
    queryFn: async ({ signal }) => {
      const resp = await api<ListTagsResponse>('/fb/v1/console/tags', { signal })
      return resp.tags ?? []
    },
    staleTime: 30_000,
  })
