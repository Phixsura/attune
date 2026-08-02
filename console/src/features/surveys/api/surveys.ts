import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  AssignSurveyLowScoreReviewsRequest,
  AssignSurveyLowScoreReviewsResponse,
  BatchUpdateSurveyLowScoreReviewsRequest,
  BatchUpdateSurveyLowScoreReviewsResponse,
  CreateSurveyCampaignRequest,
  CreateSurveyHostedLinkRequest,
  EscalateSurveyLowScoreReviewsRequest,
  EscalateSurveyLowScoreReviewsResponse,
  GetSurveyAnalyticsInsightsResponse,
  GetSurveyAnalyticsSegmentsResponse,
  GetSurveyAnalyticsTrendResponse,
  ListSurveyCampaignsResponse,
  ListSurveyInvitationsResponse,
  ListSurveyResponsesResponse,
  PreviewSurveyRecipientsRequest,
  PreviewSurveyRecipientsResponse,
  SendSurveyTestEmailRequest,
  SendSurveyTestEmailResponse,
  SurveyAnalytics,
  SurveyAnalyticsInsight,
  SurveyAnalyticsSegment,
  SurveyAnalyticsSegmentDimension,
  SurveyAnalyticsTrendBucket,
  SurveyCampaign,
  SurveyCampaignHealth,
  SurveyCampaignStatus,
  SurveyInvitation,
  SurveyLowScoreReview,
  SurveyLowScoreSeverity,
  SurveyRecoverySlaStatus,
  SurveyResponse,
  SurveyResponseStatus,
  SurveySuppressionStatus,
  UpdateSurveyCampaignRequest,
  UpdateSurveyLowScoreReviewRequest,
} from '@/proto/attune/v1/survey'

const endpoint = '/fb/v1/console/surveys'

export const surveyCampaignsQueryKey = ['console', 'surveys', 'campaigns'] as const
export const surveyInvitationsQueryKey = ['console', 'surveys', 'invitations'] as const
export const surveyResponsesQueryKey = ['console', 'surveys', 'responses'] as const
export const surveyAnalyticsQueryKey = ['console', 'surveys', 'analytics'] as const
export const surveyAnalyticsTrendQueryKey = ['console', 'surveys', 'analytics-trend'] as const
export const surveyAnalyticsSegmentsQueryKey = ['console', 'surveys', 'analytics-segments'] as const
export const surveyAnalyticsInsightsQueryKey = ['console', 'surveys', 'analytics-insights'] as const
export const surveyCampaignHealthQueryKey = ['console', 'surveys', 'campaign-health'] as const

export interface SurveyInvitationFilters {
  campaignId?: string
  responseStatus?: SurveyResponseStatus
  suppressionStatus?: SurveySuppressionStatus
  limit?: number
}

export interface SurveyResponseFilters {
  accountKey?: string
  campaignId?: string
  from?: string
  limit?: number
  lowScoreOnly?: boolean
  ownerMemberId?: string
  recoveryBlockerReason?: string
  recoverySlaStatus?: SurveyRecoverySlaStatus
  reviewSeverity?: SurveyLowScoreSeverity
  to?: string
}

export interface SurveyAnalyticsFilters {
  campaignId?: string
  from?: string
  to?: string
}

export interface SurveyAnalyticsSegmentFilters extends SurveyAnalyticsFilters {
  dimension?: SurveyAnalyticsSegmentDimension
  limit?: number
}

export interface SurveyAnalyticsInsightFilters extends SurveyAnalyticsFilters {
  limit?: number
}

export function surveyCampaignsQuery(status?: SurveyCampaignStatus, limit = 50) {
  return queryOptions({
    queryKey: [...surveyCampaignsQueryKey, status ?? '', limit],
    queryFn: async ({ signal }) => {
      const params = new URLSearchParams({ limit: String(limit) })
      if (status) params.set('status', status)
      const res = await api<ListSurveyCampaignsResponse>(
        `${endpoint}/campaigns?${params.toString()}`,
        { signal },
      )
      return res.campaigns ?? []
    },
  })
}

export function surveyInvitationsQuery(filters: SurveyInvitationFilters = {}) {
  return queryOptions({
    queryKey: [
      ...surveyInvitationsQueryKey,
      filters.campaignId ?? '',
      filters.responseStatus ?? '',
      filters.suppressionStatus ?? '',
      filters.limit ?? 25,
    ],
    queryFn: async ({ signal }) => {
      const params = surveyParams({
        campaign_id: filters.campaignId,
        response_status: filters.responseStatus,
        suppression_status: filters.suppressionStatus,
        limit: String(filters.limit ?? 25),
      })
      const res = await api<ListSurveyInvitationsResponse>(
        `${endpoint}/invitations?${params.toString()}`,
        { signal },
      )
      return res.invitations ?? []
    },
  })
}

