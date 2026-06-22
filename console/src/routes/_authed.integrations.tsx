import { createFileRoute, Outlet, redirect } from '@tanstack/react-router'
import { requireRouteAccess, resolveGroupDefault } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/integrations')({
  beforeLoad: async ({ context, location }) => {
    if (location.pathname === '/integrations') {
      const role = await requireRouteAccess(context, { permission: 'nav:settings' })
      throw redirect({ to: resolveGroupDefault('integrations', role) })
    }
  },
  component: Outlet,
})
