import type { ReactNode } from 'react'
import { usePermissions } from '@/lib/hooks/use-permissions'

type Action = 'view' | 'edit' | 'manage' | 'delete'

interface CanProps {
  action: Action
  ownerId?: string
  children: ReactNode
  fallback?: ReactNode
}

export function Can({ action, ownerId, children, fallback = null }: CanProps) {
  const perms = usePermissions()

  const allowed = (() => {
    switch (action) {
      case 'view':
        return perms.canView()
      case 'edit':
        return perms.canEdit()
      case 'manage':
        return perms.canManage()
      case 'delete':
        return perms.canDelete(ownerId)
    }
  })()

  return <>{allowed ? children : fallback}</>
}
