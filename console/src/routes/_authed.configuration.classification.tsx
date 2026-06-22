import { createFileRoute } from '@tanstack/react-router'
import { ClassificationSettingsPage } from '@/features/settings/components/classification-settings-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/configuration/classification')({
  beforeLoad: ({ context }) =>
    requireRouteAccess(context, { permission: 'settings:enrich_config:view' }),
  component: ClassificationSettingsPage,
})
