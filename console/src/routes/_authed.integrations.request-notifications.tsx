import { createFileRoute } from '@tanstack/react-router'
import {
  requestNotificationDeliveriesQuery,
  requestNotificationSenderQuery,
  requestNotificationSettingsQuery,
  requestNotificationWebhookTargetsQuery,
} from '@/features/request-notifications/api/request-notifications'
import { RequestNotificationsPage } from '@/features/request-notifications/components/request-notifications-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/integrations/request-notifications')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { adminOnly: true }),
  component: RequestNotificationsPage,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(requestNotificationSettingsQuery()),
      context.queryClient.ensureQueryData(requestNotificationSenderQuery()),
      context.queryClient.ensureQueryData(requestNotificationWebhookTargetsQuery()),
      context.queryClient.ensureQueryData(requestNotificationDeliveriesQuery(25)),
    ])
  },
})