export function surveyResponsesQuery(filters: SurveyResponseFilters = {}) {
  return queryOptions({
    queryKey: [
      ...surveyResponsesQueryKey,
      filters.campaignId ?? '',
      filters.lowScoreOnly ?? false,
      filters.from ?? '',
      filters.to ?? '',
      filters.recoverySlaStatus ?? '',
      filters.recoveryBlockerReason ?? '',
      filters.reviewSeverity ?? '',
      filters.ownerMemberId ?? '',
      filters.accountKey ?? '',
      filters.limit ?? 25,
    ],
    queryFn: async ({ signal }) => {
      const params = surveyParams({
        account_key: filters.accountKey,
        campaign_id: filters.campaignId,
        low_score_only:
          filters.lowScoreOnly === undefined ? undefined : String(filters.lowScoreOnly),
        from: filters.from,
        to: filters.to,
        recovery_sla_status: filters.recoverySlaStatus,
        recovery_blocker_reason: filters.recoveryBlockerReason,
        review_severity: filters.reviewSeverity,
        owner_member_id: filters.ownerMemberId,
        limit: String(filters.limit ?? 25),
      })
      const res = await api<ListSurveyResponsesResponse>(`${endpoint}/responses?${params}`, {
        signal,
      })
      return res.responses ?? []
    },
  })
}

export function surveyAnalyticsQuery(filters: SurveyAnalyticsFilters = {}) {
  return queryOptions({
    queryKey: [
      ...surveyAnalyticsQueryKey,
      filters.campaignId ?? '',
      filters.from ?? '',
      filters.to ?? '',
    ],
    queryFn: ({ signal }) => {
      const params = surveyParams({
        campaign_id: filters.campaignId,
        from: filters.from,
        to: filters.to,
      })
      const suffix = params.size > 0 ? `?${params.toString()}` : ''
      return api<SurveyAnalytics>(`${endpoint}/analytics${suffix}`, { signal })
    },
  })
}

export function surveyAnalyticsTrendQuery(filters: SurveyAnalyticsFilters = {}) {
  return queryOptions({
    queryKey: [
      ...surveyAnalyticsTrendQueryKey,
      filters.campaignId ?? '',
      filters.from ?? '',
      filters.to ?? '',
    ],
    queryFn: async ({ signal }) => {
      const params = surveyParams({
        campaign_id: filters.campaignId,
        from: filters.from,
        to: filters.to,
      })
      const suffix = params.size > 0 ? `?${params.toString()}` : ''
      const res = await api<GetSurveyAnalyticsTrendResponse>(
        `${endpoint}/analytics/trend${suffix}`,
        {
          signal,
        },
      )
      return res.buckets ?? []
    },
  })
}

export function surveyAnalyticsSegmentsQuery(filters: SurveyAnalyticsSegmentFilters = {}) {
  return queryOptions({
    queryKey: [
      ...surveyAnalyticsSegmentsQueryKey,
      filters.campaignId ?? '',
      filters.from ?? '',
      filters.to ?? '',
      filters.dimension ?? '',
      filters.limit ?? 8,
    ],
    queryFn: async ({ signal }) => {
      const params = surveyParams({
        campaign_id: filters.campaignId,
        from: filters.from,
        to: filters.to,
        dimension: filters.dimension,
        limit: String(filters.limit ?? 8),
      })
      const res = await api<GetSurveyAnalyticsSegmentsResponse>(
        `${endpoint}/analytics/segments?${params.toString()}`,
        { signal },
      )
      return res.segments ?? []
    },
  })
}

export function surveyAnalyticsInsightsQuery(filters: SurveyAnalyticsInsightFilters = {}) {
  return queryOptions({
    queryKey: [
      ...surveyAnalyticsInsightsQueryKey,
      filters.campaignId ?? '',
      filters.from ?? '',
      filters.to ?? '',
      filters.limit ?? 6,
    ],
    queryFn: async ({ signal }) => {
      const params = surveyParams({
        campaign_id: filters.campaignId,
        from: filters.from,
        to: filters.to,
        limit: String(filters.limit ?? 6),
      })
      const res = await api<GetSurveyAnalyticsInsightsResponse>(
        `${endpoint}/analytics/insights?${params.toString()}`,
        { signal },
      )
      return res.insights ?? []
    },
  })
}

export function surveyCampaignHealthQuery(campaignId?: string) {
  return queryOptions({
    enabled: Boolean(campaignId),
    queryKey: [...surveyCampaignHealthQueryKey, campaignId ?? ''],
    queryFn: ({ signal }) =>
      api<SurveyCampaignHealth>(
        `${endpoint}/campaigns/${encodeURIComponent(campaignId ?? '')}/health`,
        { signal },
      ),
  })
}

export function useCreateSurveyCampaign() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: CreateSurveyCampaignRequest) =>
      api<SurveyCampaign>(`${endpoint}/campaigns`, {
        method: 'POST',
        body,
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: surveyCampaignsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsTrendQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsSegmentsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsInsightsQueryKey })
      qc.invalidateQueries({ queryKey: surveyCampaignHealthQueryKey })
    },
  })
}

