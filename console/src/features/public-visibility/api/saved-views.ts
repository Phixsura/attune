import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  CreateSavedPublicVisibilityViewRequest,
  DeleteSavedPublicVisibilityViewResponse,
  ListSavedPublicVisibilityViewsResponse,
  PublicVisibilityViewState,
  SavedPublicVisibilityViewResponse,
  UpdateSavedPublicVisibilityViewRequest,
} from '@/proto/attune/v1/public_visibility'

export const publicVisibilitySavedViewsQueryKey = ['console', 'public-visibility', 'views'] as const

export interface PublicVisibilitySavedViewPayload {
  name: string
  state?: PublicVisibilityViewState
}

export interface UpdatePublicVisibilitySavedViewPayload extends PublicVisibilitySavedViewPayload {
  id: string
}

export const publicVisibilitySavedViewsQuery = () =>
  queryOptions({
    queryKey: publicVisibilitySavedViewsQueryKey,
    queryFn: async ({ signal }) =>
      api<ListSavedPublicVisibilityViewsResponse>('/fb/v1/console/public-visibility/views', {
        signal,
      }),
    staleTime: 15_000,
  })

export async function createPublicVisibilitySavedView(payload: PublicVisibilitySavedViewPayload) {
  return api<SavedPublicVisibilityViewResponse>('/fb/v1/console/public-visibility/views', {
    method: 'POST',
    body: payload satisfies CreateSavedPublicVisibilityViewRequest,
  })
}

export async function updatePublicVisibilitySavedView(
  payload: UpdatePublicVisibilitySavedViewPayload,
) {
  const { id, ...body } = payload
  return api<SavedPublicVisibilityViewResponse>(
    `/fb/v1/console/public-visibility/views/${encodeURIComponent(id)}`,
    {
      method: 'PUT',
      body: body satisfies Omit<UpdateSavedPublicVisibilityViewRequest, 'id'>,
    },
  )
}

export async function deletePublicVisibilitySavedView(id: string) {
  return api<DeleteSavedPublicVisibilityViewResponse>(
    `/fb/v1/console/public-visibility/views/${encodeURIComponent(id)}`,
    {
      method: 'DELETE',
    },
  )
}
