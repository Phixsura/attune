import { createFileRoute } from '@tanstack/react-router'
import { guardPoliciesQuery } from '@/features/guard-policies/api/guard-policies'
import { GuardPoliciesPage } from '@/features/guard-policies/components/guard-policies-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/administration/guard-policies')({
  beforeLoad: ({ context }) =>
    requireRouteAccess(context, { permission: 'settings:guard_policies:view' }),
  component: GuardPoliciesPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(guardPoliciesQuery()),
})
