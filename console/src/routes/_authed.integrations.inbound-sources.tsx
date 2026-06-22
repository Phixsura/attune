import { createFileRoute } from '@tanstack/react-router'
import { inboundSourcesQuery } from '@/features/inbound-sources/api/list-inbound-sources'
import { InboundSourcesPage } from '@/features/inbound-sources/components/inbound-sources-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/integrations/inbound-sources')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { permission: 'settings:inbound:view' }),
  component: InboundSourcesPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(inboundSourcesQuery()),
})
