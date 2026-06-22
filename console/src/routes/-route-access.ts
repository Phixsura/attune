import type { QueryClient } from '@tanstack/react-query'
import { redirect } from '@tanstack/react-router'
import { meQuery } from '@/features/session/api/get-me'
import {
  type ConsoleNavGroupId,
  type ConsoleRouteAccess,
  canAccessConsoleItem,
  getConsoleGroupDefaultPath,
  resolveLegacySettingsPath,
} from '@/lib/console-ia'
import type { Role } from '@/lib/permissions'

export type RouteAccess = ConsoleRouteAccess

interface QueryClientContext {
  queryClient: QueryClient
}

export function canAccessRoute(role: Role, access?: RouteAccess): boolean {
  return canAccessConsoleItem(role, access)
}

export async function requireRouteAccess(
  context: QueryClientContext,
  access?: RouteAccess,
): Promise<Role> {
  const me = await context.queryClient.ensureQueryData(meQuery())
  const role = (me.user?.role as Role | undefined) ?? 'viewer'
  if (!canAccessRoute(role, access)) {
    throw redirect({ to: '/feedback' })
  }
  return role
}

export function resolveLegacySettingsRedirect(search: Record<string, unknown>, role: Role): string {
  return resolveLegacySettingsPath(
    typeof search.section === 'string' ? search.section : undefined,
    role,
  )
}

export function resolveGroupDefault(group: ConsoleNavGroupId, role: Role): string {
  return getConsoleGroupDefaultPath(group, role)
}
