import { queryOptions, useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { ListMembersResponse, Member } from '@/proto/attune/v1/member'

export type { Member }

export const membersQuery = () =>
  queryOptions({
    queryKey: ['console', 'members'],
    queryFn: async ({ signal }) => {
      const resp = await api<ListMembersResponse>('/fb/v1/console/members', { signal })
      return resp.members ?? []
    },
    staleTime: 30_000,
  })

export function useMembers() {
  return useQuery(membersQuery())
}
