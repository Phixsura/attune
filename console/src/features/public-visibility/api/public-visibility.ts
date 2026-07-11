import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import {
  type ApproveModerationSubjectRequest,
  type HideModerationSubjectRequest,
  type ListModerationSubjectsResponse,
  type MarkModerationSubjectSpamRequest,
  ModerationState,
  type ModerationSubject,
  PublicAccessMode,
  PublicIdentityMode,
  type PublicRequestPublication,
  type PublicVisibilityPolicy,
  PublicWriteMode,
  type RejectModerationSubjectRequest,
  type RestoreModerationSubjectRequest,
  type UpdatePublicVisibilityPolicyRequest,
  type UpsertPublicRequestProfileRequest,
} from '@/proto/attune/v1/public_visibility'

const base = '/fb/v1/console/public-visibility'

export {
  ModerationState,
  type ModerationSubject,
  PublicAccessMode,
  PublicIdentityMode,
  type PublicRequestPublication,
  type PublicVisibilityPolicy,
  PublicWriteMode,
  type UpdatePublicVisibilityPolicyRequest,
  type UpsertPublicRequestProfileRequest,
}

export const publicVisibilityQueryKeys = {
  root: ['console', 'public-visibility'] as const,
  policy: () => [...publicVisibilityQueryKeys.root, 'policy'] as const,
  moderation: () => [...publicVisibilityQueryKeys.root, 'moderation'] as const,
  requestProfile: (requestId: string) =>
    [...publicVisibilityQueryKeys.root, 'request-profile', requestId] as const,
}

export const publicVisibilityPolicyQuery = () =>
  queryOptions({
    queryKey: publicVisibilityQueryKeys.policy(),
    queryFn: ({ signal }) => api<PublicVisibilityPolicy>(`${base}/policy`, { signal }),
    staleTime: 20_000,
  })

export const moderationSubjectsQuery = () =>
  queryOptions({
    queryKey: publicVisibilityQueryKeys.moderation(),
    queryFn: async ({ signal }) => {
      const params = new URLSearchParams()
      params.set('limit', '50')
      const resp = await api<ListModerationSubjectsResponse>(`${base}/moderation?${params}`, {
        signal,
      })
      return resp.subjects
    },
    staleTime: 10_000,
  })

export function updatePublicVisibilityPolicy(body: UpdatePublicVisibilityPolicyRequest) {
  return api<PublicVisibilityPolicy>(`${base}/policy`, { method: 'PUT', body })
}

export function getPublicRequestProfile(requestId: string) {
  return api<PublicRequestPublication>(`${base}/requests/${encodeURIComponent(requestId)}/profile`)
}

export function upsertPublicRequestProfile(body: UpsertPublicRequestProfileRequest) {
  return api<PublicRequestPublication>(
    `${base}/requests/${encodeURIComponent(body.requestId)}/profile`,
    { method: 'PUT', body },
  )
}

export function approveModerationSubject(body: ApproveModerationSubjectRequest) {
  return api<ModerationSubject>(`${base}/moderation/${body.id}:approve`, {
    method: 'POST',
    body: actionBody(body),
  })
}

export function rejectModerationSubject(body: RejectModerationSubjectRequest) {
  return api<ModerationSubject>(`${base}/moderation/${body.id}:reject`, {
    method: 'POST',
    body: actionBody(body),
  })
}

export function hideModerationSubject(body: HideModerationSubjectRequest) {
  return api<ModerationSubject>(`${base}/moderation/${body.id}:hide`, {
    method: 'POST',
    body: actionBody(body),
  })
}

export function markModerationSubjectSpam(body: MarkModerationSubjectSpamRequest) {
  return api<ModerationSubject>(`${base}/moderation/${body.id}:mark-spam`, {
    method: 'POST',
    body: actionBody(body),
  })
}

export function restoreModerationSubject(body: RestoreModerationSubjectRequest) {
  return api<ModerationSubject>(`${base}/moderation/${body.id}:restore`, {
    method: 'POST',
    body: actionBody(body),
  })
}

function actionBody(body: { reasonCode?: string; reasonNote?: string }) {
  return {
    reasonCode: body.reasonCode ?? '',
    reasonNote: body.reasonNote ?? '',
  }
}
