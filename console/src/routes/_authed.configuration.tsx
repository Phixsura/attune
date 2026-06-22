import { createFileRoute, Outlet, redirect } from '@tanstack/react-router'
import { requireRouteAccess, resolveGroupDefault } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/configuration')({
  beforeLoad: async ({ context, location }) => {
    if (location.pathname === '/configuration') {
      const role = await requireRouteAccess(context, { permission: 'nav:settings' })
      throw redirect({ to: resolveGroupDefault('configuration', role) })
    }
  },
  component: Outlet,
})
