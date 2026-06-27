import { createFileRoute } from '@tanstack/react-router'
import { SecurityPage } from '@/features/security/components/security-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/administration/security')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { adminOnly: true }),
  component: SecurityPage,
})
