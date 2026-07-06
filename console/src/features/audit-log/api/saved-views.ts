import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  AuditLogViewState,
  CreateSavedAuditLogViewRequest,
  DeleteSavedAuditLogViewResponse,
  ListSavedAuditLogViewsResponse,
  SavedAuditLogViewResponse,
  UpdateSavedAuditLogViewRequest,
} from '@/proto/attune/v1/audit'

export interface SavedAuditLogViewPayload {
  name: string
  state?: AuditLogViewState
}

export interface UpdateSavedAuditLogViewPayload extends SavedAuditLogViewPayload {
  id: string
}

export const savedAuditLogViewsQuery = () =>
  queryOptions({
    queryKey: ['console', 'audit-log', 'views'] as const,
    queryFn: async ({ signal }) =>
      api<ListSavedAuditLogViewsResponse>('/fb/v1/console/audit-log/views', { signal }),
    staleTime: 15_000,
  })

export async function createSavedAuditLogView(payload: SavedAuditLogViewPayload) {
  return api<SavedAuditLogViewResponse>('/fb/v1/console/audit-log/views', {
    method: 'POST',
    body: payload satisfies CreateSavedAuditLogViewRequest,
  })
}

export async function updateSavedAuditLogView(payload: UpdateSavedAuditLogViewPayload) {
  const { id, ...body } = payload
  return api<SavedAuditLogViewResponse>(
    `/fb/v1/console/audit-log/views/${encodeURIComponent(id)}`,
    {
      method: 'PUT',
      body: body satisfies Omit<UpdateSavedAuditLogViewRequest, 'id'>,
    },
  )
}

export async function deleteSavedAuditLogView(id: string) {
  return api<DeleteSavedAuditLogViewResponse>(
    `/fb/v1/console/audit-log/views/${encodeURIComponent(id)}`,
    {
      method: 'DELETE',
    },
  )
}
