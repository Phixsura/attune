import { createFileRoute } from '@tanstack/react-router'
import { GDPRPage } from '@/features/gdpr/components/gdpr-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/administration/gdpr')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { permission: 'settings:gdpr:view' }),
  component: GDPRPage,
})
