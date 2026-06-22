import { createFileRoute } from '@tanstack/react-router'
import { AuditLogPage } from '@/features/audit-log/components/audit-log-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/administration/audit-log')({
  beforeLoad: ({ context }) =>
    requireRouteAccess(context, { permission: 'settings:audit_log:view' }),
  component: AuditLogPage,
})
