import { createFileRoute, redirect } from '@tanstack/react-router'
import { requireRouteAccess, resolveLegacySettingsRedirect } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/settings')({
  validateSearch: (search: Record<string, unknown>) => search,
  beforeLoad: async ({ context, search }) => {
    const role = await requireRouteAccess(context, { permission: 'nav:settings' })
    throw redirect({ to: resolveLegacySettingsRedirect(search, role) })
  },
})
