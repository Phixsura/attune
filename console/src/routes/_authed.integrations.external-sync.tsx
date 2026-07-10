import { createFileRoute } from '@tanstack/react-router'
import {
  externalSyncConnectionsQuery,
  externalSyncHealthQuery,
} from '@/features/external-sync/api/external-sync'
import { ExternalSyncPage } from '@/features/external-sync/components/external-sync-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/integrations/external-sync')({
  beforeLoad: ({ context }) =>
    requireRouteAccess(context, { permission: 'settings:external_sync:view' }),
  component: ExternalSyncPage,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(externalSyncHealthQuery()),
      context.queryClient.ensureQueryData(externalSyncConnectionsQuery()),
    ])
  },
})
