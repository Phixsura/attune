import { createFileRoute } from '@tanstack/react-router'
import { deliveriesQuery } from '@/features/outbox-dead/api/list-deliveries'
import { DeadDeliveriesPage } from '@/features/outbox-dead/components/dead-deliveries-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/administration/dead-deliveries')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { adminOnly: true }),
  component: DeadDeliveriesPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(deliveriesQuery('dead')),
})
