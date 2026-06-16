import { useTranslation } from 'react-i18next'
import type { Role } from '@/lib/hooks/use-permissions'
import { cn } from '@/lib/utils'

interface RoleBadgeProps {
  role?: string
  className?: string
}

const ROLE_STYLES: Record<Role, string> = {
  admin:
    'bg-red-500/10 text-red-600 border-red-200 dark:bg-red-500/20 dark:text-red-400 dark:border-red-800',
  member:
    'bg-blue-500/10 text-blue-600 border-blue-200 dark:bg-blue-500/20 dark:text-blue-400 dark:border-blue-800',
  viewer:
    'bg-gray-500/10 text-gray-600 border-gray-200 dark:bg-gray-500/20 dark:text-gray-400 dark:border-gray-700',
}

export function RoleBadge({ role, className }: RoleBadgeProps) {
  const { t } = useTranslation()
  const safeRole = (role ?? 'viewer') as Role
  const styles = ROLE_STYLES[safeRole] ?? ROLE_STYLES.viewer

  return (
    <span className={cn('px-2 py-0.5 rounded border text-xs font-medium', styles, className)}>
      {t(`role.${safeRole}`)}
    </span>
  )
}
