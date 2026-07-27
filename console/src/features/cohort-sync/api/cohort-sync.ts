import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  Cohort,
  CohortSource,
  CohortSyncHealth,
  CohortSyncRun,
  CreateCohortSourceRequest,
  ListCohortSourcesResponse,
  ListCohortSyncRunsResponse,
  ListCohortsResponse,
  SyncCohortResponse,
  TestCohortSourceResponse,
  UpdateCohortRequest,
  UpdateCohortSourceRequest,
} from '@/proto/attune/v1/cohort_sync'

const base = '/fb/v1/console/cohort-sync'

// ---------- Sources ----------

export function listCohortSourcesQuery() {
  return queryOptions({
    queryKey: ['cohort-sync', 'sources'],
    queryFn: ({ signal }) =>
      api<ListCohortSourcesResponse>(`${base}/sources`, { signal }).then((r) => r.sources ?? []),
  })
}

export async function createCohortSource(body: CreateCohortSourceRequest): Promise<CohortSource> {
  return api<CohortSource>(`${base}/sources`, { method: 'POST', body })
}

export async function updateCohortSource(
  id: string,
  body: Omit<UpdateCohortSourceRequest, 'id'>,
): Promise<CohortSource> {
  return api<CohortSource>(`${base}/sources/${id}`, { method: 'PATCH', body })
}

export async function deleteCohortSource(id: string): Promise<void> {
  await api(`${base}/sources/${id}`, { method: 'DELETE' })
}

export async function testCohortSource(id: string): Promise<TestCohortSourceResponse> {
  return api<TestCohortSourceResponse>(`${base}/sources/${id}:test`, {
    method: 'POST',
    body: {},
  })
}

// ---------- Cohorts ----------

export function listCohortsQuery(sourceId?: string) {
  return queryOptions({
    queryKey: ['cohort-sync', 'cohorts', sourceId],
    queryFn: ({ signal }) => {
      const qs = sourceId ? `?source_id=${sourceId}` : ''
      return api<ListCohortsResponse>(`${base}/cohorts${qs}`, { signal }).then(
        (r) => r.cohorts ?? [],
      )
    },
  })
}

export async function updateCohort(
  id: string,
  body: Omit<UpdateCohortRequest, 'id'>,
): Promise<Cohort> {
  return api<Cohort>(`${base}/cohorts/${id}`, { method: 'PATCH', body })
}

export async function syncCohort(id: string): Promise<SyncCohortResponse> {
  return api<SyncCohortResponse>(`${base}/cohorts/${id}:sync`, {
    method: 'POST',
    body: {},
  })
}

// ---------- Sync Runs ----------

export function listCohortSyncRunsQuery(cohortId: string, limit = 20) {
  return queryOptions({
    queryKey: ['cohort-sync', 'runs', cohortId],
    queryFn: ({ signal }) =>
      api<ListCohortSyncRunsResponse>(`${base}/cohorts/${cohortId}/runs?limit=${limit}`, {
        signal,
      }).then((r) => r.runs ?? []),
  })
}

// ---------- Health ----------

export function cohortSyncHealthQuery() {
  return queryOptions({
    queryKey: ['cohort-sync', 'health'],
    queryFn: ({ signal }) => api<CohortSyncHealth>(`${base}/health`, { signal }),
  })
}

export type { Cohort, CohortSource, CohortSyncHealth, CohortSyncRun }
