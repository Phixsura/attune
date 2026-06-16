import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { meQuery } from '@/features/session/api/get-me'
import { hasPermission, type Permission, type Role } from '@/lib/permissions'

export type { Permission, Role } from '@/lib/permissions'

export interface Permissions {
  role: Role
  userId: string | undefined

  isAdmin: boolean
  isMember: boolean
  isViewer: boolean

  /** Check if user has a specific permission */
  can: (permission: Permission) => boolean

  /** Legacy methods - prefer using can() with specific permissions */
  canView: () => boolean
  canEdit: () => boolean
  canManage: () => boolean
  canDelete: (ownerId?: string) => boolean
}

export function usePermissions(): Permissions {
  const { data: me } = useQuery(meQuery())

  const role = (me?.user?.role as Role) ?? 'viewer'
  const userId = me?.user?.openId

  return useMemo<Permissions>(
    () => ({
      role,
      userId,

      isAdmin: role === 'admin',
      isMember: role === 'member',
      isViewer: role === 'viewer',

      can: (permission: Permission) => hasPermission(role, permission),

      // Legacy methods for backwards compatibility
      canView: () => true,
      canEdit: () => hasPermission(role, 'feedback:edit'),
      canManage: () => role === 'admin',
      canDelete: (ownerId?: string) => {
        if (role === 'admin') return true
        if (role === 'member' && ownerId && ownerId === userId) return true
        return false
      },
    }),
    [role, userId],
  )
}
