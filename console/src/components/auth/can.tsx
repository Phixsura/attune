import type { ReactNode } from 'react'
import { type Permission, usePermissions } from '@/lib/hooks/use-permissions'

interface CanProps {
  /** Permission to check */
  permission: Permission
  /** Content to render if permission is granted */
  children: ReactNode
  /** Content to render if permission is denied (default: null) */
  fallback?: ReactNode
}

/**
 * Permission-aware component wrapper.
 *
 * Usage:
 *   <Can permission="settings:members:invite">
 *     <Button>Invite Member</Button>
 *   </Can>
 *
 *   <Can permission="feedback:delete" fallback={<DisabledButton />}>
 *     <DeleteButton />
 *   </Can>
 */
export function Can({ permission, children, fallback = null }: CanProps) {
  const { can } = usePermissions()
  return <>{can(permission) ? children : fallback}</>
}

/**
 * Hook for checking permissions in component logic.
 *
 * Usage:
 *   const canInvite = useCan('settings:members:invite')
 *   if (canInvite) { ... }
 */
export function useCan(permission: Permission): boolean {
  const { can } = usePermissions()
  return can(permission)
}
