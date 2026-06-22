import { createFileRoute } from '@tanstack/react-router'
import { digestSubscriptionQuery } from '@/features/digest-subscription/api/get-digest-subscription'
import { DigestSubscriptionPage } from '@/features/digest-subscription/components/digest-subscription-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/integrations/digests')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { permission: 'settings:digest:view' }),
  component: DigestSubscriptionPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(digestSubscriptionQuery()),
})
