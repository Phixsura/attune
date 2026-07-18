import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import {
  type ApproveModerationSubjectRequest,
  type HideModerationSubjectRequest,
  type ListModerationSubjectsResponse,
  type MarkModerationSubjectSpamRequest,
  ModerationState,
  type ModerationSubject,
  type PortalSubmissionField,
  PortalSubmissionFieldKind,
  type PortalSubmissionFormConfig,
  PublicAccessMode,
  type PublicCustomerRequestDetail,
  type PublicCustomerRequestSummary,
  PublicIdentityMode,
  type PublicRequestPublication,
  PublicSurface,
  type PublicVisibilityPolicy,
  PublicWriteMode,
  type RejectModerationSubjectRequest,
  type RestoreModerationSubjectRequest,
  type RoadmapStatusMapping,
  type UpdatePublicVisibilityPolicyRequest,
  type UpsertPublicRequestProfileRequest,
} from '@/proto/attune/v1/public_visibility'

const base = '/fb/v1/console/public-visibility'
const portalBase = '/fb/v1/portal'

export interface ModerationSubjectsFilters {
  surfaces?: PublicSurface[]
}

export {
  ModerationState,
  type ModerationSubject,
  type PortalSubmissionField,
  PortalSubmissionFieldKind,
  type PortalSubmissionFormConfig,
  PublicAccessMode,
  type PublicCustomerRequestDetail,
  type PublicCustomerRequestSummary,
  PublicIdentityMode,
  type PublicRequestPublication,
  PublicSurface,
  type PublicVisibilityPolicy,
  PublicWriteMode,
  type RoadmapStatusMapping,
  type UpdatePublicVisibilityPolicyRequest,
  type UpsertPublicRequestProfileRequest,
}

export const publicVisibilityQueryKeys = {
  root: ['console', 'public-visibility'] as const,
  policy: () => [...publicVisibilityQueryKeys.root, 'policy'] as const,
  moderation: (filters?: ModerationSubjectsFilters) => {
    const surfaces = normalizeSurfaces(filters?.surfaces)
    if (surfaces.length === 0) {
      return [...publicVisibilityQueryKeys.root, 'moderation'] as const
    }
    return [...publicVisibilityQueryKeys.root, 'moderation', surfaces.join(',')] as const
  },
  requestProfile: (requestId: string) =>
    [...publicVisibilityQueryKeys.root, 'request-profile', requestId] as const,
  publicRequestDetail: (tenantSlug: string, publicSlug: string) =>
    [...publicVisibilityQueryKeys.root, 'public-request-detail', tenantSlug, publicSlug] as const,
}

export const publicVisibilityPolicyQuery = () =>
  queryOptions({
    queryKey: publicVisibilityQueryKeys.policy(),
    queryFn: ({ signal }) => api<PublicVisibilityPolicy>(`${base}/policy`, { signal }),
    staleTime: 20_000,
  })

export const moderationSubjectsQuery = (filters: ModerationSubjectsFilters = {}) =>
  queryOptions({
    queryKey: publicVisibilityQueryKeys.moderation(filters),
    queryFn: async ({ signal }) => {
      const params = new URLSearchParams()
      params.set('limit', '50')
      for (const surface of normalizeSurfaces(filters.surfaces)) {
        params.append('surface', surface)
      }
      const resp = await api<ListModerationSubjectsResponse>(`${base}/moderation?${params}`, {
        signal,
      })
      return resp.subjects
    },
    staleTime: 10_000,
  })

function normalizeSurfaces(surfaces?: PublicSurface[]) {
  if (!surfaces || surfaces.length === 0) return []
  return Array.from(new Set(surfaces.filter((surface) => surface !== PublicSurface.UNRECOGNIZED)))
    .map((surface) => surface)
    .sort()
}

export function updatePublicVisibilityPolicy(body: UpdatePublicVisibilityPolicyRequest) {
  return api<PublicVisibilityPolicy>(`${base}/policy`, { method: 'PUT', body })
}

export function getPublicRequestProfile(requestId: string) {
  return api<PublicRequestPublication>(`${base}/requests/${encodeURIComponent(requestId)}/profile`)
}

export function publicRequestDetailQuery(tenantSlug: string, publicSlug: string) {
  return queryOptions({
    queryKey: publicVisibilityQueryKeys.publicRequestDetail(tenantSlug, publicSlug),
    queryFn: ({ signal }) =>
      api<PublicCustomerRequestDetail>(
        `${portalBase}/${encodeURIComponent(tenantSlug)}/requests/${encodeURIComponent(publicSlug)}`,
        { signal },
      ),
    staleTime: 20_000,
  })
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
