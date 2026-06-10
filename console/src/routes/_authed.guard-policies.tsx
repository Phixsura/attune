import { createFileRoute } from '@tanstack/react-router'
import { guardPoliciesQuery } from '@/features/guard-policies/api/guard-policies'
import { GuardPoliciesPage } from '@/features/guard-policies/components/guard-policies-page'

export const Route = createFileRoute('/_authed/guard-policies')({
  component: GuardPoliciesPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(guardPoliciesQuery()),
})
