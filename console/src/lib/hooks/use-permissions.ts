import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { meQuery } from '@/features/session/api/get-me'

export type Role = 'admin' | 'member' | 'viewer'

const ROLE_HIERARCHY: Record<Role, number> = {
  viewer: 0,
  member: 1,
  admin: 2,
}

export interface Permissions {
  role: Role
  userId: string | undefined

  isAdmin: boolean
  isMember: boolean
  isViewer: boolean

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

      canView: () => true,
      canEdit: () => ROLE_HIERARCHY[role] >= ROLE_HIERARCHY.member,
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
