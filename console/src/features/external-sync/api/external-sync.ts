import { infiniteQueryOptions, queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import {
  type BatchResolveExternalSyncConflictsRequest,
  type BatchResolveExternalSyncConflictsResponse,
  type CreateExternalConnectionRequest,
  type DiscoverExternalConnectionSchemaResponse,
  type ExternalConnection,
  type ExternalObjectMapping,
  type ExternalObjectSchema,
  type ExternalSyncConflict,
  ExternalSyncConflictResolution,
  ExternalSyncDirection,
  type ExternalSyncEvent,
  type ExternalSyncHealthResponse,
  type ExternalSyncRecordFailure,
  type ExternalSyncRecordTimelineEntry,
  type ExternalSyncRecordTimelineResponse,
  type ExternalSyncRun,
  type ExternalSyncRunDetail,
  type GetExternalSyncRecordTimelineRequest,
  type ListExternalConnectionsResponse,
  type ListExternalObjectMappingsResponse,
  type ListExternalSyncEventsResponse,
  type ListExternalSyncRunsResponse,
  type PreviewExternalObjectMappingRequest,
  type PreviewExternalObjectMappingResponse,
  type QualifyExternalConnectionResponse,
  type ReplayExternalSyncEventResponse,
  type RequestExternalSyncBackfillRequest,
  type RequestExternalSyncBackfillResponse,
  type RequestExternalSyncRunRequest,
  type ResetExternalSyncCursorRequest,
  type ResetExternalSyncCursorResponse,
  type ResumeExternalConnectionRequest,
  type TestExternalConnectionResponse,
  type UpdateExternalConnectionRequest,
  type UpdateExternalObjectMappingRequest,
} from '@/proto/attune/v1/external_sync'

const base = '/fb/v1/console/external-sync'

export type {
  BatchResolveExternalSyncConflictsRequest,
  BatchResolveExternalSyncConflictsResponse,
  CreateExternalConnectionRequest,
  DiscoverExternalConnectionSchemaResponse,
  ExternalConnection,
  ExternalObjectMapping,
  ExternalObjectSchema,
  ExternalSyncConflict,
  ExternalSyncEvent,
  ExternalSyncHealthResponse,
  ExternalSyncRecordFailure,
  ExternalSyncRecordTimelineEntry,
  ExternalSyncRecordTimelineResponse,
  ExternalSyncRun,
  ExternalSyncRunDetail,
  GetExternalSyncRecordTimelineRequest,
  PreviewExternalObjectMappingRequest,
  PreviewExternalObjectMappingResponse,
  QualifyExternalConnectionResponse,
  ReplayExternalSyncEventResponse,
  RequestExternalSyncBackfillRequest,
  RequestExternalSyncBackfillResponse,
  RequestExternalSyncRunRequest,
  ResetExternalSyncCursorRequest,
  ResetExternalSyncCursorResponse,
  ResumeExternalConnectionRequest,
  TestExternalConnectionResponse,
  UpdateExternalConnectionRequest,
  UpdateExternalObjectMappingRequest,
}

export { ExternalSyncConflictResolution, ExternalSyncDirection }

export type ExternalSyncRunsFilter = {
  connectionId?: string
  mappingId?: string
  status?: string
}

export type ExternalSyncEventsFilter = {
  connectionId?: string
  status?: string
}

export const externalSyncQueryKeys = {
  root: ['console', 'external-sync'] as const,
  health: () => [...externalSyncQueryKeys.root, 'health'] as const,
  connections: () => [...externalSyncQueryKeys.root, 'connections'] as const,
  connectionSchema: (connectionId?: string) =>
    [...externalSyncQueryKeys.root, 'connections', connectionId ?? 'none', 'schema'] as const,
  mappings: (connectionId?: string) =>
    [...externalSyncQueryKeys.root, 'mappings', connectionId ?? 'all'] as const,
  runs: (limit: number, filter: ExternalSyncRunsFilter = {}) =>
    [...externalSyncQueryKeys.root, 'runs', limit, filter] as const,
  run: (id: string) => [...externalSyncQueryKeys.root, 'run', id] as const,
  events: (limit: number, filter: ExternalSyncEventsFilter = {}) =>
    [...externalSyncQueryKeys.root, 'events', limit, filter] as const,
  event: (id: string) => [...externalSyncQueryKeys.root, 'event', id] as const,
}

export const externalSyncHealthQuery = () =>
  queryOptions({
    queryKey: externalSyncQueryKeys.health(),
    queryFn: ({ signal }) => api<ExternalSyncHealthResponse>(`${base}/health`, { signal }),
    staleTime: 20_000,
  })

export const externalSyncConnectionsQuery = () =>
  queryOptions({
    queryKey: externalSyncQueryKeys.connections(),
    queryFn: async ({ signal }) => {
      const resp = await api<ListExternalConnectionsResponse>(`${base}/connections`, { signal })
      return resp.connections
    },
    staleTime: 20_000,
  })

export const externalSyncMappingsQuery = (connectionId?: string) =>
  queryOptions({
    queryKey: externalSyncQueryKeys.mappings(connectionId),
    queryFn: async ({ signal }) => {
      const qs = connectionId ? `?connection_id=${encodeURIComponent(connectionId)}` : ''
      const resp = await api<ListExternalObjectMappingsResponse>(`${base}/mappings${qs}`, {
        signal,
      })
      return resp.mappings
    },
    staleTime: 20_000,
  })

export const externalSyncConnectionSchemaQuery = (connectionId?: string) =>
  queryOptions({
    queryKey: externalSyncQueryKeys.connectionSchema(connectionId),
    queryFn: async ({ signal }) => {
      if (!connectionId) return []
      const resp = await api<DiscoverExternalConnectionSchemaResponse>(
        `${base}/connections/${connectionId}/schema`,
        { signal },
      )
      return resp.schemas
    },
    enabled: Boolean(connectionId),
    staleTime: 60_000,
  })

export const externalSyncRunsQuery = (limit = 25, filter: ExternalSyncRunsFilter = {}) =>
  infiniteQueryOptions({
    queryKey: externalSyncQueryKeys.runs(limit, filter),
    queryFn: async ({ pageParam, signal }) => {
      const params = new URLSearchParams()
      params.set('limit', String(limit))
      if (filter.connectionId) params.set('connection_id', filter.connectionId)
      if (filter.mappingId) params.set('mapping_id', filter.mappingId)
      if (filter.status) params.set('status', filter.status)
      if (pageParam) params.set('before_id', pageParam)
      return api<ListExternalSyncRunsResponse>(`${base}/runs?${params.toString()}`, {
        signal,
      })
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) => lastPage.nextBeforeId || undefined,
    staleTime: 10_000,
  })

export const externalSyncRunQuery = (id: string) =>
  queryOptions({
    queryKey: externalSyncQueryKeys.run(id),
    queryFn: ({ signal }) => api<ExternalSyncRunDetail>(`${base}/runs/${id}`, { signal }),
    staleTime: 10_000,
  })

export const externalSyncEventQuery = (id: string) =>
  queryOptions({
    queryKey: externalSyncQueryKeys.event(id),
    queryFn: ({ signal }) => api<ExternalSyncEvent>(`${base}/events/${id}`, { signal }),
    staleTime: 10_000,
  })

export const externalSyncEventsQuery = (limit = 25, filter: ExternalSyncEventsFilter = {}) =>
  infiniteQueryOptions({
    queryKey: externalSyncQueryKeys.events(limit, filter),
    queryFn: async ({ pageParam, signal }) => {
      const params = new URLSearchParams()
      params.set('limit', String(limit))
      if (filter.connectionId) params.set('connection_id', filter.connectionId)
      if (filter.status) params.set('status', filter.status)
      if (pageParam) params.set('before_id', pageParam)
      return api<ListExternalSyncEventsResponse>(`${base}/events?${params.toString()}`, {
        signal,
      })
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) => lastPage.nextBeforeId || undefined,
    staleTime: 10_000,
  })

export function createExternalConnection(body: CreateExternalConnectionRequest) {
  return api<ExternalConnection>(`${base}/connections`, { method: 'POST', body })
}

export function updateExternalConnection(body: UpdateExternalConnectionRequest) {
  const { id, ...patch } = body
  return api<ExternalConnection>(`${base}/connections/${id}`, { method: 'PATCH', body: patch })
}

export function deleteExternalConnection(id: string) {
  return api<void>(`${base}/connections/${id}`, { method: 'DELETE' })
}

export function testExternalConnection(id: string) {
  return api<TestExternalConnectionResponse>(`${base}/connections/${id}:test`, { method: 'POST' })
}

export function resumeExternalConnection(id: string) {
  return api<ExternalConnection>(`${base}/connections/${id}:resume`, { method: 'POST' })
}

export function qualifyExternalConnection(id: string) {
  return api<QualifyExternalConnectionResponse>(`${base}/connections/${id}:qualify`, {
    method: 'POST',
  })
}

export function updateExternalMapping(body: UpdateExternalObjectMappingRequest) {
  const { id, ...patch } = body
  return api<ExternalObjectMapping>(`${base}/mappings/${id}`, { method: 'PUT', body: patch })
}

export function previewExternalObjectMapping(body: PreviewExternalObjectMappingRequest) {
  const { id, ...payload } = body
  return api<PreviewExternalObjectMappingResponse>(`${base}/mappings/${id}:preview`, {
    method: 'POST',
    body: payload,
  })
}

export function resetExternalSyncCursor(body: ResetExternalSyncCursorRequest) {
  return api<ResetExternalSyncCursorResponse>(`${base}/mappings/${body.id}:reset-cursor`, {
    method: 'POST',
  })
}

export function requestExternalSyncBackfill(body: RequestExternalSyncBackfillRequest) {
  const { id, ...payload } = body
  return api<RequestExternalSyncBackfillResponse>(`${base}/mappings/${id}:backfill`, {
    method: 'POST',
    body: payload,
  })
}

export function requestExternalSyncRun(body: RequestExternalSyncRunRequest) {
  return api<ExternalSyncRun>(`${base}/runs`, { method: 'POST', body })
}

export function retryExternalSyncRun(id: string) {
  return api<ExternalSyncRun>(`${base}/runs/${id}:retry`, { method: 'POST' })
}

export function retryExternalSyncFailure(id: string) {
  return api<ExternalSyncRecordFailure>(`${base}/failures/${id}:retry`, { method: 'POST' })
}

export function replayExternalSyncEvent(id: string) {
  return api<ReplayExternalSyncEventResponse>(`${base}/events/${id}:replay`, { method: 'POST' })
}

export function resolveExternalSyncConflict(
  id: string,
  resolution = ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_EXTERNAL_WINS,
) {
  return api<ExternalSyncConflict>(`${base}/conflicts/${id}:resolve`, {
    method: 'POST',
    body: { resolution },
  })
}

export function batchResolveExternalSyncConflicts(
  ids: string[],
  resolution = ExternalSyncConflictResolution.EXTERNAL_SYNC_CONFLICT_RESOLUTION_EXTERNAL_WINS,
) {
  return api<BatchResolveExternalSyncConflictsResponse>(`${base}/conflicts:batch-resolve`, {
    method: 'POST',
    body: { ids, resolution },
  })
}

export function getExternalSyncRecordTimeline(body: GetExternalSyncRecordTimelineRequest) {
  return api<ExternalSyncRecordTimelineResponse>(`${base}/records:timeline`, {
    method: 'POST',
    body,
  })
}
