import { createFileRoute } from '@tanstack/react-router'
import { ControlTowerPage, controlTowerQueries } from '@/routes/-control-tower-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/control-tower')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { permission: 'usage:view' }),
  component: ControlTowerPage,
  loader: ({ context }) =>
    Promise.all([
      context.queryClient.ensureQueryData(controlTowerQueries[0]),
      context.queryClient.ensureQueryData(controlTowerQueries[1]),
      context.queryClient.ensureQueryData(controlTowerQueries[2]),
      context.queryClient.ensureQueryData(controlTowerQueries[3]),
    ]),
})
