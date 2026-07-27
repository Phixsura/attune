import { createFileRoute } from '@tanstack/react-router'
import {
  cohortSyncHealthQuery,
  listCohortSourcesQuery,
  listCohortsQuery,
} from '@/features/cohort-sync/api/cohort-sync'
import { CohortSyncPage } from '@/features/cohort-sync/components/cohort-sync-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/integrations/cohort-sync')({
  beforeLoad: ({ context }) =>
    requireRouteAccess(context, { permission: 'settings:cohort_sync:view' }),
  component: CohortSyncPage,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(cohortSyncHealthQuery()),
      context.queryClient.ensureQueryData(listCohortSourcesQuery()),
      context.queryClient.ensureQueryData(listCohortsQuery()),
    ])
  },
})