export function useUpdateSurveyCampaign() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: UpdateSurveyCampaignRequest) =>
      api<SurveyCampaign>(`${endpoint}/campaigns/${encodeURIComponent(body.id)}`, {
        method: 'PATCH',
        body,
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: surveyCampaignsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsTrendQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsSegmentsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsInsightsQueryKey })
      qc.invalidateQueries({ queryKey: surveyCampaignHealthQueryKey })
    },
  })
}

export function useArchiveSurveyCampaign() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<SurveyCampaign>(`${endpoint}/campaigns/${encodeURIComponent(id)}:archive`, {
        method: 'POST',
        body: { id },
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: surveyCampaignsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsTrendQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsSegmentsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsInsightsQueryKey })
      qc.invalidateQueries({ queryKey: surveyInvitationsQueryKey })
      qc.invalidateQueries({ queryKey: surveyCampaignHealthQueryKey })
    },
  })
}

export function useCreateSurveyHostedLink() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: CreateSurveyHostedLinkRequest) =>
      api<SurveyInvitation>(
        `${endpoint}/campaigns/${encodeURIComponent(body.campaignId)}/hosted-links`,
        {
          method: 'POST',
          body,
        },
      ),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: surveyInvitationsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsTrendQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsSegmentsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsInsightsQueryKey })
      qc.invalidateQueries({ queryKey: surveyCampaignHealthQueryKey })
    },
  })
}

export function usePreviewSurveyRecipients() {
  return useMutation({
    mutationFn: (body: PreviewSurveyRecipientsRequest) =>
      api<PreviewSurveyRecipientsResponse>(
        `${endpoint}/campaigns/${encodeURIComponent(body.campaignId)}/recipients:preview`,
        {
          method: 'POST',
          body,
        },
      ),
  })
}

export function useSendSurveyTestEmail() {
  return useMutation({
    mutationFn: (body: SendSurveyTestEmailRequest) =>
      api<SendSurveyTestEmailResponse>(
        `${endpoint}/campaigns/${encodeURIComponent(body.campaignId)}:sendTestEmail`,
        {
          method: 'POST',
          body,
        },
      ),
  })
}

export function useRetrySurveyInvitationDelivery() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<SurveyInvitation>(`${endpoint}/invitations/${encodeURIComponent(id)}:retry`, {
        method: 'POST',
        body: { id },
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: surveyInvitationsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsTrendQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsSegmentsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsInsightsQueryKey })
      qc.invalidateQueries({ queryKey: surveyCampaignHealthQueryKey })
    },
  })
}

export function useUpdateSurveyLowScoreReview() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: UpdateSurveyLowScoreReviewRequest) =>
      api<SurveyLowScoreReview>(
        `${endpoint}/responses/${encodeURIComponent(body.responseId)}/low-score-review`,
        {
          method: 'PATCH',
          body,
        },
      ),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: surveyResponsesQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsTrendQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsSegmentsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsInsightsQueryKey })
      qc.invalidateQueries({ queryKey: surveyCampaignHealthQueryKey })
    },
  })
}

export function useBatchUpdateSurveyLowScoreReviews() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: BatchUpdateSurveyLowScoreReviewsRequest) =>
      api<BatchUpdateSurveyLowScoreReviewsResponse>(
        `${endpoint}/responses/low-score-reviews:batchUpdate`,
        {
          method: 'POST',
          body,
        },
      ),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: surveyResponsesQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsTrendQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsSegmentsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsInsightsQueryKey })
      qc.invalidateQueries({ queryKey: surveyCampaignHealthQueryKey })
    },
  })
}

export function useAssignSurveyLowScoreReviews() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: AssignSurveyLowScoreReviewsRequest) =>
      api<AssignSurveyLowScoreReviewsResponse>(`${endpoint}/responses/low-score-reviews:assign`, {
        method: 'POST',
        body,
      }),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: surveyResponsesQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsTrendQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsSegmentsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsInsightsQueryKey })
      qc.invalidateQueries({ queryKey: surveyCampaignHealthQueryKey })
    },
  })
}

export function useEscalateSurveyLowScoreReviews() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: EscalateSurveyLowScoreReviewsRequest) =>
      api<EscalateSurveyLowScoreReviewsResponse>(
        `${endpoint}/responses/low-score-reviews:escalate`,
        {
          method: 'POST',
          body,
        },
      ),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: surveyResponsesQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsTrendQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsSegmentsQueryKey })
      qc.invalidateQueries({ queryKey: surveyAnalyticsInsightsQueryKey })
      qc.invalidateQueries({ queryKey: surveyCampaignHealthQueryKey })
    },
  })
}

function surveyParams(values: Record<string, string | undefined>) {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(values)) {
    if (value !== undefined && value !== '') params.set(key, value)
  }
  return params
}

export type {
  PreviewSurveyRecipientsResponse,
  SendSurveyTestEmailResponse,
  SurveyAnalytics,
  SurveyAnalyticsInsight,
  SurveyAnalyticsSegment,
  SurveyAnalyticsTrendBucket,
  SurveyCampaign,
  SurveyCampaignHealth,
  SurveyInvitation,
  SurveyResponse,
}
