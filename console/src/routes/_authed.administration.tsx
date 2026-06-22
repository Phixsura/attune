import { createFileRoute, Outlet, redirect } from '@tanstack/react-router'
import { requireRouteAccess, resolveGroupDefault } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/administration')({
  beforeLoad: async ({ context, location }) => {
    if (location.pathname === '/administration') {
      const role = await requireRouteAccess(context, { permission: 'nav:settings' })
      throw redirect({ to: resolveGroupDefault('administration', role) })
    }
  },
  component: Outlet,
})
