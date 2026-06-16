import { createFileRoute, redirect } from '@tanstack/react-router'
import { MembersPage } from '@/features/members/components/members-page'
import { meQuery } from '@/features/session/api/get-me'

export const Route = createFileRoute('/_authed/members')({
  beforeLoad: async ({ context }) => {
    const me = await context.queryClient.ensureQueryData(meQuery())
    if (me.user?.role !== 'admin') {
      throw redirect({ to: '/feedback' })
    }
  },
  component: MembersPage,
})
