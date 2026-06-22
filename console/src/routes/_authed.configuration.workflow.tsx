import { createFileRoute } from '@tanstack/react-router'
import { workflowStatesQuery } from '@/features/workflow/api/list-states'
import { WorkflowSettingsPage } from '@/features/workflow/components/workflow-settings-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/configuration/workflow')({
  beforeLoad: ({ context }) =>
    requireRouteAccess(context, { permission: 'settings:workflow:view' }),
  component: WorkflowSettingsPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(workflowStatesQuery(true)),
})
