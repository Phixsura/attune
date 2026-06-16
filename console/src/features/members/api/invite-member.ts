import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { InviteMemberResponse } from '@/proto/attune/v1/member'

export function useInviteMember() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ email, role }: { email: string; role: string }) =>
      api<InviteMemberResponse>('/fb/v1/console/members', {
        method: 'POST',
        body: { email, role },
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['console', 'members'] })
    },
  })
}
