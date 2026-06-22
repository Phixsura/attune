import { createFileRoute } from '@tanstack/react-router'
import { membersQuery } from '@/features/members/api/list-members'
import { MembersPage } from '@/features/members/components/members-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/administration/members')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { permission: 'settings:members:view' }),
  component: MembersPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(membersQuery()),
})
