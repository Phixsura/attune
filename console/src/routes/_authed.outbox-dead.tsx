import { createFileRoute } from '@tanstack/react-router'
import { deliveriesQuery } from '@/features/outbox-dead/api/list-deliveries'
import { DeadDeliveriesPage } from '@/features/outbox-dead/components/dead-deliveries-page'

export const Route = createFileRoute('/_authed/outbox-dead')({
  component: DeadDeliveriesPage,
  // Prefetch the dead queue — the page's default filter — so the first
  // paint has rows. The failed list loads on demand when the operator
  // flips the toggle.
  loader: ({ context }) => context.queryClient.ensureQueryData(deliveriesQuery('dead')),
})
