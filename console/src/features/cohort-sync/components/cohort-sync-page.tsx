import { useQuery } from '@tanstack/react-query'
import { cohortSyncHealthQuery, listCohortSourcesQuery, listCohortsQuery } from '../api/cohort-sync'
import { CohortSyncUI } from './cohort-sync-ui'

export function CohortSyncPage() {
  const sourcesQuery = useQuery(listCohortSourcesQuery())
  const cohortsQuery = useQuery(listCohortsQuery())
  const healthQuery = useQuery(cohortSyncHealthQuery())

  return (
    <CohortSyncUI
      sources={sourcesQuery.data ?? []}
      cohorts={cohortsQuery.data ?? []}
      health={healthQuery.data}
      isLoading={sourcesQuery.isLoading || cohortsQuery.isLoading || healthQuery.isLoading}
    />
  )
}
