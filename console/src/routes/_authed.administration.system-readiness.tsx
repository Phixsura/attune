import { createFileRoute } from '@tanstack/react-router'
import { SystemReadinessPage } from '@/features/system-readiness/components/system-readiness-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/administration/system-readiness')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { adminOnly: true }),
  component: SystemReadinessPage,
})
