import { createFileRoute, Outlet, redirect } from '@tanstack/react-router'
import { requireRouteAccess, resolveGroupDefault } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/analytics')({
  beforeLoad: async ({ context, location }) => {
    if (location.pathname === '/analytics') {
      const role = await requireRouteAccess(context, { permission: 'usage:view' })
      throw redirect({ to: resolveGroupDefault('analytics', role) })
    }
  },
  component: Outlet,
})
