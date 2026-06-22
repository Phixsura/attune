import { createFileRoute } from '@tanstack/react-router'
import { notifyTargetsQuery } from '@/features/notify-targets/api/list-notify-targets'
import { NotifyTargetsPage } from '@/features/notify-targets/components/notify-targets-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/integrations/notify-targets')({
  beforeLoad: ({ context }) =>
    requireRouteAccess(context, { permission: 'settings:notify_targets:view' }),
  component: NotifyTargetsPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(notifyTargetsQuery()),
})
